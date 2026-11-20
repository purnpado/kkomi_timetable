package timetable

import (
	"fmt"
	"slices"

	"kkomi-timetable/pkg/protocol"
)

var dayNames = []string{"", "월", "화", "수", "목", "금"}

// EvaluateMove는 시간표 Canvas 드래그 앤 드롭 시 제약조건을 판정하여
// GREEN(이동/맞교환 가능), YELLOW(삼각·사각 연쇄 교환 가능), RED(충돌 불가 및 사유)를 반환합니다.
func EvaluateMove(
	req protocol.EvaluateMoveRequest,
	cells []protocol.TimetableCell,
	teachers map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
) protocol.MoveEvaluationResult {
	var src *protocol.TimetableCell
	for i := range cells {
		if cells[i].ID == req.SourceCellID {
			src = &cells[i]
			break
		}
	}
	if src == nil {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "원본 수업 셀을 찾을 수 없습니다"}
	}
	if req.TargetDay < 1 || req.TargetDay > 5 || req.TargetPeriod < 1 || req.TargetPeriod > 7 {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "유효한 요일(월~금) 및 교시(1~7교시)가 아닙니다"}
	}
	if src.IsLocked {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "이동금지(고정) 설정된 수업입니다"}
	}
	if src.BlockedBy != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("행사 블록('%s')으로 잠긴 수업입니다", src.BlockedBy)}
	}
	if src.ElectiveGroup != "" {
		return protocol.MoveEvaluationResult{
			Signal: "RED",
			Mode:   "BLOCKED",
			Reason: fmt.Sprintf("고교학점제 선택과목(%s) 동시간대 블록으로 단독 이동 불가", src.ElectiveGroup),
		}
	}
	weekMon := ""
	if req.TargetDate != "" {
		weekMon = NormalizeMonday(req.TargetDate)
	}
	if blk := eventBlockReason(src.Grade, src.ClassName, req.TargetDay, req.TargetPeriod, weekMon, events); blk != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: blk}
	}
	if itn := itinerantReason(src.TeacherID, req.TargetDay, teachers); itn != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: itn}
	}
	if src.CoTeacherID != "" {
		if itn := itinerantReason(src.CoTeacherID, req.TargetDay, teachers); itn != "" {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: itn}
		}
	}

	var targetCell *protocol.TimetableCell
	for i := range cells {
		if cells[i].DayOfWeek == req.TargetDay && cells[i].Period == req.TargetPeriod && cells[i].ClassName == src.ClassName {
			targetCell = &cells[i]
			break
		}
	}

	// 1. 빈 교시 단순 이동 검사
	if targetCell == nil {
		conflict := slotConflictReason(*src, req.TargetDay, req.TargetPeriod, weekMon, map[string]bool{src.ID: true}, cells, teachers, events)
		if conflict == "" {
			step := buildChainStep(1, *src, src.DayOfWeek, src.Period, req.TargetDay, req.TargetPeriod, teachers)
			return protocol.MoveEvaluationResult{
				Signal:     "GREEN",
				Mode:       "MOVE",
				Reason:     "빈 교시로 즉시 이동 가능합니다",
				ChainSteps: []protocol.ChainSwapStep{step},
			}
		}
		if mode, path, steps := findEmptyTargetChain(src, req.TargetDay, req.TargetPeriod, weekMon, cells, teachers, events); len(steps) > 0 {
			return protocol.MoveEvaluationResult{
				Signal:       "YELLOW",
				Mode:         mode,
				Reason:       fmt.Sprintf("직접 이동 불가 → %d단계 연쇄 이동 가능", len(steps)),
				TrianglePath: path,
				ChainSteps:   steps,
			}
		}
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: conflict}
	}

	if targetCell.IsLocked {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "대상 수업이 이동금지(고정) 설정되어 있습니다"}
	}
	if targetCell.BlockedBy != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("대상 교시는 행사('%s')로 블록되어 있습니다", targetCell.BlockedBy)}
	}
	if targetCell.ElectiveGroup != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "대상 수업이 고교학점제 블록 수업입니다"}
	}

	// 2. 1:1 맞교환 판정
	ignoreBoth := map[string]bool{src.ID: true, targetCell.ID: true}
	srcConflict := slotConflictReason(*src, targetCell.DayOfWeek, targetCell.Period, weekMon, ignoreBoth, cells, teachers, events)
	tgtConflict := slotConflictReason(*targetCell, src.DayOfWeek, src.Period, weekMon, ignoreBoth, cells, teachers, events)
	if srcConflict == "" && tgtConflict == "" {
		return protocol.MoveEvaluationResult{
			Signal: "GREEN",
			Mode:   "SWAP",
			Reason: fmt.Sprintf("%s(%s) ↔ %s(%s) 1:1 수업 맞교환 가능", src.Subject, teacherName(src.TeacherID, teachers), targetCell.Subject, teacherName(targetCell.TeacherID, teachers)),
			ChainSteps: []protocol.ChainSwapStep{
				buildChainStep(1, *src, src.DayOfWeek, src.Period, targetCell.DayOfWeek, targetCell.Period, teachers),
				buildChainStep(2, *targetCell, targetCell.DayOfWeek, targetCell.Period, src.DayOfWeek, src.Period, teachers),
			},
		}
	}

	// 3. 3단계(삼각) → 4단계(사각) 순환 연쇄 교환 탐색
	if mode, path, steps := findCyclicChainSwap(src, targetCell, weekMon, cells, teachers, events); len(steps) > 0 {
		return protocol.MoveEvaluationResult{
			Signal:       "YELLOW",
			Mode:         mode,
			Reason:       fmt.Sprintf("1:1 맞교환 불가 → %d단계 순환 연쇄 교환(%s) 가능", len(steps), modeLabel(mode)),
			TrianglePath: path,
			ChainSteps:   steps,
		}
	}

	reason := srcConflict
	if reason == "" {
		reason = tgtConflict
	}
	return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: reason}
}

func modeLabel(mode string) string {
	if mode == "QUAD" {
		return "사각 교환 1~4단계"
	}
	return "삼각 교환 1~3단계"
}

func findEmptyTargetChain(
	src *protocol.TimetableCell,
	td, tp int,
	weekMon string,
	cells []protocol.TimetableCell,
	teachers map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
) (string, []string, []protocol.ChainSwapStep) {
	classCells := sameClassCandidates(src.ClassName, cells, src.ID)
	// 2단계 연쇄 (중간 -> 빈칸, 원본 -> 중간)
	for _, mid := range classCells {
		if mid.DayOfWeek == td && mid.Period == tp {
			continue
		}
		ign := map[string]bool{src.ID: true, mid.ID: true}
		if slotConflictReason(mid, td, tp, weekMon, ign, cells, teachers, events) == "" &&
			slotConflictReason(*src, mid.DayOfWeek, mid.Period, weekMon, ign, cells, teachers, events) == "" {
			steps := []protocol.ChainSwapStep{
				buildChainStep(1, mid, mid.DayOfWeek, mid.Period, td, tp, teachers),
				buildChainStep(2, *src, src.DayOfWeek, src.Period, mid.DayOfWeek, mid.Period, teachers),
			}
			return "TRIANGLE", []string{src.ID, mid.ID, fmt.Sprintf("%s-%d-%d", src.ClassName, td, tp)}, steps
		}
	}
	return "", nil, nil
}

func findCyclicChainSwap(
	src, tgt *protocol.TimetableCell,
	weekMon string,
	cells []protocol.TimetableCell,
	teachers map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
) (string, []string, []protocol.ChainSwapStep) {
	classCells := sameClassCandidates(src.ClassName, cells, src.ID, tgt.ID)

	// 1) 3단계 삼각 순환 (A→B→C→A 또는 A→C→B→A)
	for _, mid := range classCells {
		ign := map[string]bool{src.ID: true, tgt.ID: true, mid.ID: true}
		if slotConflictReason(*src, mid.DayOfWeek, mid.Period, weekMon, ign, cells, teachers, events) == "" &&
			slotConflictReason(mid, tgt.DayOfWeek, tgt.Period, weekMon, ign, cells, teachers, events) == "" &&
			slotConflictReason(*tgt, src.DayOfWeek, src.Period, weekMon, ign, cells, teachers, events) == "" {
			steps := []protocol.ChainSwapStep{
				buildChainStep(1, *src, src.DayOfWeek, src.Period, mid.DayOfWeek, mid.Period, teachers),
				buildChainStep(2, mid, mid.DayOfWeek, mid.Period, tgt.DayOfWeek, tgt.Period, teachers),
				buildChainStep(3, *tgt, tgt.DayOfWeek, tgt.Period, src.DayOfWeek, src.Period, teachers),
			}
			return "TRIANGLE", []string{src.ID, mid.ID, tgt.ID}, steps
		}
		if slotConflictReason(*src, tgt.DayOfWeek, tgt.Period, weekMon, ign, cells, teachers, events) == "" &&
			slotConflictReason(*tgt, mid.DayOfWeek, mid.Period, weekMon, ign, cells, teachers, events) == "" &&
			slotConflictReason(mid, src.DayOfWeek, src.Period, weekMon, ign, cells, teachers, events) == "" {
			steps := []protocol.ChainSwapStep{
				buildChainStep(1, *src, src.DayOfWeek, src.Period, tgt.DayOfWeek, tgt.Period, teachers),
				buildChainStep(2, *tgt, tgt.DayOfWeek, tgt.Period, mid.DayOfWeek, mid.Period, teachers),
				buildChainStep(3, mid, mid.DayOfWeek, mid.Period, src.DayOfWeek, src.Period, teachers),
			}
			return "TRIANGLE", []string{src.ID, tgt.ID, mid.ID}, steps
		}
	}

	// 2) 4단계 사각 순환 (A→M1→M2→C→A 또는 A→C→M1→M2→A)
	for _, m1 := range classCells {
		for _, m2 := range classCells {
			if m1.ID == m2.ID {
				continue
			}
			ign := map[string]bool{src.ID: true, tgt.ID: true, m1.ID: true, m2.ID: true}
			if slotConflictReason(*src, m1.DayOfWeek, m1.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(m1, m2.DayOfWeek, m2.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(m2, tgt.DayOfWeek, tgt.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(*tgt, src.DayOfWeek, src.Period, weekMon, ign, cells, teachers, events) == "" {
				steps := []protocol.ChainSwapStep{
					buildChainStep(1, *src, src.DayOfWeek, src.Period, m1.DayOfWeek, m1.Period, teachers),
					buildChainStep(2, m1, m1.DayOfWeek, m1.Period, m2.DayOfWeek, m2.Period, teachers),
					buildChainStep(3, m2, m2.DayOfWeek, m2.Period, tgt.DayOfWeek, tgt.Period, teachers),
					buildChainStep(4, *tgt, tgt.DayOfWeek, tgt.Period, src.DayOfWeek, src.Period, teachers),
				}
				return "QUAD", []string{src.ID, m1.ID, m2.ID, tgt.ID}, steps
			}
			if slotConflictReason(*src, tgt.DayOfWeek, tgt.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(*tgt, m1.DayOfWeek, m1.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(m1, m2.DayOfWeek, m2.Period, weekMon, ign, cells, teachers, events) == "" &&
				slotConflictReason(m2, src.DayOfWeek, src.Period, weekMon, ign, cells, teachers, events) == "" {
				steps := []protocol.ChainSwapStep{
					buildChainStep(1, *src, src.DayOfWeek, src.Period, tgt.DayOfWeek, tgt.Period, teachers),
					buildChainStep(2, *tgt, tgt.DayOfWeek, tgt.Period, m1.DayOfWeek, m1.Period, teachers),
					buildChainStep(3, m1, m1.DayOfWeek, m1.Period, m2.DayOfWeek, m2.Period, teachers),
					buildChainStep(4, m2, m2.DayOfWeek, m2.Period, src.DayOfWeek, src.Period, teachers),
				}
				return "QUAD", []string{src.ID, tgt.ID, m1.ID, m2.ID}, steps
			}
		}
	}
	return "", nil, nil
}

func sameClassCandidates(className string, cells []protocol.TimetableCell, excludeIDs ...string) []protocol.TimetableCell {
	var out []protocol.TimetableCell
	for _, c := range cells {
		if c.ClassName != className || c.IsLocked || c.BlockedBy != "" || c.ElectiveGroup != "" || slices.Contains(excludeIDs, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func buildChainStep(step int, cell protocol.TimetableCell, fd, fp, td, tp int, teachers map[string]protocol.Teacher) protocol.ChainSwapStep {
	tName := teacherName(cell.TeacherID, teachers)
	desc := fmt.Sprintf("%d단계: %s(%s·%s) %s %d교시 → %s %d교시",
		step, tName, cell.ClassName, cell.Subject, dayNames[fd], fp, dayNames[td], tp)
	return protocol.ChainSwapStep{
		Step: step, CellID: cell.ID, ClassName: cell.ClassName, Subject: cell.Subject,
		TeacherID: cell.TeacherID, TeacherName: tName, Room: cell.Room,
		FromDay: fd, FromPeriod: fp, ToDay: td, ToPeriod: tp, Description: desc,
	}
}

func slotConflictReason(
	cell protocol.TimetableCell,
	day, period int,
	weekMon string,
	ignoreIDs map[string]bool,
	cells []protocol.TimetableCell,
	teachers map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
) string {
	if blk := eventBlockReason(cell.Grade, cell.ClassName, day, period, weekMon, events); blk != "" {
		return blk
	}
	if itn := itinerantReason(cell.TeacherID, day, teachers); itn != "" {
		return itn
	}
	if cell.CoTeacherID != "" {
		if itn := itinerantReason(cell.CoTeacherID, day, teachers); itn != "" {
			return itn
		}
	}
	for _, c := range cells {
		if ignoreIDs[c.ID] || c.DayOfWeek != day || c.Period != period {
			continue
		}
		if c.TeacherID == cell.TeacherID || c.CoTeacherID == cell.TeacherID {
			return fmt.Sprintf("%s 선생님이 동시간대 %s반(%s) 수업 중입니다", teacherName(cell.TeacherID, teachers), c.ClassName, c.Subject)
		}
		if cell.CoTeacherID != "" && (c.TeacherID == cell.CoTeacherID || c.CoTeacherID == cell.CoTeacherID) {
			return fmt.Sprintf("코티칭 %s 선생님이 동시간대 %s반(%s) 수업 중입니다", teacherName(cell.CoTeacherID, teachers), c.ClassName, c.Subject)
		}
		if cell.Room != "" && cell.Room != cell.ClassName+"교실" && c.Room == cell.Room {
			return fmt.Sprintf("교과교실(%s)이 동시간대 %s반(%s)에서 사용 중입니다", cell.Room, c.ClassName, c.Subject)
		}
	}
	if exceedsThreeConsecutiveMap(cell.TeacherID, day, period, ignoreIDs, cells) {
		return fmt.Sprintf("%s 선생님의 3연강 초과 제한에 걸립니다", teacherName(cell.TeacherID, teachers))
	}
	if cell.CoTeacherID != "" && exceedsThreeConsecutiveMap(cell.CoTeacherID, day, period, ignoreIDs, cells) {
		return fmt.Sprintf("코티칭 %s 선생님의 3연강 초과 제한에 걸립니다", teacherName(cell.CoTeacherID, teachers))
	}
	return ""
}

func matchesEventTarget(ev protocol.SchoolEvent, grade int, className string) bool {
	if len(ev.TargetClasses) > 0 && slices.Contains(ev.TargetClasses, className) {
		return true
	}
	if len(ev.TargetGrades) > 0 && slices.Contains(ev.TargetGrades, grade) {
		return true
	}
	return false
}

func eventBlockReason(grade int, className string, day, period int, weekMon string, events []protocol.SchoolEvent) string {
	// 정규 기본 시간표 편집(weekMon == "")에는 단일 일자 행사를 영구 블록으로 적용하지 않음
	if weekMon == "" {
		return ""
	}
	targetDate := DateForWeekday(weekMon, day)
	for _, ev := range events {
		if ev.Status == protocol.StatusApproved && ev.LinkType == protocol.EventLinkClassBlock && ev.EventDate == targetDate {
			if matchesEventTarget(ev, grade, className) && (ev.StartPeriod == 0 || (period >= ev.StartPeriod && period <= ev.EndPeriod)) {
				return fmt.Sprintf("확정 행사 '%s'(%s) 수업블록과 충돌", ev.Title, ev.EventDate)
			}
		}
	}
	return ""
}

func itinerantReason(teacherID string, day int, teachers map[string]protocol.Teacher) string {
	if t, ok := teachers[teacherID]; ok && len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, day) {
		return fmt.Sprintf("순회교사 %s 선생님은 해당 요일에 출강하지 않습니다", t.Name)
	}
	return ""
}

func teacherName(id string, teachers map[string]protocol.Teacher) string {
	if t, ok := teachers[id]; ok {
		return t.Name
	}
	return id
}

func exceedsThreeConsecutiveMap(teacherID string, day, targetPeriod int, ignoreIDs map[string]bool, cells []protocol.TimetableCell) bool {
	occupied := make(map[int]bool)
	for _, c := range cells {
		if !ignoreIDs[c.ID] && c.DayOfWeek == day && (c.TeacherID == teacherID || c.CoTeacherID == teacherID) {
			occupied[c.Period] = true
		}
	}
	occupied[targetPeriod] = true

	streak := 0
	for p := 1; p <= 7; p++ {
		if occupied[p] {
			streak++
			if streak > 3 {
				return true
			}
		} else {
			streak = 0
		}
	}
	return false
}
