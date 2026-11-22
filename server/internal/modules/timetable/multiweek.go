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

	// 1. 해당 주차 확정 행사(CLASS_BLOCK) 및 공휴일/휴업일(HOLIDAY) 반영
	for _, ev := range events {
		if !protocol.EventBlocksClasses(ev) || !protocol.EventOverlapsRange(ev, monStr, friStr) {
			continue
		}
		notePrefix := "[행사블록]"
		if ev.LinkType == protocol.EventLinkHoliday {
			notePrefix = "[휴업일]"
		}
		for _, dateStr := range protocol.EventDates(ev) {
			if dateStr < monStr || dateStr > friStr {
				continue
			}
			parsed, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				continue
			}
			wd := int(parsed.Weekday())
			if wd < 1 || wd > 5 {
				continue
			}
			for i := range out {
				if out[i].DayOfWeek != wd || !matchesEventTarget(ev, out[i].Grade, out[i].ClassName) {
					continue
				}
				if ev.StartPeriod == 0 || (out[i].Period >= ev.StartPeriod && out[i].Period <= ev.EndPeriod) {
					out[i].BlockedBy = ev.Title
					out[i].ChangeNote = fmt.Sprintf("%s %s", notePrefix, ev.Title)
				}
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
				if idx := findEffectiveSlotByReq(out, applyDate, rq); idx >= 0 {
					if rq.PartnerID != "" {
						replaceSlotTeacher(&out[idx], rq.RequesterID, rq.PartnerID)
					}
					if rq.TargetCell.Subject != "" {
						out[idx].Subject = rq.TargetCell.Subject
					}
					out[idx].ChangeNote = fmt.Sprintf("[일일보강] %s(%s)", rq.PartnerName, shortMD(applyDate))
				}
			}
		case "MOVE", "SWAP", "TRIANGLE", "QUAD", "BLOCK_MOVE":
			if rq.Scope != "DATE_ONLY" {
				continue
			}
			effDate := rq.SourceDate
			if effDate == "" {
				effDate = rq.TargetDate
			}
			if effDate == "" || NormalizeMonday(effDate) != monStr {
				continue
			}
			applyDateOnlyChainOverlay(out, monStr, effDate, rq)
		}
	}
	return out
}

func findEffectiveSlotByReq(cells []protocol.TimetableCell, dateStr string, rq protocol.TimetableChangeRequest) int {
	if idx := findEffectiveSlot(cells, dateStr, rq.SourceCell.ClassName, rq.SourceCell.Period); idx >= 0 {
		return idx
	}
	if rq.SourceCell.ID != "" {
		for i := range cells {
			if cells[i].ID == rq.SourceCell.ID && cells[i].LessonDate == dateStr {
				return i
			}
		}
	}
	return -1
}

func applyDateOnlyChainOverlay(out []protocol.TimetableCell, monStr, effDate string, rq protocol.TimetableChangeRequest) {
	findByID := func(id string) int {
		for i := range out {
			if out[i].ID == id {
				return i
			}
		}
		return -1
	}
	noteTag := fmt.Sprintf("[일회성 %s] %s", rq.RequestType, shortMD(effDate))
	if len(rq.ChainSteps) > 0 {
		for _, step := range rq.ChainSteps {
			if idx := findByID(step.CellID); idx >= 0 {
				out[idx].DayOfWeek = step.ToDay
				out[idx].Period = step.ToPeriod
				out[idx].LessonDate = DateForWeekday(monStr, step.ToDay)
				out[idx].ChangeNote = noteTag
			}
		}
		return
	}
	if rq.RequestType == "SWAP" && rq.SourceCell.ID != "" && rq.TargetCell.ID != "" {
		sIdx, tIdx := findByID(rq.SourceCell.ID), findByID(rq.TargetCell.ID)
		if sIdx >= 0 && tIdx >= 0 {
			out[sIdx].DayOfWeek, out[tIdx].DayOfWeek = out[tIdx].DayOfWeek, out[sIdx].DayOfWeek
			out[sIdx].Period, out[tIdx].Period = out[tIdx].Period, out[sIdx].Period
			out[sIdx].LessonDate = DateForWeekday(monStr, out[sIdx].DayOfWeek)
			out[tIdx].LessonDate = DateForWeekday(monStr, out[tIdx].DayOfWeek)
			out[sIdx].ChangeNote = noteTag
			out[tIdx].ChangeNote = noteTag
		}
		return
	}
	if rq.RequestType == "MOVE" && rq.SourceCell.ID != "" && rq.TargetCell.DayOfWeek > 0 && rq.TargetCell.Period > 0 {
		if sIdx := findByID(rq.SourceCell.ID); sIdx >= 0 {
			out[sIdx].DayOfWeek = rq.TargetCell.DayOfWeek
			out[sIdx].Period = rq.TargetCell.Period
			out[sIdx].LessonDate = DateForWeekday(monStr, out[sIdx].DayOfWeek)
			out[sIdx].ChangeNote = noteTag
		}
	}
}

func replaceSlotTeacher(slot *protocol.TimetableCell, oldTeacherID, newTeacherID string) {
	if slot == nil || newTeacherID == "" {
		return
	}
	replacedCo := false
	if slot.CoTeacherID != "" && oldTeacherID != "" && slot.CoTeacherID == oldTeacherID && slot.TeacherID != oldTeacherID {
		slot.CoTeacherID = newTeacherID
		replacedCo = true
	}
	for i, tid := range slot.CoTeacherIDs {
		if oldTeacherID != "" && tid == oldTeacherID && slot.TeacherID != oldTeacherID {
			slot.CoTeacherIDs[i] = newTeacherID
			replacedCo = true
		}
	}
	if !replacedCo {
		slot.TeacherID = newTeacherID
		if slot.CoTeacherID == newTeacherID {
			slot.CoTeacherID = ""
		}
		var filtered []string
		for _, tid := range slot.CoTeacherIDs {
			if tid != "" && tid != newTeacherID {
				filtered = append(filtered, tid)
			}
		}
		slot.CoTeacherIDs = filtered
	}
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
	slot.CoTeacherIDs = append([]string(nil), incoming.CoTeacherIDs...)
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
	constraints, _ := s.ListConstraints()

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

	return findCrossWeekForCell(*src, srcWeekCells, tgtWeekCells, tMap, events, constraints, srcMon == tgtMon), nil
}

func findCrossWeekForCell(
	src protocol.TimetableCell,
	srcWeekCells []protocol.TimetableCell,
	tgtWeekCells []protocol.TimetableCell,
	tMap map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
	constraints []protocol.TeacherConstraint,
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

		if reason1 := slotConflictReason(cand, src.DayOfWeek, src.Period, src.LessonDate, srcIgnore, srcWeekCells, tMap, srcEvents, constraints); reason1 != "" {
			continue
		}
		if reason2 := slotConflictReason(src, cand.DayOfWeek, cand.Period, cand.LessonDate, tgtIgnore, tgtWeekCells, tMap, tgtEvents, constraints); reason2 != "" {
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
	aTeachers := protocol.AllCellTeachers(a)
	for _, bt := range protocol.AllCellTeachers(b) {
		if slices.Contains(aTeachers, bt) {
			return true
		}
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
