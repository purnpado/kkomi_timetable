package timetable

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// PlanTeacherAbsence는 주중 갑작스러운 미출근/결근 기간의 모든 수업을 조회해
// 각 교시마다 (1) 다음주·다다음주 주차간 맞교환 후보, (2) 당일 동시간대 공강 대타 교사 후보를 자동 추천합니다.
func (s *Service) PlanTeacherAbsence(req protocol.AbsencePlanRequest) ([]protocol.AbsenceSlotPlan, error) {
	if strings.TrimSpace(req.TeacherID) == "" {
		return nil, errors.New("대상 교사를 선택해 주세요")
	}
	startT, err := time.Parse("2006-01-02", strings.TrimSpace(req.StartDate))
	if err != nil {
		return nil, errors.New("시작 일자(YYYY-MM-DD) 형식이 올바르지 않습니다")
	}
	endDateStr := strings.TrimSpace(req.EndDate)
	if endDateStr == "" {
		endDateStr = req.StartDate
	}
	endT, err := time.Parse("2006-01-02", endDateStr)
	if err != nil || endT.Before(startT) {
		return nil, errors.New("종료 일자는 시작 일자보다 같거나 이후여야 합니다")
	}
	if endT.Sub(startT) > 14*24*time.Hour {
		return nil, errors.New("한 번에 최대 14일 이내 기간만 대책 수립이 가능합니다")
	}
	sp, ep := req.StartPeriod, req.EndPeriod
	if sp <= 0 {
		sp = 1
	}
	if ep <= 0 {
		ep = 7
	}

	baseCells, err := s.ListCells()
	if err != nil {
		return nil, err
	}
	teachersList, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}
	reqs, _ := s.ListRequests()
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")

	weekCache := make(map[string][]protocol.TimetableCell)
	getWeek := func(mon string) []protocol.TimetableCell {
		if c, ok := weekCache[mon]; ok {
			return c
		}
		res := buildEffectiveWeekCells(mon, baseCells, tMap, reqs, events)
		weekCache[mon] = res
		return res
	}

	var plans []protocol.AbsenceSlotPlan
	for cur := startT; !cur.After(endT); cur = cur.AddDate(0, 0, 1) {
		wd := int(cur.Weekday())
		if wd < 1 || wd > 5 {
			continue
		}
		dateStr := cur.Format("2006-01-02")
		monStr := NormalizeMonday(dateStr)
		weekCells := getWeek(monStr)

		var dayCells []protocol.TimetableCell
		for _, c := range weekCells {
			if c.LessonDate == dateStr && c.BlockedBy == "" && c.Period >= sp && c.Period <= ep &&
				(c.TeacherID == req.TeacherID || c.CoTeacherID == req.TeacherID) {
				dayCells = append(dayCells, c)
			}
		}
		sort.Slice(dayCells, func(i, j int) bool { return dayCells[i].Period < dayCells[j].Period })

		nextMon1 := AddWeeks(monStr, 1)
		nextMon2 := AddWeeks(monStr, 2)
		nextWeek1 := getWeek(nextMon1)
		nextWeek2 := getWeek(nextMon2)

		for _, c := range dayCells {
			var crossOpts []protocol.CrossWeekCandidate
			for _, cw := range append(
				findCrossWeekForCell(c, weekCells, nextWeek1, tMap, events, false),
				findCrossWeekForCell(c, weekCells, nextWeek2, tMap, events, false)...,
			) {
				if cw.TargetDate >= req.StartDate && cw.TargetDate <= endDateStr {
					continue // 미출근 기간 자체 날짜는 제외
				}
				crossOpts = append(crossOpts, cw)
				if len(crossOpts) >= 8 {
					break
				}
			}

			subOpts := rankSubstituteCandidates(c, weekCells, teachersList, req.TeacherID)
			plans = append(plans, protocol.AbsenceSlotPlan{
				LessonDate:        dateStr,
				Cell:              c,
				CrossWeekOptions:  crossOpts,
				SubstituteOptions: subOpts,
			})
		}
	}
	return plans, nil
}

func rankSubstituteCandidates(
	slot protocol.TimetableCell,
	weekCells []protocol.TimetableCell,
	teachers []protocol.Teacher,
	absentID string,
) []protocol.AbsenceSubCandidate {
	var list []protocol.AbsenceSubCandidate
	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) || t.ID == absentID {
			continue
		}
		if len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, slot.DayOfWeek) {
			continue
		}
		busy := false
		for _, wc := range weekCells {
			if wc.DayOfWeek == slot.DayOfWeek && wc.Period == slot.Period &&
				(wc.TeacherID == t.ID || wc.CoTeacherID == t.ID) {
				busy = true
				break
			}
		}
		if busy || exceedsThreeConsecutiveMap(t.ID, slot.DayOfWeek, slot.Period, map[string]bool{}, weekCells) {
			continue
		}
		badge := fmt.Sprintf("[공강·보강%d회]", t.SubstitutionCount)
		if t.Subject == slot.Subject {
			badge = fmt.Sprintf("[1순위: 동일교과(%s)·공강]", t.Subject)
		} else if t.HomeroomClass != "" && t.HomeroomClass == slot.ClassName {
			badge = fmt.Sprintf("[담임교사(%s)·공강]", t.HomeroomClass)
		}
		list = append(list, protocol.AbsenceSubCandidate{
			TeacherID:         t.ID,
			TeacherName:       t.Name,
			Subject:           t.Subject,
			HomeroomClass:     t.HomeroomClass,
			SubstitutionCount: t.SubstitutionCount,
			PriorityBadge:     badge,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		score := func(c protocol.AbsenceSubCandidate) int {
			s := 0
			if c.Subject == slot.Subject {
				s += 100
			}
			if c.HomeroomClass != "" && c.HomeroomClass == slot.ClassName {
				s += 50
			}
			return s
		}
		if score(list[i]) != score(list[j]) {
			return score(list[i]) > score(list[j])
		}
		if list[i].SubstitutionCount != list[j].SubstitutionCount {
			return list[i].SubstitutionCount < list[j].SubstitutionCount
		}
		return list[i].TeacherName < list[j].TeacherName
	})
	return list
}

// ApplyAbsenceResolutions는 미출근 교시별 해결책(다음주 맞교환 또는 당일 공강교사 대타)을
// 일괄 대기열 등록하거나 담당자 권한 시 즉시 확정 반영합니다.
func (s *Service) ApplyAbsenceResolutions(req protocol.AbsenceApplyRequest, reviewerName string, canApprove bool) ([]protocol.TimetableChangeRequest, error) {
	if len(req.Items) == 0 {
		return nil, errors.New("적용할 결근 대책 항목이 없습니다")
	}
	// 일괄 적용 전 동일 일자·교시 대타 교사 중복 배정 및 동일 타 주차 슬롯 중복 사용 사전 차단
	usedSubSlot := make(map[string]bool)
	usedCrossTarget := make(map[string]bool)
	for _, it := range req.Items {
		if it.Mode == "CROSS_WEEK_SWAP" {
			tKey := fmt.Sprintf("%s#%s#%d", it.TargetDate, it.TargetCell.ClassName, it.TargetCell.Period)
			if usedCrossTarget[tKey] {
				return nil, fmt.Errorf("%s %s반 %d교시 주차 맞교환 슬롯이 중복 지정되었습니다", it.TargetDate, it.TargetCell.ClassName, it.TargetCell.Period)
			}
			usedCrossTarget[tKey] = true
		} else if strings.TrimSpace(it.PartnerID) != "" {
			d := strings.TrimSpace(it.TargetDate)
			if d == "" {
				d = strings.TrimSpace(it.SourceDate)
			}
			sKey := fmt.Sprintf("%s#%d#%s", d, it.SourceCell.Period, it.PartnerID)
			if usedSubSlot[sKey] {
				return nil, fmt.Errorf("%s %d교시에 동일 보강 교사(%s)가 중복 배정되었습니다", d, it.SourceCell.Period, it.PartnerName)
			}
			usedSubSlot[sKey] = true
		}
	}
	var results []protocol.TimetableChangeRequest
	for _, it := range req.Items {
		reqType := "ABSENCE_SUBSTITUTE"
		tgtCell := it.SourceCell
		if it.Mode == "CROSS_WEEK_SWAP" {
			reqType = "CROSS_WEEK_SWAP"
			tgtCell = it.TargetCell
		}
		submitted, err := s.SubmitRequest(protocol.TimetableChangeRequest{
			RequestType:   reqType,
			Scope:         "DATE_ONLY",
			SourceDate:    it.SourceDate,
			TargetDate:    it.TargetDate,
			RequesterID:   req.RequesterID,
			RequesterName: req.RequesterName,
			PartnerID:     it.PartnerID,
			PartnerName:   it.PartnerName,
			PartnerAgreed: true,
			SourceCell:    it.SourceCell,
			TargetCell:    tgtCell,
			Reason:        it.Reason,
		})
		if err != nil {
			return results, err
		}
		if req.AutoApprove && canApprove {
			committed, err := s.CommitRequest(submitted.ID, reviewerName, true, "")
			if err != nil {
				return results, err
			}
			results = append(results, *committed)
		} else {
			results = append(results, submitted)
		}
	}
	return results, nil
}
