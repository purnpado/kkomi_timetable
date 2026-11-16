package timetable

import (
	"fmt"
	"slices"

	"kkomi-timetable/pkg/protocol"
)

// EvaluateMove는 시간표 Canvas 드래그 앤 드롭 시 제약조건을 판정하여
// GREEN(이동/맞교환 가능), YELLOW(삼각 교환 가능), RED(충돌 불가 및 사유)를 반환합니다.
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

	// 1. 고교학점제 이동수업 블록 제약 확인
	if src.ElectiveGroup != "" {
		return protocol.MoveEvaluationResult{
			Signal: "RED",
			Mode:   "BLOCKED",
			Reason: fmt.Sprintf("고교학점제 선택과목(%s) 동시간대 블록으로 단독 이동 불가", src.ElectiveGroup),
		}
	}

	// 2. 승인된 학교 행사 수업블록 확인
	for _, ev := range events {
		if ev.Status == protocol.StatusApproved && ev.LinkType == protocol.EventLinkClassBlock {
			if slices.Contains(ev.TargetGrades, src.Grade) && req.TargetPeriod >= ev.StartPeriod && req.TargetPeriod <= ev.EndPeriod {
				return protocol.MoveEvaluationResult{
					Signal: "RED",
					Mode:   "BLOCKED",
					Reason: fmt.Sprintf("확정 행사 '%s' 수업블록과 충돌", ev.Title),
				}
			}
		}
	}

	// 3. 순회교사 출강 가능 요일 확인
	if t, ok := teachers[src.TeacherID]; ok && len(t.ItinerantDays) > 0 {
		if !slices.Contains(t.ItinerantDays, req.TargetDay) {
			return protocol.MoveEvaluationResult{
				Signal: "RED",
				Mode:   "BLOCKED",
				Reason: fmt.Sprintf("순회교사 %s 선생님은 해당 요일에 출강하지 않습니다", t.Name),
			}
		}
	}

	// 4. 대상 슬롯에 이미 수업이 있는지 검사
	var targetCell *protocol.TimetableCell
	for i := range cells {
		if cells[i].DayOfWeek == req.TargetDay && cells[i].Period == req.TargetPeriod && cells[i].ClassName == src.ClassName {
			targetCell = &cells[i]
			break
		}
	}

	// 빈 교시로 단순 이동하는 경우: 교사/코티칭 교사 동시간대 충돌 및 3연강 확인
	if targetCell == nil {
		if conflict := teacherBusyReason(src.TeacherID, req.TargetDay, req.TargetPeriod, src.ID, cells, teachers); conflict != "" {
			// 교사가 다른 반 수업 중이면 삼각 교환 후보를 탐색해 노란색(YELLOW) 제안 시도
			if tri := findTrianglePath(src, req.TargetDay, req.TargetPeriod, cells, teachers); len(tri) > 0 {
				return protocol.MoveEvaluationResult{
					Signal:       "YELLOW",
					Mode:         "TRIANGLE",
					Reason:       "1:1 이동은 불가하나 삼각 교환 경로가 존재합니다",
					TrianglePath: tri,
				}
			}
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: conflict}
		}
		if src.CoTeacherID != "" {
			if conflict := teacherBusyReason(src.CoTeacherID, req.TargetDay, req.TargetPeriod, src.ID, cells, teachers); conflict != "" {
				return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "코티칭 " + conflict}
			}
		}
		if exceedsThreeConsecutive(src.TeacherID, req.TargetDay, req.TargetPeriod, src.ID, cells) {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "이동 시 교사 3연강 초과 제한에 걸립니다"}
		}
		return protocol.MoveEvaluationResult{Signal: "GREEN", Mode: "MOVE", Reason: "빈 교시로 즉시 이동 가능합니다"}
	}

	// 대상 교시에 다른 수업이 있는 경우: 1:1 맞교환 가능 여부 또는 삼각 교환 판정
	if targetCell.ElectiveGroup != "" {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "대상 수업이 고교학점제 블록 수업입니다"}
	}
	srcBusyAtTarget := teacherBusyReason(src.TeacherID, targetCell.DayOfWeek, targetCell.Period, targetCell.ID, cells, teachers)
	tgtBusyAtSource := teacherBusyReason(targetCell.TeacherID, src.DayOfWeek, src.Period, src.ID, cells, teachers)
	if srcBusyAtTarget == "" && tgtBusyAtSource == "" {
		return protocol.MoveEvaluationResult{
			Signal: "GREEN",
			Mode:   "SWAP",
			Reason: fmt.Sprintf("%s(%s) ↔ %s(%s) 1:1 수업 맞교환 가능", src.Subject, teachers[src.TeacherID].Name, targetCell.Subject, teachers[targetCell.TeacherID].Name),
		}
	}

	// 삼각 교환(A -> B -> C -> A) 경로 탐색
	if tri := findTrianglePath(src, targetCell.DayOfWeek, targetCell.Period, cells, teachers); len(tri) > 0 {
		return protocol.MoveEvaluationResult{
			Signal:       "YELLOW",
			Mode:         "TRIANGLE",
			Reason:       "1:1 맞교환 불가 -> 삼각 순환 교환(A↔B↔C) 가능",
			TrianglePath: tri,
		}
	}

	reason := srcBusyAtTarget
	if reason == "" {
		reason = tgtBusyAtSource
	}
	return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: reason}
}

func teacherBusyReason(teacherID string, day, period int, ignoreCellID string, cells []protocol.TimetableCell, teachers map[string]protocol.Teacher) string {
	for _, c := range cells {
		if c.ID == ignoreCellID {
			continue
		}
		if c.DayOfWeek == day && c.Period == period && (c.TeacherID == teacherID || c.CoTeacherID == teacherID) {
			name := teacherID
			if t, ok := teachers[teacherID]; ok {
				name = t.Name
			}
			return fmt.Sprintf("%s 선생님이 동시간대 %s반(%s) 수업 중입니다", name, c.ClassName, c.Subject)
		}
	}
	return ""
}

func exceedsThreeConsecutive(teacherID string, day, targetPeriod int, ignoreCellID string, cells []protocol.TimetableCell) bool {
	occupied := make(map[int]bool)
	for _, c := range cells {
		if c.ID != ignoreCellID && c.DayOfWeek == day && c.TeacherID == teacherID {
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

func findTrianglePath(src *protocol.TimetableCell, targetDay, targetPeriod int, cells []protocol.TimetableCell, teachers map[string]protocol.Teacher) []string {
	for _, mid := range cells {
		if mid.ClassName != src.ClassName || mid.ID == src.ID {
			continue
		}
		if mid.DayOfWeek == targetDay && mid.Period == targetPeriod {
			continue
		}
		// src 교사가 mid 시간에 비어 있고, mid 교사가 target 시간에 비어 있는지 확인
		if teacherBusyReason(src.TeacherID, mid.DayOfWeek, mid.Period, mid.ID, cells, teachers) == "" &&
			teacherBusyReason(mid.TeacherID, targetDay, targetPeriod, src.ID, cells, teachers) == "" {
			return []string{src.ID, mid.ID, fmt.Sprintf("%s-%d-%d", src.ClassName, targetDay, targetPeriod)}
		}
	}
	return nil
}
