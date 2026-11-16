package modules_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

// TestCurriculumLoadGenerationAndRoomConflict는 교사별 시수표 기반 시간표 자동 편성과
// 교과교실(특별실) 동시간대 중복 배정 차단을 검증합니다.
func TestCurriculumLoadGenerationAndRoomConflict(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	summary, err := ttSvc.ValidateCurriculum()
	if err != nil || len(summary.ClassStatuses) == 0 {
		t.Fatalf("시수표 일치성 검사 실패: err=%v, statuses=%+v", err, summary.ClassStatuses)
	}

	created, err := ttSvc.AutoGenerateFromCurriculum(true)
	if err != nil || len(created) == 0 {
		t.Fatalf("시수표 기반 자동 편성 실패: len=%d, err=%v", len(created), err)
	}

	// 배정된 셀 수가 시수표 총 배당 시수(summary.TotalAssigned)와 정확히 일치하는지 확인
	if len(created) != summary.TotalAssigned {
		t.Fatalf("시수표 총합(%d)과 편성된 수업 셀(%d)이 일치하지 않습니다", summary.TotalAssigned, len(created))
	}

	// 특별실/교과교실 동시간대 중복 배정이 0건인지 검증
	roomSlots := make(map[string]string)
	var sciSlot protocol.TimetableCell
	for _, c := range created {
		if c.Room == "과학실1" {
			sciSlot = c
		}
		if c.Room != "" && c.Room != c.ClassName+"교실" {
			k := fmt.Sprintf("%s-%d-%d", c.Room, c.DayOfWeek, c.Period)
			if prev, dup := roomSlots[k]; dup {
				t.Fatalf("교과교실(%s)이 동시간대(%s, %s)에 중복 배정되었습니다", c.Room, prev, c.ClassName)
			}
			roomSlots[k] = c.ClassName
		}
	}

	// 동일 시간대 과학실1에 다른 반 수동 배정 시 차단 검증
	if sciSlot.ID != "" {
		if _, err := ttSvc.SaveCell(protocol.TimetableCell{
			DayOfWeek: sciSlot.DayOfWeek,
			Period:    sciSlot.Period,
			ClassName: "3-2",
			Grade:     3,
			Subject:   "과학",
			TeacherID: "t04",
			Room:      "과학실1",
		}); err == nil {
			t.Fatalf("이미 사용 중인 교과교실(과학실1) 동시간대 중복 배정이 차단되지 않았습니다")
		}
	}
}

// TestTeachersAndLoadsExcelRoundTrip은 교사 명렬표 및 시수표 엑셀 다운로드/업로드 왕복을 검증합니다.
func TestTeachersAndLoadsExcelRoundTrip(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	tBytes, err := ttSvc.ExportTeachersXLSX()
	if err != nil || len(tBytes) == 0 {
		t.Fatalf("교사 명렬표 엑셀 내보내기 실패: %v", err)
	}
	tImported, err := ttSvc.ImportTeachersXLSX(bytes.NewReader(tBytes))
	if err != nil || tImported == 0 {
		t.Fatalf("교사 명렬표 엑셀 재업로드 실패: imported=%d, err=%v", tImported, err)
	}

	lBytes, err := ttSvc.ExportTeachingLoadsXLSX()
	if err != nil || len(lBytes) == 0 {
		t.Fatalf("교사별 시수표 엑셀 내보내기 실패: %v", err)
	}
	lImported, err := ttSvc.ImportTeachingLoadsXLSX(bytes.NewReader(lBytes))
	if err != nil || lImported == 0 {
		t.Fatalf("교사별 시수표 엑셀 재업로드 실패: imported=%d, err=%v", lImported, err)
	}
}

// TestSameNameDisambiguationAndQuadChainSteps는 동명이인 과목 기반 로그인 구분과
// 삼각(3단계)·사각(4단계) 연쇄 교환 경로 상세 반환 및 일괄 서버 반영을 검증합니다.
func TestSameNameDisambiguationAndQuadChainSteps(t *testing.T) {
	store, bus := setupTestEnv(t)
	authSvc := auth.NewService(store)
	ttSvc := timetable.NewService(store, bus)

	// 1. 동명이인(강국어: t06=국어, dup=수학) 추가 및 로그인 구분 검증
	if _, err := authSvc.SaveTeacher(protocol.Teacher{
		ID: "t-dup", Username: "kang_math", Name: "강국어", Subject: "수학", Position: "평교사", Roles: []protocol.Role{protocol.RoleTeacher},
	}, "1234"); err != nil {
		t.Fatalf("동명이인 교사 저장 실패: %v", err)
	}
	if _, err := authSvc.AuthenticateLogin("", "강국어", "", "1234"); err == nil || !strings.Contains(err.Error(), "동명이인") {
		t.Fatalf("과목 미지정 동명이인 로그인이 차단되지 않았습니다: %v", err)
	}
	uMath, err := authSvc.AuthenticateLogin("", "강국어", "수학", "1234")
	if err != nil || uMath.ID != "t-dup" {
		t.Fatalf("담당과목(수학) 기반 동명이인 로그인 실패: err=%v, user=%+v", err, uMath)
	}

	// 2. 사각(4단계) 연쇄 교환 테스트 시나리오 구성:
	// 3-2반: A(월1, tA) → D(월4, tD)로 이동하려 할 때,
	// - tA는 월4 다른반(2-1) 수업 중이라 1:1 불가
	// - 3-2 M1(월2, tM1)은 월4(D자리)가 비지만 월1(A자리)엔 2-1 수업 중이라 3단계 불가
	// - 3-2 M2(월3, tM2)는 월1(A자리)과 월4(D자리) 모두 비어 있으나 M2->D 경로에서 D(tD)는 월1 불가, M2는 월1 가능
	//   => A(월1)→M1(월2)→M2(월3)→D(월4)→A(월1) 또는 A→M1→D→M2→A 검증!
	teachers := map[string]protocol.Teacher{
		"tA":  {ID: "tA", Name: "교사A"},
		"tM1": {ID: "tM1", Name: "교사B"},
		"tM2": {ID: "tM2", Name: "교사C"},
		"tD":  {ID: "tD", Name: "교사D"},
	}
	cells := []protocol.TimetableCell{
		// 대상 학급 3-2 (4개 교시)
		{ID: "q-A", DayOfWeek: 1, Period: 1, ClassName: "3-2", Grade: 3, Subject: "국어", TeacherID: "tA"},
		{ID: "q-M1", DayOfWeek: 1, Period: 2, ClassName: "3-2", Grade: 3, Subject: "수학", TeacherID: "tM1"},
		{ID: "q-M2", DayOfWeek: 1, Period: 3, ClassName: "3-2", Grade: 3, Subject: "영어", TeacherID: "tM2"},
		{ID: "q-D", DayOfWeek: 1, Period: 4, ClassName: "3-2", Grade: 3, Subject: "사회", TeacherID: "tD"},
		// 제약 셀 (다른 반 2-1):
		// tA는 월3, 월4 불가 (월2 M1자리만 가능)
		{ID: "b-A-3", DayOfWeek: 1, Period: 3, ClassName: "2-1", Grade: 2, Subject: "국어", TeacherID: "tA"},
		{ID: "b-A-4", DayOfWeek: 1, Period: 4, ClassName: "2-1", Grade: 2, Subject: "국어", TeacherID: "tA"},
		// tM1은 월1, 월4 불가 (월3 M2자리만 가능)
		{ID: "b-M1-1", DayOfWeek: 1, Period: 1, ClassName: "2-1", Grade: 2, Subject: "수학", TeacherID: "tM1"},
		{ID: "b-M1-4", DayOfWeek: 1, Period: 4, ClassName: "2-1", Grade: 2, Subject: "수학", TeacherID: "tM1"},
		// tM2는 월1, 월2 불가 (월4 D자리만 가능)
		{ID: "b-M2-1", DayOfWeek: 1, Period: 1, ClassName: "2-1", Grade: 2, Subject: "영어", TeacherID: "tM2"},
		{ID: "b-M2-2", DayOfWeek: 1, Period: 2, ClassName: "2-1", Grade: 2, Subject: "영어", TeacherID: "tM2"},
		// tD는 월2, 월3 불가 (월1 A자리만 가능)
		{ID: "b-D-2", DayOfWeek: 1, Period: 2, ClassName: "2-1", Grade: 2, Subject: "사회", TeacherID: "tD"},
		{ID: "b-D-3", DayOfWeek: 1, Period: 3, ClassName: "2-1", Grade: 2, Subject: "사회", TeacherID: "tD"},
	}

	res := timetable.EvaluateMove(protocol.EvaluateMoveRequest{
		SourceCellID: "q-A", TargetDay: 1, TargetPeriod: 4,
	}, cells, teachers, nil)
	if res.Signal != "YELLOW" || res.Mode != "QUAD" || len(res.ChainSteps) != 4 {
		t.Fatalf("사각(4단계) 연쇄 교환 탐색 실패: %+v", res)
	}

	// DB 저장 후 CommitRequest 실행 시 4단계 모든 셀이 새 교시로 반영되는지 확인
	for _, c := range cells[:4] {
		_ = store.SaveRecord("timetable_cells", c.ID, c)
	}
	req, err := ttSvc.SubmitRequest(protocol.TimetableChangeRequest{
		RequestType: "QUAD", RequesterID: "tA", RequesterName: "교사A",
		SourceCell: cells[0], TargetCell: cells[3], ChainSteps: res.ChainSteps, Reason: "4단계 테스트",
	})
	if err != nil {
		t.Fatalf("4단계 연쇄 교환 대기열 등록 실패: %v", err)
	}
	if _, err := ttSvc.CommitRequest(req.ID, "이수업", true, ""); err != nil {
		t.Fatalf("4단계 연쇄 교환 확정 실패: %v", err)
	}
	afterCells, _ := ttSvc.ListCells()
	for _, c := range afterCells {
		if c.ID == "q-A" && c.Period != 2 {
			t.Fatalf("q-A가 2교시로 이동되지 않았습니다: %+v", c)
		}
		if c.ID == "q-D" && c.Period != 1 {
			t.Fatalf("q-D가 1교시로 이동되지 않았습니다: %+v", c)
		}
	}
}

// TestCrossWeekSwapAndMidWeekAbsencePlan은 이번 주↔다음 주·다다음 주 주차 간 맞교환과
// 주중 갑작스러운 미출근 시 다음주 교환·당일 공강교사 대타 일괄 처리를 검증합니다.
func TestCrossWeekSwapAndMidWeekAbsencePlan(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	// 1. 이번 주(2026-11-16 월 1교시 c-1-1-11: 강국어) ↔ 다음 주(2026-11-23 주간) 맞교환 탐색
	cands, err := ttSvc.FindCrossWeekSwapCandidates("c-1-1-11", "2026-11-16", "2026-11-23")
	if err != nil || len(cands) == 0 {
		t.Fatalf("주차 간 맞교환 후보 탐색 실패: len=%d, err=%v", len(cands), err)
	}
	var chosen *protocol.CrossWeekCandidate
	for i := range cands {
		if cands[i].TargetCell.ID == "c-1-2-11" {
			chosen = &cands[i]
			break
		}
	}
	if chosen == nil || chosen.TargetDate != "2026-11-23" {
		t.Fatalf("다음 주 2교시(c-1-2-11) 맞교환 후보가 누락되었습니다: %+v", cands)
	}

	req, err := ttSvc.SubmitRequest(protocol.TimetableChangeRequest{
		RequestType:   "CROSS_WEEK_SWAP",
		Scope:         "DATE_ONLY",
		SourceDate:    "2026-11-16",
		TargetDate:    chosen.TargetDate,
		RequesterID:   "t06",
		RequesterName: "강국어",
		PartnerID:     chosen.TargetCell.TeacherID,
		PartnerName:   "정행사",
		PartnerAgreed: true,
		SourceCell:    protocol.TimetableCell{ID: "c-1-1-11", DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1, Subject: "국어", SubjectCode: "KOR101", TeacherID: "t06", Room: "1-1교실"},
		TargetCell:    chosen.TargetCell,
		ChainSteps:    chosen.ChainSteps,
		Reason:        "이번주 불가로 다음주 교환",
	})
	if err != nil {
		t.Fatalf("주차 간 교환 등록 실패: %v", err)
	}
	if _, err := ttSvc.CommitRequest(req.ID, "이수업", true, ""); err != nil {
		t.Fatalf("주차 간 교환 승인 실패: %v", err)
	}

	// 이번 주(11/16), 다음 주(11/23), 다다음 주(11/30), 정규 기본 시간표 각각 검증
	w1, _ := ttSvc.ListWeekCells("2026-11-16")
	for _, c := range w1 {
		if c.ID == "c-1-1-11" && (c.TeacherID != "t05" || !strings.Contains(c.ChangeNote, "주차교환")) {
			t.Fatalf("이번 주 1교시에 주차 교환이 반영되지 않았습니다: %+v", c)
		}
	}
	w2, _ := ttSvc.ListWeekCells("2026-11-23")
	for _, c := range w2 {
		if c.ID == "c-1-2-11" && (c.TeacherID != "t06" || !strings.Contains(c.ChangeNote, "주차교환")) {
			t.Fatalf("다음 주 2교시에 주차 교환이 반영되지 않았습니다: %+v", c)
		}
	}
	w3, _ := ttSvc.ListWeekCells("2026-11-30")
	for _, c := range w3 {
		if c.ID == "c-1-1-11" && c.TeacherID != "t06" {
			t.Fatalf("다다음 주 시간표가 오염되었습니다: %+v", c)
		}
	}
	base, _ := ttSvc.ListCells()
	for _, c := range base {
		if c.ID == "c-1-1-11" && c.TeacherID != "t06" {
			t.Fatalf("정규 기본 시간표가 변경되었습니다: %+v", c)
		}
	}

	// 2. 주중 갑작스러운 미출근 (2026-11-18 수요일 t06 강국어): 다음주 교환 + 공강 보강 대책 수립 및 일괄 적용
	plans, err := ttSvc.PlanTeacherAbsence(protocol.AbsencePlanRequest{
		TeacherID: "t06", StartDate: "2026-11-18", EndDate: "2026-11-18", StartPeriod: 1, EndPeriod: 7, Reason: "긴급 병가",
	})
	if err != nil || len(plans) == 0 {
		t.Fatalf("주중 미출근 대책 산출 실패: len=%d, err=%v", len(plans), err)
	}
	slot := plans[0]
	if slot.Cell.ID != "c-3-4-31" || len(slot.CrossWeekOptions) == 0 || len(slot.SubstituteOptions) == 0 {
		t.Fatalf("수요일 4교시 결근 대책 후보가 부족합니다: %+v", slot)
	}
	sub := slot.SubstituteOptions[0]
	applied, err := ttSvc.ApplyAbsenceResolutions(protocol.AbsenceApplyRequest{
		RequesterID:   "t06",
		RequesterName: "강국어",
		AutoApprove:   true,
		Items: []protocol.AbsenceApplyItem{
			{
				Mode:        "SUBSTITUTE",
				SourceDate:  slot.LessonDate,
				TargetDate:  slot.LessonDate,
				SourceCell:  slot.Cell,
				TargetCell:  slot.Cell,
				PartnerID:   sub.TeacherID,
				PartnerName: sub.TeacherName,
				Reason:      "[긴급병가 보강]",
			},
		},
	}, "이수업", true)
	if err != nil || len(applied) != 1 || applied[0].Status != protocol.StatusApproved {
		t.Fatalf("미출근 일괄 즉시 반영 실패: len=%d, err=%v", len(applied), err)
	}

	w1After, _ := ttSvc.ListWeekCells("2026-11-16")
	for _, c := range w1After {
		if c.ID == "c-3-4-31" && (c.TeacherID != sub.TeacherID || !strings.Contains(c.ChangeNote, "일일보강")) {
			t.Fatalf("당일 일일 보강이 이번 주 시간표에 반영되지 않았습니다: %+v", c)
		}
	}
	w2After, _ := ttSvc.ListWeekCells("2026-11-23")
	for _, c := range w2After {
		if c.ID == "c-3-4-31" && c.TeacherID != "t06" {
			t.Fatalf("일일 보강이 다음 주 시간표까지 침범했습니다: %+v", c)
		}
	}
}

// TestInternalExamAnnualEqualizationAndExemption은 특수반 제외(0건 배제), 별도고사실 선택 개설(일반교사 1인 순환),
// 정감독 단독 및 정·부감독 동시 운영 시 전교원 연간 감독 시수 균등 배정을 검증합니다.
func TestInternalExamAnnualEqualizationAndExemption(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := internal_exam.NewService(store, bus)

	// 기실시 누적 0회 초기화 후 별도고사실 없는 6개 교시 편성안 준비
	if _, err := svc.UpdateTeacherExamPolicy(internal_exam.TeacherExamPolicyUpdate{ResetAllBaseCounts: true}); err != nil {
		t.Fatalf("기실시 0회 초기화 실패: %v", err)
	}
	specs := []internal_exam.ExamPeriodSpec{
		{Period: 1, Grade: 1, Subject: "국어", StartTime: "08:50", EndTime: "09:35", DurationMinutes: 45, ClassRooms: []string{"1-1", "1-2"}},
		{Period: 2, Grade: 2, Subject: "수학", StartTime: "09:50", EndTime: "10:40", DurationMinutes: 50, ClassRooms: []string{"2-1", "2-2"}},
		{Period: 3, Grade: 3, Subject: "영어", StartTime: "10:55", EndTime: "11:35", DurationMinutes: 40, ClassRooms: []string{"3-1", "3-2"}},
		{Period: 4, Grade: 1, Subject: "사회", StartTime: "13:00", EndTime: "13:45", DurationMinutes: 45, ClassRooms: []string{"1-1", "1-2"}},
		{Period: 5, Grade: 2, Subject: "과학", StartTime: "14:00", EndTime: "14:45", DurationMinutes: 45, ClassRooms: []string{"2-1", "2-2"}},
		{Period: 6, Grade: 3, Subject: "역사", StartTime: "15:00", EndTime: "15:40", DurationMinutes: 40, ClassRooms: []string{"3-1", "3-2"}},
	}

	// 1. 별도고사실 미개설 + 정감독 1인 단독 운영 (DeputyMode: false)
	singleSlots, err := svc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-eq-single", Title: "정감독 단독 균등 검증", ExamDate: "2026-11-21", DeputyMode: false, HonoraryParentOn: false,
	}, specs)
	if err != nil || len(singleSlots) == 0 {
		t.Fatalf("정감독 단독 배정 실패: %v", err)
	}
	for _, sl := range singleSlots {
		if sl.TeacherID == "t01" {
			t.Fatalf("시스템 기본 최고관리자(%s)가 단독 운영 감독에 배정되었습니다", sl.TeacherName)
		}
		if sl.TeacherID == "t15" {
			t.Fatalf("특수반 제외 교사(%s)가 단독 운영 감독에 배정되었습니다", sl.TeacherName)
		}
		if strings.Contains(sl.RoomName, "별도") {
			t.Fatalf("미개설 상태에서 별도고사실 슬롯이 생성되었습니다: %+v", sl)
		}
	}
	singleSum, err := svc.CalculateFairnessSummary()
	if err != nil {
		t.Fatalf("균등 현황 산출 실패: %v", err)
	}
	if singleSum.ExemptCount != 1 || singleSum.MaxGap > 1 {
		t.Fatalf("정감독 단독 연간 총 시수 불균형 또는 제외수 오류: exempt=%d, gap=%d (%d~%d)",
			singleSum.ExemptCount, singleSum.MaxGap, singleSum.MinTotal, singleSum.MaxTotal)
	}

	// 2. 1교시에 별도고사실(독감·감염병) 선택 개설 + 정·부감독 2인 공동 운영 (DeputyMode: true)
	specsWithIso := append([]internal_exam.ExamPeriodSpec{}, specs...)
	specsWithIso[0].ClassRooms = []string{"1-1", "1-2", "별도고사실(독감·감염병)"}
	deputySlots, err := svc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-eq-deputy", Title: "정·부감독 공동+별도고사실 검증", ExamDate: "2026-11-22", DeputyMode: true, HonoraryParentOn: false, BalanceMode: "TOTAL_AND_ROLE",
	}, specsWithIso)
	if err != nil || len(deputySlots) == 0 {
		t.Fatalf("정·부감독 배정 실패: %v", err)
	}
	isoCount := 0
	for _, sl := range deputySlots {
		if sl.TeacherID == "t15" {
			t.Fatalf("특수반 제외 교사(%s)가 정·부감독에 배정되었습니다", sl.TeacherName)
		}
		if strings.Contains(sl.RoomName, "별도고사실") {
			isoCount++
			if sl.SlotRole != "MAIN" {
				t.Fatalf("별도고사실은 1인 정감독만 배정되어야 합니다: %+v", sl)
			}
		}
	}
	if isoCount != 1 {
		t.Fatalf("개설된 별도고사실 정감독 배정 수 오류: got=%d, want=1", isoCount)
	}
	deputySum, err := svc.CalculateFairnessSummary()
	if err != nil {
		t.Fatalf("정·부감독 균등 현황 산출 실패: %v", err)
	}
	if deputySum.MaxGap > 1 || deputySum.MainGap > 1 {
		t.Fatalf("정·부감독 연간 시수 불균형: totalGap=%d (%d~%d), mainGap=%d (%d~%d)",
			deputySum.MaxGap, deputySum.MinTotal, deputySum.MaxTotal, deputySum.MainGap, deputySum.MinMain, deputySum.MaxMain)
	}

	// 3. 고교선택과목·교과교실·별도고사실(독감·감염병) 카탈로그 수집 및 고사실별 과목·담임 배제 검증
	cat := svc.BuildRoomCatalog()
	if len(cat.SpecialRooms) == 0 || len(cat.ElectiveRooms) == 0 || len(cat.IsolatedRooms) == 0 {
		t.Fatalf("고교선택과목/교과교실/별도고사실 카탈로그가 비어 있습니다: %+v", cat)
	}
	elecSlots, err := svc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-elective-room", Title: "선택과목·별도고사실 시험", ExamDate: "2026-11-23", DeputyMode: false,
	}, []internal_exam.ExamPeriodSpec{
		{Period: 1, Grade: 2, Subject: "심화수학, 영어", ClassRooms: []string{"세미나실A(국어)", "2-1(영어)", "별도고사실(독감·감염병)"}},
	})
	if err != nil || len(elecSlots) < 3 {
		t.Fatalf("선택과목·별도고사실 감독 배정 실패: len=%d, err=%v", len(elecSlots), err)
	}
	for _, sl := range elecSlots {
		if sl.SlotRole == "MAIN" && strings.HasPrefix(sl.RoomName, "세미나실A(국어)") && (sl.TeacherID == "t17" || sl.TeacherID == "t06") {
			t.Fatalf("국어 교과 담당(%s)이 세미나실A(국어) 선택과목 정감독에 배정되었습니다", sl.TeacherName)
		}
		if sl.SlotRole == "MAIN" && strings.HasPrefix(sl.RoomName, "2-1(영어)") && (sl.TeacherID == "t02" || sl.TeacherID == "t03" || sl.TeacherID == "t08") {
			t.Fatalf("2-1 담임 또는 영어 담당(%s)이 2-1(영어) 정감독에 배정되었습니다", sl.TeacherName)
		}
	}
}
