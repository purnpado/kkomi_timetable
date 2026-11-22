package timetable

import (
	"fmt"
	"sort"

	"kkomi-timetable/pkg/protocol"
)

func evaluateWholeBlockMove(
	src protocol.TimetableCell,
	req protocol.EvaluateMoveRequest,
	cells []protocol.TimetableCell,
	teachers map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
	constraints []protocol.TeacherConstraint,
	cfg protocol.SchoolConfig,
) protocol.MoveEvaluationResult {
	var blockCells []protocol.TimetableCell
	blockKind := ""
	if src.ElectiveGroup != "" {
		blockKind = fmt.Sprintf("선택블록(%s)", src.ElectiveGroup)
		for _, c := range cells {
			if c.ElectiveGroup == src.ElectiveGroup && c.DayOfWeek == src.DayOfWeek && c.Period == src.Period {
				blockCells = append(blockCells, c)
			}
		}
	} else if src.BlockGroupID != "" {
		blockKind = "연강 블록"
		for _, c := range cells {
			if c.BlockGroupID == src.BlockGroupID && c.ClassName == src.ClassName {
				blockCells = append(blockCells, c)
			}
		}
	}
	if len(blockCells) == 0 {
		return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "이동할 블록 수업 묶음을 찾을 수 없습니다"}
	}
	sort.Slice(blockCells, func(i, j int) bool {
		if blockCells[i].Period != blockCells[j].Period {
			return blockCells[i].Period < blockCells[j].Period
		}
		return blockCells[i].ClassName < blockCells[j].ClassName
	})

	deltaP := 0
	if src.ElectiveGroup == "" && src.BlockGroupID != "" {
		deltaP = req.TargetPeriod - src.Period
	}
	weekMon := ""
	if req.TargetDate != "" {
		weekMon = NormalizeMonday(req.TargetDate)
	}

	type movePair struct {
		src protocol.TimetableCell
		td  int
		tp  int
	}
	var pairs []movePair
	blockIDSet := make(map[string]bool, len(blockCells))
	for _, bc := range blockCells {
		blockIDSet[bc.ID] = true
		if bc.IsLocked {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s 내 '%s' 수업이 고정 잠금 상태입니다", blockKind, bc.ClassName)}
		}
		if bc.BlockedBy != "" {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s 내 '%s' 수업이 행사로 잠겨 있습니다", blockKind, bc.ClassName)}
		}
		tp := req.TargetPeriod
		if src.ElectiveGroup == "" {
			tp = bc.Period + deltaP
		}
		maxP := 7
		if cfg.DailyPeriodLimits[parseGrade(bc.ClassName, bc.Grade)][req.TargetDay] > 0 {
			maxP = cfg.DailyPeriodLimits[parseGrade(bc.ClassName, bc.Grade)][req.TargetDay]
		}
		if tp < 1 || tp > maxP {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s 통째 이동 시 %s 교시 범위(1~%d)를 벗어납니다", blockKind, bc.ClassName, maxP)}
		}
		if bc.DayOfWeek == req.TargetDay && tp == bc.Period {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: "동일한 시간대로 이동할 수 없습니다"}
		}
		pairs = append(pairs, movePair{src: bc, td: req.TargetDay, tp: tp})
	}

	// 대상 슬롯 점유 셀 확인 (전부 비어있거나 맞교환 가능한지 검사)
	slotByKey := make(map[string]protocol.TimetableCell, len(cells))
	for _, c := range cells {
		slotByKey[fmt.Sprintf("%s-%d-%d", c.ClassName, c.DayOfWeek, c.Period)] = c
	}
	var displaced []movePair
	for _, mp := range pairs {
		tk := fmt.Sprintf("%s-%d-%d", mp.src.ClassName, mp.td, mp.tp)
		if occ, exists := slotByKey[tk]; exists && !blockIDSet[occ.ID] {
			if occ.IsLocked || occ.BlockedBy != "" || (occ.ElectiveGroup != "" && occ.ElectiveGroup == src.ElectiveGroup) {
				return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s반 %d교시(%s)는 이동 불가 상태입니다", occ.ClassName, mp.tp, occ.Subject)}
			}
			if occ.ElectiveGroup != "" {
				return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("대상 슬롯 %s반(%s)이 다른 선택블록(%s)에 속해 있습니다", occ.ClassName, occ.Subject, occ.ElectiveGroup)}
			}
			if occ.BlockGroupID != "" && occ.BlockGroupID != mp.src.BlockGroupID {
				return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("대상 슬롯 %s반(%s)이 다른 연강 블록에 묶여 있습니다", occ.ClassName, occ.Subject)}
			}
			displaced = append(displaced, movePair{src: occ, td: mp.src.DayOfWeek, tp: mp.src.Period})
		}
	}

	// 시뮬레이션 시간표 생성 후 동일 슬롯 중복 점유 및 전체 충돌 일괄 검증
	sim := make([]protocol.TimetableCell, 0, len(cells))
	movedByID := make(map[string]movePair, len(pairs)+len(displaced))
	for _, mp := range pairs {
		movedByID[mp.src.ID] = mp
	}
	for _, dp := range displaced {
		movedByID[dp.src.ID] = dp
	}
	occupiedAfterSim := make(map[string]string, len(cells))
	for _, c := range cells {
		if mp, ok := movedByID[c.ID]; ok {
			c.DayOfWeek = mp.td
			c.Period = mp.tp
		}
		key := fmt.Sprintf("%s-%d-%d", c.ClassName, c.DayOfWeek, c.Period)
		if prevSubj, dup := occupiedAfterSim[key]; dup {
			return protocol.MoveEvaluationResult{
				Signal: "RED",
				Mode:   "BLOCKED",
				Reason: fmt.Sprintf("%s 통째 이동 시 %s반 %d교시에 '%s'·'%s' 수업이 겹칩니다", blockKind, c.ClassName, c.Period, prevSubj, c.Subject),
			}
		}
		occupiedAfterSim[key] = c.Subject
		sim = append(sim, c)
	}

	var steps []protocol.ChainSwapStep
	var sNotes []string
	stepNo := 0
	for _, mp := range pairs {
		stepNo++
		ign := map[string]bool{mp.src.ID: true}
		movedCell := mp.src
		movedCell.DayOfWeek = mp.td
		movedCell.Period = mp.tp
		if reason := slotConflictReasonWithConfig(movedCell, mp.td, mp.tp, weekMon, ign, sim, teachers, events, constraints, cfg); reason != "" {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s 통째 이동 충돌: %s", blockKind, reason)}
		}
		if note := softNotesForSlot(movedCell, mp.td, mp.tp, constraints, teachers); note != "" {
			sNotes = append(sNotes, note)
		}
		steps = append(steps, buildChainStep(stepNo, mp.src, mp.src.DayOfWeek, mp.src.Period, mp.td, mp.tp, teachers))
	}
	for _, dp := range displaced {
		stepNo++
		ign := map[string]bool{dp.src.ID: true}
		movedCell := dp.src
		movedCell.DayOfWeek = dp.td
		movedCell.Period = dp.tp
		if reason := slotConflictReasonWithConfig(movedCell, dp.td, dp.tp, weekMon, ign, sim, teachers, events, constraints, cfg); reason != "" {
			return protocol.MoveEvaluationResult{Signal: "RED", Mode: "BLOCKED", Reason: fmt.Sprintf("%s 맞교환 대상 충돌: %s", blockKind, reason)}
		}
		if note := softNotesForSlot(movedCell, dp.td, dp.tp, constraints, teachers); note != "" {
			sNotes = append(sNotes, note)
		}
		steps = append(steps, buildChainStep(stepNo, dp.src, dp.src.DayOfWeek, dp.src.Period, dp.td, dp.tp, teachers))
	}

	reason := fmt.Sprintf("%s %d개 수업 통째 이동 가능", blockKind, len(pairs))
	if len(displaced) > 0 {
		reason = fmt.Sprintf("%s(%d개) ↔ 기존 수업(%d개) 통째 맞교환 가능", blockKind, len(pairs), len(displaced))
	}
	if len(sNotes) > 0 {
		reason += " (참고: 기피 교시 포함)"
	}
	return protocol.MoveEvaluationResult{
		Signal:     "GREEN",
		Mode:       "BLOCK_MOVE",
		Reason:     reason,
		ChainSteps: steps,
	}
}
