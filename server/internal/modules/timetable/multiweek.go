package timetable

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// NormalizeMonday는 입력된 날짜(YYYY-MM-DD)가 속한 주의 월요일 날짜(YYYY-MM-DD)를 반환합니다.
func NormalizeMonday(dateStr string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(dateStr))
	if err != nil {
		t = time.Now()
	}
	wd := int(t.Weekday()) // 0=일, 1=월 ... 6=토
	if wd == 0 {
		wd = 7
	}
	mon := t.AddDate(0, 0, -(wd - 1))
	return mon.Format("2006-01-02")
}

// DateForWeekday는 기준 월요일(weekStart)과 요일 번호(1=월~5=금)에 해당하는 YYYY-MM-DD를 반환합니다.
func DateForWeekday(weekStart string, day int) string {
	mon, err := time.Parse("2006-01-02", NormalizeMonday(weekStart))
	if err != nil {
		mon = time.Now()
	}
	if day < 1 {
		day = 1
	}
	return mon.AddDate(0, 0, day-1).Format("2006-01-02")
}

// AddWeeks는 기준 날짜에서 weeks주 전·후 날짜(YYYY-MM-DD)를 계산합니다.
func AddWeeks(dateStr string, weeks int) string {
	t, err := time.Parse("2006-01-02", NormalizeMonday(dateStr))
	if err != nil {
		t = time.Now()
	}
	return t.AddDate(0, 0, weeks*7).Format("2006-01-02")
}

// ListWeekCells는 특정 주차(월요일 weekStart 기준)의 실제 적용 시간표를 반환합니다.
// weekStart가 비어 있거나 "BASE"이면 기본 정규 시간표(ListCells)를 그대로 반환합니다.
func (s *Service) ListWeekCells(weekStart string) ([]protocol.TimetableCell, error) {
	if strings.TrimSpace(weekStart) == "" || strings.EqualFold(strings.TrimSpace(weekStart), "BASE") {
		return s.ListCells()
	}
	baseCells, err := s.ListCells()
	if err != nil {
		return nil, err
	}
	teachersList, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	reqs, _ := s.ListRequests()
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")

	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}
	return buildEffectiveWeekCells(NormalizeMonday(weekStart), baseCells, tMap, reqs, events), nil
}

func buildEffectiveWeekCells(
	monStr string,
	baseCells []protocol.TimetableCell,
	tMap map[string]protocol.Teacher,
	reqs []protocol.TimetableChangeRequest,
	events []protocol.SchoolEvent,
) []protocol.TimetableCell {
	friStr := DateForWeekday(monStr, 5)
	out := make([]protocol.TimetableCell, len(baseCells))
	for i, c := range baseCells {
		c.LessonDate = DateForWeekday(monStr, c.DayOfWeek)
		out[i] = c
	}

	// 1. 해당 주차 확정 행사(CLASS_BLOCK) 반영
	for _, ev := range events {
		if ev.Status != protocol.StatusApproved || ev.LinkType != protocol.EventLinkClassBlock {
			continue
		}
		if ev.EventDate < monStr || ev.EventDate > friStr {
			continue
		}
		parsed, err := time.Parse("2006-01-02", ev.EventDate)
		if err != nil {
			continue
		}
		wd := int(parsed.Weekday())
		for i := range out {
			if out[i].DayOfWeek != wd || !matchesEventTarget(ev, out[i].Grade, out[i].ClassName) {
				continue
			}
			if ev.StartPeriod == 0 || (out[i].Period >= ev.StartPeriod && out[i].Period <= ev.EndPeriod) {
				out[i].BlockedBy = ev.Title
				out[i].ChangeNote = fmt.Sprintf("[행사블록] %s", ev.Title)
			}
		}
	}

	// 2. 확정된 일자·주차별 변경 요청(CROSS_WEEK_SWAP / DATE_ONLY 보강)을 확정 시각순 오버레이
	ordered := make([]protocol.TimetableChangeRequest, len(reqs))
	copy(ordered, reqs)
	orderKey := func(r protocol.TimetableChangeRequest) string {
		if r.CommittedAt != "" {
			return r.CommittedAt
		}
		return r.CreatedAt
	}
	sort.Slice(ordered, func(i, j int) bool {
		ki, kj := orderKey(ordered[i]), orderKey(ordered[j])
		if ki != kj {
			return ki < kj
		}
		return ordered[i].ID < ordered[j].ID
	})

	for _, rq := range ordered {
		if rq.Status != protocol.StatusApproved {
			continue
		}
		switch rq.RequestType {
		case "CROSS_WEEK_SWAP":
			if rq.SourceDate >= monStr && rq.SourceDate <= friStr {
				if idx := findEffectiveSlot(out, rq.SourceDate, rq.SourceCell.ClassName, rq.SourceCell.Period); idx >= 0 {
					applySwapOverlay(&out[idx], rq.TargetCell, tMap, fmt.Sprintf("[주차교환↔%s] %s", shortMD(rq.TargetDate), rq.TargetCell.Subject))
				}
			}
			if rq.TargetDate >= monStr && rq.TargetDate <= friStr {
				if idx := findEffectiveSlot(out, rq.TargetDate, rq.TargetCell.ClassName, rq.TargetCell.Period); idx >= 0 {
					applySwapOverlay(&out[idx], rq.SourceCell, tMap, fmt.Sprintf("[주차교환↔%s] %s", shortMD(rq.SourceDate), rq.SourceCell.Subject))
				}
			}
		case "ABSENCE_SUBSTITUTE", "SUBSTITUTE", "EXTERNAL_TRIP", "EVENT_TRIP":
			if rq.Scope != "DATE_ONLY" {
				continue
			}
			applyDate := rq.TargetDate
			if applyDate == "" {
				applyDate = rq.SourceDate
			}
			if applyDate >= monStr && applyDate <= friStr {
				if idx := findEffectiveSlot(out, applyDate, rq.SourceCell.ClassName, rq.SourceCell.Period); idx >= 0 {
					if rq.PartnerID != "" {
						if out[idx].CoTeacherID != "" && out[idx].CoTeacherID == rq.RequesterID && out[idx].TeacherID != rq.RequesterID {
							out[idx].CoTeacherID = rq.PartnerID
						} else {
							out[idx].TeacherID = rq.PartnerID
							if out[idx].CoTeacherID == rq.PartnerID {
								out[idx].CoTeacherID = ""
							}
						}
					}
					if rq.TargetCell.Subject != "" {
						out[idx].Subject = rq.TargetCell.Subject
					}
					out[idx].ChangeNote = fmt.Sprintf("[일일보강] %s(%s)", rq.PartnerName, shortMD(applyDate))
				}
			}
		}
	}
	return out
}

func findEffectiveSlot(cells []protocol.TimetableCell, dateStr, className string, period int) int {
	for i := range cells {
		if cells[i].LessonDate == dateStr && cells[i].ClassName == className && cells[i].Period == period {
			return i
		}
	}
	return -1
}

func applySwapOverlay(slot *protocol.TimetableCell, incoming protocol.TimetableCell, tMap map[string]protocol.Teacher, note string) {
	slot.Subject = incoming.Subject
	slot.SubjectCode = incoming.SubjectCode
	slot.TeacherID = incoming.TeacherID
	slot.CoTeacherID = incoming.CoTeacherID
	if incoming.Room != "" {
		slot.Room = incoming.Room
	}
	_ = tMap
	slot.ChangeNote = note
}

func shortMD(dateStr string) string {
	if len(dateStr) >= 10 {
		return dateStr[5:]
	}
	return dateStr
}

// FindCrossWeekSwapCandidates는 이번 주(sourceDate)에 변경할 수 없는 수업을
// 다음 주·다다음 주 등 지정 주차(targetWeekStart)의 동일 학급 수업과 맞교환할 수 있는 후보를 탐색합니다.
func (s *Service) FindCrossWeekSwapCandidates(sourceCellID, sourceDate, targetWeekStart string) ([]protocol.CrossWeekCandidate, error) {
	if strings.TrimSpace(sourceDate) == "" {
		sourceDate = time.Now().Format("2006-01-02")
	}
	srcMon := NormalizeMonday(sourceDate)
	if strings.TrimSpace(targetWeekStart) == "" {
		targetWeekStart = AddWeeks(srcMon, 1)
	}
	tgtMon := NormalizeMonday(targetWeekStart)

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

	srcWeekCells := buildEffectiveWeekCells(srcMon, baseCells, tMap, reqs, events)
	tgtWeekCells := buildEffectiveWeekCells(tgtMon, baseCells, tMap, reqs, events)

	var src *protocol.TimetableCell
	for i := range srcWeekCells {
		if srcWeekCells[i].ID == sourceCellID {
			src = &srcWeekCells[i]
			break
		}
	}
	if src == nil {
		return nil, errors.New("원본 수업 셀을 찾을 수 없습니다")
	}
	if src.IsLocked {
		return nil, errors.New("이동금지(고정) 설정된 수업은 주차 교환할 수 없습니다")
	}
	if src.ElectiveGroup != "" {
		return nil, errors.New("고교학점제 선택과목 동시 블록은 개별 주차 교환이 불가합니다")
	}
	if strings.TrimSpace(sourceDate) != "" && src.LessonDate != sourceDate {
		// 요일이 다른 날짜를 입력한 경우에도 원본 셀 실제 요일 날짜로 보정
		sourceDate = src.LessonDate
	}

	return findCrossWeekForCell(*src, srcWeekCells, tgtWeekCells, tMap, events, srcMon == tgtMon), nil
}

func findCrossWeekForCell(
	src protocol.TimetableCell,
	srcWeekCells []protocol.TimetableCell,
	tgtWeekCells []protocol.TimetableCell,
	tMap map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
	sameWeek bool,
) []protocol.CrossWeekCandidate {
	var cands []protocol.CrossWeekCandidate
	for _, cand := range tgtWeekCells {
		if cand.ClassName != src.ClassName || cand.IsLocked || cand.ElectiveGroup != "" || cand.BlockedBy != "" {
			continue
		}
		if sameWeek && cand.DayOfWeek == src.DayOfWeek && cand.Period == src.Period {
			continue
		}
		// 다른 주차와 맞교환할 때 본인(주담당/코티칭)의 수업과는 교환할 수 없음
		if !sameWeek && hasTeacherOverlap(src, cand) {
			continue
		}

		srcIgnore := map[string]bool{src.ID: true}
		tgtIgnore := map[string]bool{cand.ID: true}
		if sameWeek {
			srcIgnore[cand.ID] = true
			tgtIgnore[src.ID] = true
		}

		// 각 해당 일자의 행사 블록만 충돌 검사 대상에 포함
		srcEvents := filterDateEvents(events, src.LessonDate)
		tgtEvents := filterDateEvents(events, cand.LessonDate)

		if reason1 := slotConflictReason(cand, src.DayOfWeek, src.Period, src.LessonDate, srcIgnore, srcWeekCells, tMap, srcEvents); reason1 != "" {
			continue
		}
		if reason2 := slotConflictReason(src, cand.DayOfWeek, cand.Period, cand.LessonDate, tgtIgnore, tgtWeekCells, tMap, tgtEvents); reason2 != "" {
			continue
		}

		srcT := teacherName(src.TeacherID, tMap)
		tgtT := teacherName(cand.TeacherID, tMap)
		step1 := buildChainStep(1, cand, cand.DayOfWeek, cand.Period, src.DayOfWeek, src.Period, tMap)
		step1.FromDate = cand.LessonDate
		step1.ToDate = src.LessonDate
		step1.Description = fmt.Sprintf("1단계(%s %s %d교시): %s(%s) 선생님이 '%s' 선행 수업",
			src.LessonDate, dayNames[src.DayOfWeek], src.Period, tgtT, cand.ClassName, cand.Subject)

		step2 := buildChainStep(2, src, src.DayOfWeek, src.Period, cand.DayOfWeek, cand.Period, tMap)
		step2.FromDate = src.LessonDate
		step2.ToDate = cand.LessonDate
		step2.Description = fmt.Sprintf("2단계(%s %s %d교시): %s(%s) 선생님이 '%s' 보충 수업",
			cand.LessonDate, dayNames[cand.DayOfWeek], cand.Period, srcT, src.ClassName, src.Subject)

		cands = append(cands, protocol.CrossWeekCandidate{
			TargetDate: cand.LessonDate,
			TargetCell: cand,
			ChainSteps: []protocol.ChainSwapStep{step1, step2},
			Reason: fmt.Sprintf("%s %s %d교시(%s·%s) ↔ %s %s %d교시(%s·%s)",
				shortMD(src.LessonDate), dayNames[src.DayOfWeek], src.Period, src.Subject, srcT,
				shortMD(cand.LessonDate), dayNames[cand.DayOfWeek], cand.Period, cand.Subject, tgtT),
		})
	}
	return cands
}

func hasTeacherOverlap(a, b protocol.TimetableCell) bool {
	if a.TeacherID != "" && (a.TeacherID == b.TeacherID || a.TeacherID == b.CoTeacherID) {
		return true
	}
	if a.CoTeacherID != "" && (a.CoTeacherID == b.TeacherID || a.CoTeacherID == b.CoTeacherID) {
		return true
	}
	return false
}

func filterDateEvents(events []protocol.SchoolEvent, dateStr string) []protocol.SchoolEvent {
	var out []protocol.SchoolEvent
	for _, ev := range events {
		if ev.EventDate == dateStr {
			out = append(out, ev)
		}
	}
	return out
}
