package internal_exam

import (
	"fmt"
	"sort"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

func equalizeAnnualLoadsWithConstraints(
	slots []protocol.ExamSlot,
	regularTeachers []protocol.Teacher,
	load map[string]*roleLoad,
	deputyMode bool,
	unavailByDate map[string]map[string]string,
	avoidConsecutive bool,
	maxDaily int,
) {
	if len(regularTeachers) < 2 || len(slots) == 0 {
		return
	}
	tByID := make(map[string]protocol.Teacher, len(regularTeachers))
	for _, t := range regularTeachers {
		tByID[t.ID] = t
	}
	slotKey := func(sl protocol.ExamSlot) string {
		return fmt.Sprintf("%s#%d", sl.ExamDate, sl.Period)
	}
	usage := buildSlotUsageMaps(slots)

	effTotal := func(t protocol.Teacher) int {
		return load[t.ID].Total + t.ExamReductionWeight
	}

	canAssignTo := func(t protocol.Teacher, sl protocol.ExamSlot, removeSlot *protocol.ExamSlot, allowSubjectCorridor bool) bool {
		if !canTeacherFitSlot(t, sl, allowSubjectCorridor, tByID, unavailByDate) {
			return false
		}
		if t.ExamMaxCount > 0 && removeSlot == nil && usage.currentCount[t.ID] >= t.ExamMaxCount {
			return false
		}
		exclP := -1
		if removeSlot == nil || removeSlot.ExamDate != sl.ExamDate {
			if maxDaily > 0 && usage.dailyCount[sl.ExamDate][t.ID] >= maxDaily {
				return false
			}
		} else {
			exclP = removeSlot.Period
		}
		if (avoidConsecutive || t.ExamNoConsecutive) && isConsecutiveExcluding(usage.dailyPeriods[sl.ExamDate][t.ID], sl.Period, exclP) {
			return false
		}
		return true
	}

	moveSlot := func(idx int, newT protocol.Teacher) {
		sl := slots[idx]
		oldID := sl.TeacherID
		role := sl.SlotRole
		usage.mark(oldID, sl.ExamDate, sl.Period, false)
		usage.mark(newT.ID, sl.ExamDate, sl.Period, true)
		load[oldID].add(role, -1)
		load[newT.ID].add(role, 1)
		slots[idx].TeacherID = newT.ID
		slots[idx].TeacherName = newT.Name
	}

	// 1단계: 연간 총 감독횟수(ProjectedTotal + 경감가중치) 최대-최소 편차를 1회 이하로 평탄화
	for iter := 0; iter < 120; iter++ {
		minTot, maxTot := 1<<30, -1
		for _, t := range regularTeachers {
			tot := effTotal(t)
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
		// 직접 이관: effTotal >= minTot+2 교사의 슬롯을 effTotal <= old-2 교사에게 전달
		for i := range slots {
			oldID := slots[i].TeacherID
			oldT, ok := tByID[oldID]
			if !ok {
				continue
			}
			oldEff := effTotal(oldT)
			if oldEff < minTot+2 {
				continue
			}
			k := slotKey(slots[i])
			for _, cand := range regularTeachers {
				if effTotal(cand) > oldEff-2 || usage.usedPerPeriod[k][cand.ID] {
					continue
				}
				if canAssignTo(cand, slots[i], nil, true) {
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
			hT, ok := tByID[hID]
			if !ok || effTotal(hT) < minTot+2 {
				continue
			}
			hEff := effTotal(hT)
			ki := slotKey(slots[i])
			for j := range slots {
				mID := slots[j].TeacherID
				if i == j || mID == hID {
					continue
				}
				midT, ok := tByID[mID]
				if !ok || usage.usedPerPeriod[ki][midT.ID] || !canAssignTo(midT, slots[i], &slots[j], true) {
					continue
				}
				kj := slotKey(slots[j])
				for _, lowT := range regularTeachers {
					if lowT.ID == hID || lowT.ID == mID || effTotal(lowT) > hEff-2 {
						continue
					}
					if !usage.usedPerPeriod[kj][lowT.ID] && canAssignTo(lowT, slots[j], nil, true) {
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
		// 동일 날짜·교시 내 MAIN(고정감독 교사) <-> SUB(저정감독 교사) 역할 맞교환
		for i := range slots {
			if slots[i].SlotRole != "MAIN" {
				continue
			}
			aID := slots[i].TeacherID
			aT, okA := tByID[aID]
			if !okA || load[aID].Main < minM+2 {
				continue
			}
			ki := slotKey(slots[i])
			for j := range slots {
				if slots[j].SlotRole != "SUB" || slotKey(slots[j]) != ki {
					continue
				}
				bID := slots[j].TeacherID
				bT, okB := tByID[bID]
				if !okB || bID == aID || load[bID].Main > load[aID].Main-2 {
					continue
				}
				if canTeacherFitSlot(aT, slots[j], false, tByID, unavailByDate) &&
					canTeacherFitSlot(bT, slots[i], false, tByID, unavailByDate) {
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
			k := slotKey(slots[i])
			for _, bT := range regularTeachers {
				if load[bT.ID].Main > load[aID].Main-2 || usage.usedPerPeriod[k][bT.ID] {
					continue
				}
				if load[bT.ID].Total+1 > minTot+1 {
					continue
				}
				if canAssignTo(bT, slots[i], nil, false) {
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

	exemptIDs := make(map[string]bool)
	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			exemptIDs[t.ID] = true
			continue
		}
		if exempt, _ := IsEffectiveExamExempt(t); exempt {
			exemptIDs[t.ID] = true
		}
	}

	dayCount, consecPairs, maxDaily := computeMultiDayFairnessStats(slots, exemptIDs)
	balanceMode := cfg.BalanceMode
	if balanceMode == "" {
		balanceMode = "TOTAL_AND_ROLE"
	}
	summary := protocol.ExamFairnessSummary{
		DeputyMode:           cfg.DeputyMode,
		BalanceMode:          balanceMode,
		ExamDayCount:         dayCount,
		ConsecutivePairCount: consecPairs,
		MaxDailyAssigned:     maxDaily,
		MinTotal:             1 << 30,
		MinMain:              1 << 30,
	}

	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
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
			TeacherID:           t.ID,
			TeacherName:         t.Name,
			Subject:             t.Subject,
			Position:            t.Position,
			HomeroomClass:       t.HomeroomClass,
			IsExempt:            exempt,
			ExemptReason:        reason,
			ExamMaxCount:        t.ExamMaxCount,
			ExamReductionWeight: t.ExamReductionWeight,
			ExamNoConsecutive:   t.ExamNoConsecutive,
			ExamReductionReason: t.ExamReductionReason,
			BaseMain:            bm,
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
