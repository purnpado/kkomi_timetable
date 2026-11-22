package modules_test

import (
	"fmt"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/timetable"
)

func clearStage2Collections(t *testing.T, store *database.Store) {
	t.Helper()
	err := store.WithTx(func(tx *database.TxStore) error {
		return tx.ClearCollections("timetable_cells", "teaching_loads", "teacher_constraints", "timetable_requests", "school_events", "timetable_workflow")
	})
	if err != nil {
		t.Fatalf("컬렉션 초기화 실패: %v", err)
	}
}

func saveTestTeacher(t *testing.T, authSvc *auth.Service, id, name, subj string, itin []int) {
	t.Helper()
	hasCls := true
	_, err := authSvc.SaveTeacher(protocol.Teacher{
		ID:            id,
		Username:      id,
		Name:          name,
		Subject:       subj,
		TeacherType:   protocol.TeacherTypeSubject,
		HasClasses:    &hasCls,
		ItinerantDays: itin,
		Roles:         []protocol.Role{protocol.RoleTeacher},
	}, "1234")
	if err != nil {
		t.Fatalf("테스트 교사(%s) 생성 실패: %v", id, err)
	}
}

// TestStage2DateOnlyVsPermanentAndAgreement는 일회성(DATE_ONLY)과 정규 영구 변경 분리,
// 상대 교사 사전 합의(AgreeRequest)를 검증합니다.
func TestStage2DateOnlyVsPermanentAndAgreement(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	cells, err := ttSvc.ListCells()
	if err != nil || len(cells) < 2 {
		t.Fatalf("시드 시간표 셀 로드 실패: %v", err)
	}
	var c1, c2 protocol.TimetableCell
	for _, c := range cells {
		if c.ClassName != "1-1" || c.IsLocked || c.ElectiveGroup != "" {
			continue
		}
		if c1.ID == "" {
			c1 = c
		} else if c.ID != c1.ID && c2.ID == "" {
			c2 = c
			break
		}
	}
	if c1.ID == "" || c2.ID == "" {
		t.Fatalf("1-1반 교환용 셀 2개를 찾지 못했습니다")
	}

	// 1. 일회성(DATE_ONLY) 맞교환 신청 및 상대교사 동의 검증
	reqOneOff, err := ttSvc.SubmitRequest(protocol.TimetableChangeRequest{
		RequestType:   "SWAP",
		Scope:         "DATE_ONLY",
		SourceDate:    "2026-11-23",
		TargetDate:    "2026-11-23",
		RequesterID:   c1.TeacherID,
		RequesterName: "요청교사",
		PartnerID:     c2.TeacherID,
		PartnerName:   "상대교사",
		PartnerAgreed: false,
		SourceCell:    c1,
		TargetCell:    c2,
		Reason:        "이번 주 한정 수업 교환",
	})
	if err != nil {
		t.Fatalf("일회성 신청 실패: %v", err)
	}
	if _, err := ttSvc.AgreeRequest(reqOneOff.ID, "other-id", "타인", true, "권한없음", false); err == nil {
		t.Fatalf("타인의 사전 합의 동의가 차단되지 않았습니다")
	}
	agreed, err := ttSvc.AgreeRequest(reqOneOff.ID, c2.TeacherID, "상대교사", true, "쉬는시간 구두합의", false)
	if err != nil || !agreed.PartnerAgreed {
		t.Fatalf("상대 교사 사전 합의 처리 실패: %+v, err=%v", agreed, err)
	}
	if _, err := ttSvc.CommitRequest(reqOneOff.ID, "일과계", true, ""); err != nil {
		t.Fatalf("일회성 요청 승인 실패: %v", err)
	}

	// 마스터 정규 시간표는 그대로, 대상 주차만 변경됨 확인
	baseAfter, _ := ttSvc.ListCells()
	for _, bc := range baseAfter {
		if bc.ID == c1.ID && (bc.DayOfWeek != c1.DayOfWeek || bc.Period != c1.Period) {
			t.Fatalf("일회성 변경이 정규 시간표를 변경했습니다: %+v", bc)
		}
	}
	wkTarget, _ := ttSvc.ListWeekCells("2026-11-23")
	movedHit := false
	for _, wc := range wkTarget {
		if wc.ID == c1.ID && wc.DayOfWeek == c2.DayOfWeek && wc.Period == c2.Period && strings.Contains(wc.ChangeNote, "일회성") {
			movedHit = true
		}
	}
	if !movedHit {
		t.Fatalf("대상 주차(2026-11-23)에 일회성 맞교환이 반영되지 않았습니다")
	}
	wkOther, _ := ttSvc.ListWeekCells("2026-11-30")
	for _, wc := range wkOther {
		if wc.ID == c1.ID && (wc.DayOfWeek != c1.DayOfWeek || wc.Period != c1.Period) {
			t.Fatalf("다른 주차(2026-11-30)에 일회성 변경이 누출되었습니다: %+v", wc)
		}
	}

	// 2. 정규 시간표 영구 수정(Scope == "") 승인 시 마스터 DB 영구 반영 검증
	reqPerm, _ := ttSvc.SubmitRequest(protocol.TimetableChangeRequest{
		RequestType: "SWAP",
		Scope:       "",
		RequesterID: c1.TeacherID,
		PartnerID:   c2.TeacherID,
		SourceCell:  c1,
		TargetCell:  c2,
		Reason:      "정규 시간표 영구 교체",
	})
	if _, err := ttSvc.CommitRequest(reqPerm.ID, "일과계", true, ""); err != nil {
		t.Fatalf("정규 영구 변경 승인 실패: %v", err)
	}
	basePerm, _ := ttSvc.ListCells()
	permMoved := false
	for _, bc := range basePerm {
		if bc.ID == c1.ID && bc.DayOfWeek == c2.DayOfWeek && bc.Period == c2.Period {
			permMoved = true
		}
	}
	if !permMoved {
		t.Fatalf("정규 영구 변경이 마스터 시간표에 반영되지 않았습니다")
	}
}

// TestStage2ExploreAllSwapsAndElectiveBlockMove는 전수 탐색(MOVE/SWAP/TRIANGLE/CROSS_WEEK/BLOCK_MOVE)과
// 품질 점수 정렬 및 고교학점제 선택블록·2연강 블록 통째 이동을 검증합니다.
func TestStage2ExploreAllSwapsAndElectiveBlockMove(t *testing.T) {
	store, bus := setupTestEnv(t)
	clearStage2Collections(t, store)
	authSvc := auth.NewService(store)
	ttSvc := timetable.NewService(store, bus)

	saveTestTeacher(t, authSvc, "ts1", "교사1", "국어", nil)
	saveTestTeacher(t, authSvc, "ts2", "교사2", "수학", nil)
	saveTestTeacher(t, authSvc, "ts3", "교사3", "영어", nil)
	saveTestTeacher(t, authSvc, "ts4", "교사4", "과학", nil)

	// 1-1: 월1(ts1), 화1(ts2), 수1(ts3); 1-2: 화1(ts1) -> 월1↔화1 1:1은 불가하지만 월1→수1→화1 삼각 및 다주간/빈교시 탐색 가능
	mustSave := func(c protocol.TimetableCell) {
		if _, err := ttSvc.SaveCell(c); err != nil {
			t.Fatalf("셀 저장 실패(%s): %v", c.ID, err)
		}
	}
	mustSave(protocol.TimetableCell{ID: "c-1-1-1-1", DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1, Subject: "국어", TeacherID: "ts1"})
	mustSave(protocol.TimetableCell{ID: "c-2-1-1-1", DayOfWeek: 2, Period: 1, ClassName: "1-1", Grade: 1, Subject: "수학", TeacherID: "ts2"})
	mustSave(protocol.TimetableCell{ID: "c-3-1-1-1", DayOfWeek: 3, Period: 1, ClassName: "1-1", Grade: 1, Subject: "영어", TeacherID: "ts3"})
	mustSave(protocol.TimetableCell{ID: "c-2-1-1-2", DayOfWeek: 2, Period: 1, ClassName: "1-2", Grade: 1, Subject: "국어", TeacherID: "ts1"})
	mustSave(protocol.TimetableCell{ID: "c-4-1-1-1", DayOfWeek: 4, Period: 1, ClassName: "1-1", Grade: 1, Subject: "과학", TeacherID: "ts4"})

	exp, err := ttSvc.ExploreSwapOptions(protocol.ExploreSwapsRequest{
		SourceCellID:   "c-1-1-1-1",
		Scope:          "DATE_ONLY",
		WeekStart:      "2026-11-23",
		IncludeCrossWk: true,
		MaxCrossWeeks:  2,
		IncludeBlocks:  true,
		MaxResults:     100,
	})
	if err != nil {
		t.Fatalf("전수 탐색 실패: %v", err)
	}
	if exp.Counts["MOVE"] == 0 || exp.Counts["TRIANGLE"] == 0 || exp.Counts["CROSS_WEEK"] == 0 {
		t.Fatalf("전수 탐색 유형별 결과 부족: counts=%+v", exp.Counts)
	}
	for i := 1; i < len(exp.Options); i++ {
		if exp.Options[i-1].Score < exp.Options[i].Score {
			t.Fatalf("전수 탐색 결과가 품질 점수 내림차순이 아닙니다: [%d]=%d < [%d]=%d", i-1, exp.Options[i-1].Score, i, exp.Options[i].Score)
		}
	}

	// 고교학점제 선택블록 통째 이동 (2-1, 2-2) 및 2연강 통째 이동 검증
	mustSave(protocol.TimetableCell{ID: "el-1", DayOfWeek: 4, Period: 2, ClassName: "2-1", Grade: 2, Subject: "심화수학", TeacherID: "ts2", ElectiveGroup: "EL-A"})
	mustSave(protocol.TimetableCell{ID: "el-2", DayOfWeek: 4, Period: 2, ClassName: "2-2", Grade: 2, Subject: "물리탐구", TeacherID: "ts4", ElectiveGroup: "EL-A"})
	mustSave(protocol.TimetableCell{ID: "blk-1", DayOfWeek: 5, Period: 1, ClassName: "2-1", Grade: 2, Subject: "국어", TeacherID: "ts1", BlockGroupID: "BG-1"})
	mustSave(protocol.TimetableCell{ID: "blk-2", DayOfWeek: 5, Period: 2, ClassName: "2-1", Grade: 2, Subject: "국어", TeacherID: "ts1", BlockGroupID: "BG-1"})

	soloRes, _ := ttSvc.EvaluateMove(protocol.EvaluateMoveRequest{SourceCellID: "el-1", TargetDay: 5, TargetPeriod: 3, MoveWholeBlock: false})
	if soloRes.Signal != "RED" {
		t.Fatalf("선택블록 단독 이동은 차단되어야 합니다: %+v", soloRes)
	}
	blockRes, _ := ttSvc.EvaluateMove(protocol.EvaluateMoveRequest{SourceCellID: "el-1", TargetDay: 5, TargetPeriod: 3, MoveWholeBlock: true})
	if blockRes.Signal != "GREEN" || blockRes.Mode != "BLOCK_MOVE" || len(blockRes.ChainSteps) != 2 {
		t.Fatalf("선택블록 통째 이동 실패: %+v", blockRes)
	}
	soloBlockRes, _ := ttSvc.EvaluateMove(protocol.EvaluateMoveRequest{SourceCellID: "blk-1", TargetDay: 3, TargetPeriod: 2, MoveWholeBlock: false})
	if soloBlockRes.Signal != "RED" {
		t.Fatalf("2연강 블록 단독 이동은 차단되어야 합니다: %+v", soloBlockRes)
	}
	seqRes, _ := ttSvc.EvaluateMove(protocol.EvaluateMoveRequest{SourceCellID: "blk-1", TargetDay: 3, TargetPeriod: 2, MoveWholeBlock: true})
	if seqRes.Signal != "GREEN" || seqRes.Mode != "BLOCK_MOVE" || len(seqRes.ChainSteps) != 2 {
		t.Fatalf("2연강 블록 통째 이동 실패: %+v", seqRes)
	}
}

// TestStage2FETEjectionChainDiagnosticsRoomsAndCoTeaching는 FET 재귀 연쇄 복구, 미배정 역추적 진단,
// 교과교실제 On/Off·동시 수용 한도(체육관 2학급), 3인 이상 코티칭 충돌 검사를 검증합니다.
func TestStage2FETEjectionChainDiagnosticsRoomsAndCoTeaching(t *testing.T) {
	store, bus := setupTestEnv(t)
	clearStage2Collections(t, store)
	authSvc := auth.NewService(store)
	ttSvc := timetable.NewService(store, bus)

	saveTestTeacher(t, authSvc, "tf_blk2", "예정교사2", "물리", []int{2})
	saveTestTeacher(t, authSvc, "tf_blk1", "예정교사1", "화학", nil)
	saveTestTeacher(t, authSvc, "tf_last", "후순위교사", "미술", nil)
	saveTestTeacher(t, authSvc, "tf_co1", "코티처1", "융합", nil)
	saveTestTeacher(t, authSvc, "tf_co2", "코티처2", "융합", nil)
	saveTestTeacher(t, authSvc, "tf_co3", "코티처3", "융합", nil)

	// 1. 체육관(MaxConcurrent=2) 동시 2반 허용/3반 차단 및 교과교실제 Off 검증
	for i, cls := range []string{"1-1", "1-2"} {
		tid := []string{"tf_co1", "tf_co2"}[i]
		if _, err := ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 4, ClassName: cls, Grade: 1, Subject: "체육", TeacherID: tid, Room: "체육관"}); err != nil {
			t.Fatalf("체육관 %d반 동시 배정 실패: %v", i+1, err)
		}
	}
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 4, ClassName: "1-3", Grade: 1, Subject: "체육", TeacherID: "tf_co3", Room: "체육관"}); err == nil {
		t.Fatalf("체육관 동시 수용 한도(2학급) 초과 3학급 배정이 차단되지 않았습니다")
	}

	// 2. 3인 코티칭 충돌 검증
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 4, ClassName: "1-1", Grade: 1, Subject: "융합",
		TeacherID: "tf_co1", CoTeacherID: "tf_co2", CoTeacherIDs: []string{"tf_co3"},
	}); err != nil {
		t.Fatalf("3인 코티칭 셀 저장 실패: %v", err)
	}
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 4, ClassName: "1-2", Grade: 1, Subject: "탐구", TeacherID: "tf_co3",
	}); err == nil {
		t.Fatalf("3번째 코티칭 교사의 동시간대 타 학급 배정이 차단되지 않았습니다")
	}

	// 교과교실제 Off 전환 후 특별실 제한 비활성 확인
	cfg, _ := ttSvc.GetSchoolConfig()
	off := false
	cfg.SubjectClassroomMode = &off
	cfg.GradeClassCounts = map[int]int{1: 1, 2: 0, 3: 0}
	cfg.DailyPeriodLimits = map[int]map[int]int{1: {1: 2, 2: 2, 3: 1, 4: 1, 5: 1}}
	if _, err := ttSvc.SaveSchoolConfig(cfg); err != nil {
		t.Fatalf("학교 설정 저장 실패: %v", err)
	}
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 4, ClassName: "1-3", Grade: 1, Subject: "체육", TeacherID: "tf_co3", Room: "체육관"}); err != nil {
		t.Fatalf("교과교실제 Off 상태에서 교실 경합 해제가 적용되지 않았습니다: %v", err)
	}

	// 3. FET depth-2 재귀 연쇄 복구 검증 (tf_blk2: 화1->화2, tf_blk1: 월1->화1, tf_last: 월1)
	allowOnly := func(tid string, allowed [][2]int) {
		okSet := make(map[int]bool)
		for _, a := range allowed {
			okSet[a[0]*10+a[1]] = true
		}
		for d := 1; d <= 5; d++ {
			for p := 1; p <= 2; p++ {
				if !okSet[d*10+p] {
					if _, err := ttSvc.SaveConstraint(protocol.TeacherConstraint{
						TeacherID: tid, DayOfWeek: d, Period: p, ConstraintType: protocol.ConstraintHardAvoid, Reason: "테스트 제한",
					}, "테스트"); err != nil {
						t.Fatalf("제약 저장 실패: %v", err)
					}
				}
			}
		}
	}
	allowOnly("tf_blk2", [][2]int{{2, 1}, {2, 2}})
	allowOnly("tf_blk1", [][2]int{{1, 1}, {2, 1}})
	allowOnly("tf_last", [][2]int{{1, 1}})

	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{TeacherID: "tf_blk2", Grade: 1, Subject: "물리", DedicatedRoom: "과학실1", ClassHours: map[string]int{"1-1": 1}})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{TeacherID: "tf_blk1", Grade: 1, Subject: "화학", DedicatedRoom: "과학실2", ClassHours: map[string]int{"1-1": 1}})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{TeacherID: "tf_last", Grade: 1, Subject: "미술", ClassHours: map[string]int{"1-1": 1}})

	res, err := ttSvc.AutoGenerateFromCurriculumDetailed(true)
	if err != nil {
		t.Fatalf("FET 자동 편성 실패: %v", err)
	}
	if res.UnplacedCount != 0 || res.EjectionRepairedCount < 1 {
		t.Fatalf("FET 다단계 재귀 교환 복구 실패: unplaced=%d, repaired=%d, warnings=%v", res.UnplacedCount, res.EjectionRepairedCount, res.Warnings)
	}

	// 4. 불가능 시수 추가 시 미배정 원인 역추적 진단 반환 검증
	saveTestTeacher(t, authSvc, "tf_imp", "불가교사", "도덕", nil)
	allowOnly("tf_imp", [][2]int{{1, 1}})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{TeacherID: "tf_imp", Grade: 1, Subject: "도덕", ClassHours: map[string]int{"1-1": 1}})
	resImp, err := ttSvc.AutoGenerateFromCurriculumDetailed(true)
	if err != nil {
		t.Fatalf("불가능 시수 진단 실행 실패: %v", err)
	}
	if resImp.UnplacedCount == 0 || len(resImp.Diagnostics) == 0 || resImp.Diagnostics[0].PrimaryCause == "" {
		t.Fatalf("미배정 시수 역추적 진단이 생성되지 않았습니다: %+v", resImp)
	}
}

// TestStage2CreativeActivitiesWorkflowLockAndSemesterLedger는 창체 동시/고정 편성,
// 3단계 워크플로우 LOCKED 잠금 보호, 학기 시수 확보 원장 및 미보충 부채 해소를 검증합니다.
func TestStage2CreativeActivitiesWorkflowLockAndSemesterLedger(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)

	// 1. 창체 동시/고정 편성 및 LOCKED 잠금 검증
	applied, err := ttSvc.ApplyCreativeActivities([]protocol.CreativeActivitySlot{
		{Title: "창체(동아리)", Grade: 1, DayOfWeek: 5, Period: 6, Locked: true, Simultaneous: true},
	}, "시간표담당")
	if err != nil || len(applied) == 0 {
		t.Fatalf("창체 고정 편성 실패: len=%d, err=%v", len(applied), err)
	}
	for _, c := range applied {
		if !c.IsLocked || c.SubjectCode != "창체" {
			t.Fatalf("창체 셀 잠금 또는 코드 설정 오류: %+v", c)
		}
	}

	if wf, err := ttSvc.SetWorkflowPhase(protocol.WorkflowLocked, "시간표담당", "확정"); err != nil || wf.Phase != protocol.WorkflowLocked {
		t.Fatalf("워크플로우 잠금 실패: %+v, err=%v", wf, err)
	}
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 7, ClassName: "1-1", Grade: 1, Subject: "특강", TeacherID: "t02"}); err == nil {
		t.Fatalf("LOCKED 상태에서 기초 시간표 셀 수정이 차단되지 않았습니다")
	}

	cells, _ := ttSvc.ListCells()
	var c1, c2 protocol.TimetableCell
	for _, c := range cells {
		if c.ClassName == "1-1" && !c.IsLocked {
			if c1.ID == "" {
				c1 = c
			} else if c2.ID == "" {
				c2 = c
				break
			}
		}
	}
	reqOneOff, _ := ttSvc.SubmitRequest(protocol.TimetableChangeRequest{
		RequestType: "SWAP", Scope: "DATE_ONLY", SourceDate: "2026-11-23", TargetDate: "2026-11-23",
		RequesterID: c1.TeacherID, PartnerID: c2.TeacherID, SourceCell: c1, TargetCell: c2, Reason: "확정 후 일회성 교환",
	})
	if _, err := ttSvc.CommitRequest(reqOneOff.ID, "시간표담당", true, ""); err != nil {
		t.Fatalf("LOCKED 상태에서도 일회성 변경은 허용되어야 합니다: %v", err)
	}

	// 2. 학기 시수 원장 및 행사 결손 부채 해소 검증
	_, _ = ttSvc.SaveSemesterLedgerConfig(protocol.SemesterLedgerConfig{SemesterName: "1학기", TotalWeeks: 17, MinRatioPct: 95, IncludeCurrent: true})
	ev, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: fmt.Sprintf("evt-ldg-%s", c1.ClassName), Title: "학년 체육대회", Department: "체육부",
		EventDate: "2026-11-25", StartPeriod: c1.Period, EndPeriod: c1.Period,
		TargetClasses: []string{c1.ClassName}, LinkType: protocol.EventLinkClassBlock,
	})
	if err == nil {
		_, _ = calSvc.CommitEvent(ev.ID, "행사담당", true)
	}
	// 수요일(2026-11-25)에 해당하는 셀이 없을 수도 있으므로 c1의 요일에 맞춘 행사 날짜(월=23..금=27) 선택
	wdDate := timetable.DateForWeekday("2026-11-23", c1.DayOfWeek)
	ev2, _ := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-ldg-exact", Title: "진로특강", Department: "진로부",
		EventDate: wdDate, StartPeriod: c1.Period, EndPeriod: c1.Period,
		TargetClasses: []string{c1.ClassName}, LinkType: protocol.EventLinkClassBlock,
	})
	_, _ = calSvc.CommitEvent(ev2.ID, "행사담당", true)

	report, err := ttSvc.CalculateSemesterLedger()
	if err != nil || len(report.Debts) == 0 {
		t.Fatalf("학기 시수 원장 부채 추적 실패: debts=%d, err=%v", len(report.Debts), err)
	}
	targetDebtID := "debt-evt-evt-ldg-exact-" + c1.ID
	if err := ttSvc.SetMakeupDebtResolved(targetDebtID, true); err != nil {
		t.Fatalf("미보충 부채 해결 처리 실패: %v", err)
	}
	report2, _ := ttSvc.CalculateSemesterLedger()
	if report2.Summary["unresolvedDebtCount"] >= report.Summary["unresolvedDebtCount"] {
		t.Fatalf("부채 해결 후 미해결 건수가 감소하지 않았습니다: before=%d, after=%d",
			report.Summary["unresolvedDebtCount"], report2.Summary["unresolvedDebtCount"])
	}
}
