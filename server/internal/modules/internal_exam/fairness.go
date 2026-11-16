package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// TeacherExamPolicyUpdate는 교사별 교내 시험감독 제외(특수반·보건실 등) 및
// 연간 누적 감독횟수 설정/초기화 요청 구조체입니다.
type TeacherExamPolicyUpdate struct {
	TeacherID                string `json:"teacherId"`
	InternalExamExempt       bool   `json:"internalExamExempt"`
	InternalExamExemptReason string `json:"internalExamExemptReason"`
	AnnualMainCount          int    `json:"annualMainCount"`
	AnnualSubCount           int    `json:"annualSubCount"`
	AnnualCorridorCount      int    `json:"annualCorridorCount"`
	ResetAllBaseCounts       bool   `json:"resetAllBaseCounts"`
	CommitCurrentToBase      bool   `json:"commitCurrentToBase"`
}

func equalizeAnnualLoads(
	slots []protocol.ExamSlot,
	regularTeachers []protocol.Teacher,
	load map[string]*roleLoad,
	deputyMode bool,
) {
	if len(regularTeachers) < 2 || len(slots) == 0 {
		return
	}
	tByID := make(map[string]protocol.Teacher, len(regularTeachers))
	for _, t := range regularTeachers {
		tByID[t.ID] = t
	}
	usedPerPeriod := make(map[int]map[string]bool)
	for _, sl := range slots {
		if usedPerPeriod[sl.Period] == nil {
			usedPerPeriod[sl.Period] = make(map[string]bool)
		}
		usedPerPeriod[sl.Period][sl.TeacherID] = true
	}

	canFit := func(t protocol.Teacher, sl protocol.ExamSlot, allowSubjectCorridor bool) bool {
		if sl.SlotRole == "HONORARY" {
			return false
		}
		if sl.SlotRole == "CORRIDOR" {
			if !allowSubjectCorridor {
				return false
			}
			// 교과 질문 담당 복도감독일 때는 동일 과목 교사에게만 이관
			if curT, ok := tByID[sl.TeacherID]; ok && roomOrSpecMatchesTeacherSubject("", sl.Subject, curT.Subject) {
				return roomOrSpecMatchesTeacherSubject("", sl.Subject, t.Subject)
			}
			return true
		}
		roomClass := extractRoomBaseClass(sl.RoomName)
		if roomClass != "" && strings.EqualFold(strings.TrimSpace(t.HomeroomClass), roomClass) {
			return false
		}
		if roomOrSpecMatchesTeacherSubject(sl.RoomName, sl.Subject, t.Subject) {
			return false
		}
		return true
	}

	moveSlot := func(idx int, newT protocol.Teacher) {
		oldID := slots[idx].TeacherID
		role := slots[idx].SlotRole
		p := slots[idx].Period
		usedPerPeriod[p][oldID] = false
		usedPerPeriod[p][newT.ID] = true
		load[oldID].add(role, -1)
		load[newT.ID].add(role, 1)
		slots[idx].TeacherID = newT.ID
		slots[idx].TeacherName = newT.Name
	}

	// 1단계: 연간 총 감독횟수(ProjectedTotal) 최대-최소 편차를 1회 이하로 평탄화
	for iter := 0; iter < 120; iter++ {
		minTot, maxTot := 1<<30, -1
		for _, t := range regularTeachers {
			tot := load[t.ID].Total
			if tot < minTot {
				minTot = tot
			}
			if tot > maxTot {
				maxTot = tot
			}
		}
		if maxTot-minTot <= 1 {
			break
		}
		moved := false
		// 직접 이관: maxTot(또는 >= minTot+2) 교사의 슬롯을 <= load[old]-2 교사에게 전달
		for i := range slots {
			oldID := slots[i].TeacherID
			if _, ok := tByID[oldID]; !ok {
				continue
			}
			if load[oldID].Total < minTot+2 {
				continue
			}
			for _, cand := range regularTeachers {
				if load[cand.ID].Total > load[oldID].Total-2 || usedPerPeriod[slots[i].Period][cand.ID] {
					continue
				}
				if canFit(cand, slots[i], true) {
					moveSlot(i, cand)
					moved = true
					break
				}
			}
			if moved {
				break
			}
		}
		if moved {
			continue
		}
		// 1단계 경유 이관: high(T1) -> mid(T2), mid(T2) -> low(T3)
		for i := range slots {
			hID := slots[i].TeacherID
			if _, ok := tByID[hID]; !ok || load[hID].Total < minTot+2 {
				continue
			}
			for j := range slots {
				mID := slots[j].TeacherID
				if i == j || mID == hID {
					continue
				}
				midT, ok := tByID[mID]
				if !ok || usedPerPeriod[slots[i].Period][midT.ID] || !canFit(midT, slots[i], true) {
					continue
				}
				for _, lowT := range regularTeachers {
					if lowT.ID == hID || lowT.ID == mID || load[lowT.ID].Total > load[hID].Total-2 {
						continue
					}
					if !usedPerPeriod[slots[j].Period][lowT.ID] && canFit(lowT, slots[j], true) {
						moveSlot(j, lowT)
						moveSlot(i, midT)
						moved = true
						break
					}
				}
				if moved {
					break
				}
			}
			if moved {
				break
			}
		}
		if !moved {
			break
		}
	}

	if !deputyMode {
		return
	}

	// 2단계: 정·부감독 동시 운영 시 정감독(MAIN) 횟수도 균등하게 평탄화
	for iter := 0; iter < 80; iter++ {
		minM, maxM, minTot := 1<<30, -1, 1<<30
		for _, t := range regularTeachers {
			if load[t.ID].Main < minM {
				minM = load[t.ID].Main
			}
			if load[t.ID].Main > maxM {
				maxM = load[t.ID].Main
			}
			if load[t.ID].Total < minTot {
				minTot = load[t.ID].Total
			}
		}
		if maxM-minM <= 1 {
			break
		}
		swapped := false
		// 동일 교시 내 MAIN(고정감독 교사) <-> SUB(저정감독 교사) 역할 맞교환
		for i := range slots {
			if slots[i].SlotRole != "MAIN" {
				continue
			}
			aID := slots[i].TeacherID
			aT, okA := tByID[aID]
			if !okA || load[aID].Main < minM+2 {
				continue
			}
			for j := range slots {
				if slots[j].SlotRole != "SUB" || slots[j].Period != slots[i].Period {
					continue
				}
				bID := slots[j].TeacherID
				bT, okB := tByID[bID]
				if !okB || bID == aID || load[bID].Main > load[aID].Main-2 {
					continue
				}
				if canFit(aT, slots[j], false) && canFit(bT, slots[i], false) {
					load[aID].add("MAIN", -1)
					load[aID].add("SUB", 1)
					load[bID].add("SUB", -1)
					load[bID].add("MAIN", 1)
					slots[i].TeacherID, slots[i].TeacherName = bT.ID, bT.Name
					slots[j].TeacherID, slots[j].TeacherName = aT.ID, aT.Name
					swapped = true
					break
				}
			}
			if swapped {
				break
			}
		}
		if swapped {
			continue
		}
		// 총 시수 편차 1 이내를 유지하면서 MAIN 슬롯 직접 전달
		for i := range slots {
			if slots[i].SlotRole != "MAIN" {
				continue
			}
			aID := slots[i].TeacherID
			if _, ok := tByID[aID]; !ok || load[aID].Main < minM+2 {
				continue
			}
			for _, bT := range regularTeachers {
				if load[bT.ID].Main > load[aID].Main-2 || usedPerPeriod[slots[i].Period][bT.ID] {
					continue
				}
				if load[bT.ID].Total+1 > minTot+1 {
					continue
				}
				if canFit(bT, slots[i], false) {
					moveSlot(i, bT)
					swapped = true
					break
				}
			}
			if swapped {
				break
			}
		}
		if !swapped {
			break
		}
	}
}

// CalculateFairnessSummary는 1년간 전교원의 감독 시수(기실시 + 이번 시험 = 연간 합계)와
// 특수반·보건실 제외 현황, 최대-최소 편차 통계를 산출합니다.
func (s *Service) CalculateFairnessSummary() (protocol.ExamFairnessSummary, error) {
	cfg, _ := s.GetConfig()
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return protocol.ExamFairnessSummary{}, err
	}
	slots, _ := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")

	curMap := make(map[string]*roleLoad)
	for _, sl := range slots {
		if curMap[sl.TeacherID] == nil {
			curMap[sl.TeacherID] = &roleLoad{}
		}
		curMap[sl.TeacherID].add(sl.SlotRole, 1)
	}

	balanceMode := cfg.BalanceMode
	if balanceMode == "" {
		balanceMode = "TOTAL_AND_ROLE"
	}
	summary := protocol.ExamFairnessSummary{
		DeputyMode:  cfg.DeputyMode,
		BalanceMode: balanceMode,
		MinTotal:    1 << 30,
		MinMain:     1 << 30,
	}

	for _, t := range teachers {
		if t.IsParentHonorary {
			continue
		}
		exempt, reason := IsEffectiveExamExempt(t)
		bm, bs, bc, btot := EffectiveBaseCounts(t)
		cur := curMap[t.ID]
		cm, cs, cc, ctot := 0, 0, 0, 0
		if cur != nil {
			cm, cs, cc, ctot = cur.Main, cur.Sub, cur.Corridor, cur.Total
		}
		stat := protocol.ExamFairnessStat{
			TeacherID:         t.ID,
			TeacherName:       t.Name,
			Subject:           t.Subject,
			Position:          t.Position,
			HomeroomClass:     t.HomeroomClass,
			IsExempt:          exempt,
			ExemptReason:      reason,
			BaseMain:          bm,
			BaseSub:           bs,
			BaseCorridor:      bc,
			BaseTotal:         btot,
			CurrentMain:       cm,
			CurrentSub:        cs,
			CurrentCorridor:   cc,
			CurrentTotal:      ctot,
			ProjectedMain:     bm + cm,
			ProjectedSub:      bs + cs,
			ProjectedCorridor: bc + cc,
			ProjectedTotal:    btot + ctot,
		}
		summary.Stats = append(summary.Stats, stat)
		if exempt {
			summary.ExemptCount++
			continue
		}
		summary.EligibleCount++
		if stat.ProjectedTotal < summary.MinTotal {
			summary.MinTotal = stat.ProjectedTotal
		}
		if stat.ProjectedTotal > summary.MaxTotal {
			summary.MaxTotal = stat.ProjectedTotal
		}
		if stat.ProjectedMain < summary.MinMain {
			summary.MinMain = stat.ProjectedMain
		}
		if stat.ProjectedMain > summary.MaxMain {
			summary.MaxMain = stat.ProjectedMain
		}
	}

	if summary.EligibleCount == 0 {
		summary.MinTotal, summary.MinMain = 0, 0
	} else {
		summary.MaxGap = summary.MaxTotal - summary.MinTotal
		summary.MainGap = summary.MaxMain - summary.MinMain
	}

	sort.Slice(summary.Stats, func(i, j int) bool {
		if summary.Stats[i].IsExempt != summary.Stats[j].IsExempt {
			return !summary.Stats[i].IsExempt
		}
		if summary.Stats[i].ProjectedTotal != summary.Stats[j].ProjectedTotal {
			return summary.Stats[i].ProjectedTotal < summary.Stats[j].ProjectedTotal
		}
		return summary.Stats[i].TeacherName < summary.Stats[j].TeacherName
	})
	return summary, nil
}

// UpdateTeacherExamPolicy는 특수반·보건실 제외 설정, 기실시 연간 시수 수정, 전체 0회 초기화
// 또는 이번 시험 배정의 연간 누적 확정 반영을 수행합니다.
func (s *Service) UpdateTeacherExamPolicy(req TeacherExamPolicyUpdate) ([]protocol.Teacher, error) {
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	if req.ResetAllBaseCounts {
		for i := range teachers {
			teachers[i].AnnualMainCount = 0
			teachers[i].AnnualSubCount = 0
			teachers[i].AnnualCorridorCount = 0
			teachers[i].InternalExamCount = 0
			if err := s.store.SaveRecord("teachers", teachers[i].ID, teachers[i]); err != nil {
				return nil, err
			}
		}
		return teachers, nil
	}
	if req.CommitCurrentToBase {
		slots, _ := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
		curMap := make(map[string]*roleLoad)
		for _, sl := range slots {
			if curMap[sl.TeacherID] == nil {
				curMap[sl.TeacherID] = &roleLoad{}
			}
			curMap[sl.TeacherID].add(sl.SlotRole, 1)
		}
		for i := range teachers {
			if cur := curMap[teachers[i].ID]; cur != nil {
				bm, bs, bc, _ := EffectiveBaseCounts(teachers[i])
				teachers[i].AnnualMainCount = bm + cur.Main
				teachers[i].AnnualSubCount = bs + cur.Sub
				teachers[i].AnnualCorridorCount = bc + cur.Corridor
				teachers[i].InternalExamCount = teachers[i].AnnualMainCount + teachers[i].AnnualSubCount + teachers[i].AnnualCorridorCount
				if err := s.store.SaveRecord("teachers", teachers[i].ID, teachers[i]); err != nil {
					return nil, err
				}
			}
		}
		return teachers, nil
	}
	for i := range teachers {
		if teachers[i].ID == req.TeacherID {
			teachers[i].InternalExamExempt = req.InternalExamExempt
			teachers[i].InternalExamExemptReason = strings.TrimSpace(req.InternalExamExemptReason)
			teachers[i].AnnualMainCount = max(0, req.AnnualMainCount)
			teachers[i].AnnualSubCount = max(0, req.AnnualSubCount)
			teachers[i].AnnualCorridorCount = max(0, req.AnnualCorridorCount)
			teachers[i].InternalExamCount = teachers[i].AnnualMainCount + teachers[i].AnnualSubCount + teachers[i].AnnualCorridorCount
			if err := s.store.SaveRecord("teachers", teachers[i].ID, teachers[i]); err != nil {
				return nil, err
			}
			return teachers, nil
		}
	}
	return nil, fmt.Errorf("대상 교사(%s)를 찾을 수 없습니다", req.TeacherID)
}
