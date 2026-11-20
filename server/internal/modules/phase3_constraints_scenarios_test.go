package modules_test

import (
	"fmt"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/timetable"
)

func TestPhase3TeacherConstraintsAndBatch(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := timetable.NewService(store, bus)

	var evtCount int
	bus.Subscribe(protocol.EventTeacherConstraintUpdated, func(evt protocol.DomainEvent) {
		evtCount++
	})

	// 1. admin·학부모 명예감독 제약 등록 차단 검증
	if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t01", DayOfWeek: 1, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "테스트",
	}, "관리자"); err == nil {
		t.Fatalf("시스템 관리자(admin) 계정에 제약 등록이 허용되었습니다")
	}
	if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "p01", DayOfWeek: 1, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "테스트",
	}, "관리자"); err == nil {
		t.Fatalf("학부모 명예감독 계정에 제약 등록이 허용되었습니다")
	}

	// 2. Hard / Meeting / Soft 단건 저장 및 이벤트 발행 확인
	hardC, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 2, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "외부 연수",
	}, "이수업")
	if err != nil {
		t.Fatalf("HARD_AVOID 저장 실패: %v", err)
	}
	meetC, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 3, Period: 2, ConstraintType: protocol.ConstraintMeetingFree, Reason: "동학년 협의회",
	}, "이수업")
	if err != nil {
		t.Fatalf("MEETING_FREE 저장 실패: %v", err)
	}
	if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 4, Period: 1, ConstraintType: protocol.ConstraintSoftAvoid, Reason: "1교시 희망 공강",
	}, "이수업"); err != nil {
		t.Fatalf("SOFT_AVOID 저장 실패: %v", err)
	}
	if evtCount != 3 {
		t.Fatalf("TeacherConstraintUpdated 이벤트 발행 수 불일치: got=%d, want=3", evtCount)
	}

	// 3. BatchSaveConstraints (DEPT_HEADS 및 SUBJECT) 검증
	deptSaved, err := svc.BatchSaveConstraints(protocol.BatchConstraintRequest{
		TargetScope:    "DEPT_HEADS",
		GroupTag:       "부장회의",
		DayOfWeek:      5,
		Period:         6,
		ConstraintType: protocol.ConstraintMeetingFree,
		Reason:         "주간 부장회의",
	}, "이수업")
	if err != nil || len(deptSaved) != 3 {
		t.Fatalf("DEPT_HEADS 일괄 등록 실패: err=%v, len=%d (want 3)", err, len(deptSaved))
	}
	mathSaved, err := svc.BatchSaveConstraints(protocol.BatchConstraintRequest{
		TargetScope:    "SUBJECT",
		Subject:        "수학",
		GroupTag:       "수학교과협의회",
		DayOfWeek:      4,
		Period:         6,
		ConstraintType: protocol.ConstraintMeetingFree,
		Reason:         "수학과 협의회",
	}, "이수업")
	if err != nil || len(mathSaved) != 2 {
		t.Fatalf("SUBJECT 일괄 등록 실패: err=%v, len=%d (want 2)", err, len(mathSaved))
	}

	// 4. SaveCell 및 EvaluateMoveWithConstraints 판정 검증
	if _, err := svc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 1, ClassName: "1-2", Grade: 1, Subject: "국어", TeacherID: "t06",
	}); err == nil {
		t.Fatalf("HARD_AVOID 슬롯에 SaveCell이 허용되었습니다")
	}
	if _, err := svc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 3, Period: 2, ClassName: "1-2", Grade: 1, Subject: "국어", TeacherID: "t06",
	}); err == nil {
		t.Fatalf("MEETING_FREE 슬롯에 SaveCell이 허용되었습니다")
	}
	if _, err := svc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 4, Period: 1, ClassName: "1-2", Grade: 1, Subject: "국어", TeacherID: "t06",
	}); err != nil {
		t.Fatalf("SOFT_AVOID 슬롯 SaveCell이 거부되었습니다: %v", err)
	}

	teachersList, _ := database.LoadCollection[protocol.Teacher](store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, tc := range teachersList {
		tMap[tc.ID] = tc
	}
	if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 5, Period: 2, ConstraintType: protocol.ConstraintSoftAvoid, Reason: "금2 기피",
	}, "이수업"); err != nil {
		t.Fatalf("t06 금2 Soft 저장 실패: %v", err)
	}
	constraints, _ := svc.ListConstraints()

	// 연쇄 우회 없이 직접 제약 판정이 나오도록 단일 셀 컨텍스트로 Hard/Meeting/Soft 판정 검증
	evalCells := []protocol.TimetableCell{
		{ID: "eval-src", DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1, Subject: "국어", TeacherID: "t06"},
	}
	resHard := timetable.EvaluateMoveWithConstraints(protocol.EvaluateMoveRequest{
		SourceCellID: "eval-src", TargetDay: 2, TargetPeriod: 1,
	}, evalCells, tMap, nil, constraints)
	if resHard.Signal != "RED" {
		t.Fatalf("Hard 제약 이동 판정이 RED가 아닙니다: %+v", resHard)
	}
	resMeet := timetable.EvaluateMoveWithConstraints(protocol.EvaluateMoveRequest{
		SourceCellID: "eval-src", TargetDay: 3, TargetPeriod: 2,
	}, evalCells, tMap, nil, constraints)
	if resMeet.Signal != "RED" {
		t.Fatalf("Meeting 제약 이동 판정이 RED가 아닙니다: %+v", resMeet)
	}
	resSoft := timetable.EvaluateMoveWithConstraints(protocol.EvaluateMoveRequest{
		SourceCellID: "eval-src", TargetDay: 5, TargetPeriod: 2,
	}, evalCells, tMap, nil, constraints)
	if resSoft.Signal != "GREEN" || !strings.Contains(resSoft.Reason, "기피 교시") {
		t.Fatalf("Soft 제약 이동 판정 오류: %+v", resSoft)
	}

	// 5. DeleteConstraint 후 해당 슬롯 SaveCell 허용 확인
	if err := svc.DeleteConstraint(hardC.ID, "이수업"); err != nil {
		t.Fatalf("제약 삭제 실패: %v", err)
	}
	if err := svc.DeleteConstraint(meetC.ID, "이수업"); err != nil {
		t.Fatalf("제약 삭제 실패: %v", err)
	}
	if _, err := svc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 1, ClassName: "1-2", Grade: 1, Subject: "국어", TeacherID: "t06",
	}); err != nil {
		t.Fatalf("Hard 제약 삭제 후 SaveCell 실패: %v", err)
	}

	// 6. AutoGenerateFromCurriculumDetailed에서 Hard 배제 & 불가피 Soft 경고 확인
	_ = store.WithTx(func(tx *database.TxStore) error {
		return tx.ClearCollections("teaching_loads", "timetable_cells", "teacher_constraints")
	})
	_, _ = svc.SaveSchoolConfig(protocol.SchoolConfig{
		ID:                 "default",
		SchoolName:         "테스트교",
		AcademicYear:       2026,
		GradeClassCounts:   map[int]int{1: 1},
		VirtualClassCounts: map[int]int{},
		DailyPeriodLimits:  map[int]map[int]int{1: {1: 1, 2: 1, 3: 1, 4: 1, 5: 1}},
	})
	_, err = svc.SaveTeachingLoad(protocol.TeachingLoad{
		ID: "ld-test-kor", TeacherID: "t06", Subject: "국어", Grade: 1,
		ClassHours: map[string]int{"1-1": 2}, BlockHours: 1,
	})
	if err != nil {
		t.Fatalf("테스트 시수표 저장 실패: %v", err)
	}
	// 월·화·수 1교시 Hard 금지, 목 1교시 비제약, 금 1교시 Soft 기피 -> 2시간 중 목1(일반), 금1(기피 경고) 배정 기대
	for _, d := range []int{1, 2, 3} {
		if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
			TeacherID: "t06", DayOfWeek: d, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "금지",
		}, "이수업"); err != nil {
			t.Fatalf("Hard 제약 저장 실패: %v", err)
		}
	}
	if _, err := svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 5, Period: 1, ConstraintType: protocol.ConstraintSoftAvoid, Reason: "금1 기피",
	}, "이수업"); err != nil {
		t.Fatalf("Soft 제약 저장 실패: %v", err)
	}

	autoRes, err := svc.AutoGenerateFromCurriculumDetailed(true)
	if err != nil {
		t.Fatalf("시수표 자동 편성 실패: %v", err)
	}
	if len(autoRes.Cells) != 2 || autoRes.UnplacedCount != 0 {
		t.Fatalf("자동 편성 결과 셀 수 오류: cells=%d, unplaced=%d, warns=%v", len(autoRes.Cells), autoRes.UnplacedCount, autoRes.Warnings)
	}
	hasDay4, hasDay5, hasSoftWarn := false, false, false
	for _, c := range autoRes.Cells {
		if c.DayOfWeek <= 3 {
			t.Fatalf("Hard 제약 요일(%d)에 배정되었습니다: %+v", c.DayOfWeek, c)
		}
		if c.DayOfWeek == 4 {
			hasDay4 = true
		}
		if c.DayOfWeek == 5 {
			hasDay5 = true
		}
	}
	for _, w := range autoRes.Warnings {
		if strings.Contains(w, "[기피교시 배정]") {
			hasSoftWarn = true
		}
	}
	if !hasDay4 || !hasDay5 || !hasSoftWarn {
		t.Fatalf("일반 우선 후 불가피 Soft 배정·경고 누락: day4=%v, day5=%v, warns=%v", hasDay4, hasDay5, autoRes.Warnings)
	}

	// 7. PlanTeacherAbsence에서 Hard 교사 제외 및 Soft 교사 [기피교시] 후순위 표시 확인
	// 목요일(2026-12-10, day=4) 1교시 t06 결근 가정: t07 Hard 금지, t08 Soft 기피 설정
	_, _ = svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t07", DayOfWeek: 4, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "출장",
	}, "이수업")
	_, _ = svc.SaveConstraint(protocol.TeacherConstraint{
		TeacherID: "t08", DayOfWeek: 4, Period: 1, ConstraintType: protocol.ConstraintSoftAvoid, Reason: "기피",
	}, "이수업")

	plans, err := svc.PlanTeacherAbsence(protocol.AbsencePlanRequest{
		TeacherID: "t06", StartDate: "2026-12-10", EndDate: "2026-12-10", StartPeriod: 1, EndPeriod: 1,
	})
	if err != nil || len(plans) != 1 {
		t.Fatalf("미출근 대책 조회 실패: err=%v, len=%d", err, len(plans))
	}
	foundSoftBadge := false
	for _, cand := range plans[0].SubstituteOptions {
		if cand.TeacherID == "t07" {
			t.Fatalf("Hard 제약 교사(t07)가 대타 후보에 포함되었습니다")
		}
		if cand.TeacherID == "t08" && strings.Contains(cand.PriorityBadge, "[기피교시]") {
			foundSoftBadge = true
		}
	}
	if !foundSoftBadge {
		t.Fatalf("Soft 제약 교사(t08)의 [기피교시] 배지가 누락되었습니다: %+v", plans[0].SubstituteOptions)
	}
}

func TestPhase3ScenariosAndUndoRedoStack(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := timetable.NewService(store, bus)

	// 1. 품질 지표 산출 검증 (충돌·연강·전일공강 포함 테스트 셋)
	teachersList, _ := database.LoadCollection[protocol.Teacher](store, "teachers")
	testCells := []protocol.TimetableCell{
		{ID: "x1", DayOfWeek: 1, Period: 1, ClassName: "1-1", TeacherID: "t06", Subject: "국어"},
		{ID: "x2", DayOfWeek: 1, Period: 2, ClassName: "1-2", TeacherID: "t06", Subject: "국어"},
		{ID: "x3", DayOfWeek: 1, Period: 3, ClassName: "2-1", TeacherID: "t06", Subject: "국어"},
		{ID: "x4", DayOfWeek: 1, Period: 4, ClassName: "2-2", TeacherID: "t06", Subject: "국어"}, // 4연강+1
		{ID: "x5", DayOfWeek: 1, Period: 1, ClassName: "3-1", TeacherID: "t06", Subject: "국어"}, // 동시간대 교사 충돌+1
	}
	m := timetable.CalculateScenarioMetrics(testCells, teachersList, []protocol.TeacherConstraint{
		{TeacherID: "t06", DayOfWeek: 1, Period: 1, ConstraintType: protocol.ConstraintHardAvoid},
	})
	if m.TotalCells != 5 || m.ConflictCount != 1 || m.HardAvoidViolated < 1 || m.Consecutive4Plus != 1 || m.FreeDayTeachers != 1 {
		t.Fatalf("시나리오 지표 산출 오류: %+v", m)
	}

	curM, err := svc.CurrentMetrics()
	if err != nil || curM.TotalCells == 0 {
		t.Fatalf("현재 시간표 지표 조회 실패: err=%v, metrics=%+v", err, curM)
	}

	// 2. 시나리오 A 저장 -> 셀 수정 -> ApplyScenario 복원 및 이벤트/Undo 이력 검증
	scA, err := svc.SaveScenario("A", "안정안", "초기 시드 기준", "이수업")
	if err != nil || scA.ID != "scenario-A" {
		t.Fatalf("시나리오 A 저장 실패: err=%v, sc=%+v", err, scA)
	}
	if _, err := svc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 5, Period: 4, ClassName: "3-2", Grade: 3, Subject: "국어", TeacherID: "t06",
	}); err != nil {
		t.Fatalf("시나리오 비교용 셀 추가 실패: %v", err)
	}

	var cellsUpdatedCount int
	bus.Subscribe(protocol.EventTimetableCellsUpdated, func(evt protocol.DomainEvent) {
		cellsUpdatedCount++
	})

	applied, err := svc.ApplyScenario("scenario-A", "이수업")
	if err != nil {
		t.Fatalf("시나리오 A 적용 실패: %v", err)
	}
	afterApply, _ := svc.ListCells()
	if len(afterApply) != len(applied.Cells) || cellsUpdatedCount == 0 {
		t.Fatalf("시나리오 적용 후 셀 수(%d vs %d) 또는 이벤트(%d) 불일치", len(afterApply), len(applied.Cells), cellsUpdatedCount)
	}

	// 3. Undo/Redo 20단계 스택 상한(25회 연속 편집) 및 새 편집 시 Redo 초기화 검증
	for i := 0; i < 25; i++ {
		day := (i % 5) + 1
		period := ((i / 5) % 5) + 1
		// 기존 시드와 안 겹치는 3-5반 슬롯에 순차 저장 (teacher t09 -> 빈 시간)
		if _, err := svc.SaveCell(protocol.TimetableCell{
			ID:        fmt.Sprintf("hist-test-%d", i),
			DayOfWeek: day,
			Period:    period,
			ClassName: fmt.Sprintf("3-%d", 5+(i/25)),
			Grade:     3,
			Subject:   "국어",
			TeacherID: "t16",
			Room:      fmt.Sprintf("특별실-%d", i),
		}); err != nil {
			t.Fatalf("%d회차 셀 저장 실패: %v", i, err)
		}
	}

	st, err := svc.GetHistoryStatus()
	if err != nil || st.UndoCount != 20 || !st.CanUndo || st.CanRedo {
		t.Fatalf("20단계 스택 상한 검증 실패: err=%v, status=%+v", err, st)
	}

	stUndo, restoredUndo, err := svc.UndoTimetable("이수업")
	if err != nil || stUndo.UndoCount != 19 || !stUndo.CanRedo || stUndo.RedoCount != 1 {
		t.Fatalf("1차 Undo 상태 오류: err=%v, status=%+v", err, stUndo)
	}
	if len(restoredUndo) != len(afterApply)+24 {
		t.Fatalf("Undo 복원 셀 개수 불일치: got=%d, want=%d", len(restoredUndo), len(afterApply)+24)
	}

	stRedo, restoredRedo, err := svc.RedoTimetable("이수업")
	if err != nil || stRedo.UndoCount != 20 || stRedo.RedoCount != 0 {
		t.Fatalf("Redo 상태 오류: err=%v, status=%+v", err, stRedo)
	}
	if len(restoredRedo) != len(afterApply)+25 {
		t.Fatalf("Redo 복원 셀 개수 불일치: got=%d, want=%d", len(restoredRedo), len(afterApply)+25)
	}

	// 다시 Undo 후 새 편집 발생 시 Redo 스택 초기화 검증
	if _, _, err := svc.UndoTimetable("이수업"); err != nil {
		t.Fatalf("2차 Undo 실패: %v", err)
	}
	if _, err := svc.SaveCell(protocol.TimetableCell{
		ID: "hist-clear-redo", DayOfWeek: 5, Period: 7, ClassName: "3-9", Grade: 3, Subject: "사회", TeacherID: "t16",
	}); err != nil {
		t.Fatalf("Undo 후 신규 저장 실패: %v", err)
	}
	stAfterNew, err := svc.GetHistoryStatus()
	if err != nil || stAfterNew.CanRedo || stAfterNew.RedoCount != 0 {
		t.Fatalf("새 편집 후 Redo 초기화 실패: err=%v, status=%+v", err, stAfterNew)
	}
}
