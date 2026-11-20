package modules_test

import (
	"slices"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/stats"
)

func TestPhase4MultiDayExamNoConsecutiveAndPerDayUnseal(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := internal_exam.NewService(store, bus)

	// 2026-11-23(월) ~ 2026-11-26(목) 4일 연속 지필고사, 연강 금지 및 1일 최대 2교시 제한
	dates := []string{"2026-11-23", "2026-11-24", "2026-11-25", "2026-11-26"}
	cfg := protocol.ExamConfig{
		ID:                   "exam-4day-final",
		Title:                "2026학년도 2학기 4일 통합 지필고사",
		ExamDate:             dates[0],
		ExamDates:            dates,
		UnsealTime:           "08:20",
		IsSealed:             true,
		DeputyMode:           false,
		BalanceMode:          "TOTAL_AND_ROLE",
		AvoidConsecutive:     true,
		MaxDailyPerTeacher:   2,
		SubjectTeacherPolicy: protocol.SubjectModeCorridorFirst,
	}
	daySpecs := [][3]struct {
		grade   int
		subject string
		rooms   []string
	}{
		{
			{1, "국어", []string{"1-1", "1-2"}},
			{2, "수학", []string{"2-1", "2-2"}},
			{3, "영어", []string{"3-1", "3-2"}},
		},
		{
			{1, "사회", []string{"1-1", "1-2"}},
			{2, "과학", []string{"2-1", "2-2"}},
			{3, "역사", []string{"3-1", "3-2"}},
		},
		{
			{1, "수학", []string{"1-1", "1-2"}},
			{2, "영어", []string{"2-1", "2-2"}},
			{3, "국어", []string{"3-1", "3-2"}},
		},
		{
			{1, "과학", []string{"1-1", "1-2"}},
			{2, "사회", []string{"2-1", "2-2"}},
			{3, "정보", []string{"3-1", "3-2"}},
		},
	}

	var specs []internal_exam.ExamPeriodSpec
	for dIdx, d := range dates {
		for p := 1; p <= 3; p++ {
			sp := daySpecs[dIdx][p-1]
			specs = append(specs, internal_exam.ExamPeriodSpec{
				ExamDate:        d,
				Period:          p,
				Grade:           sp.grade,
				Subject:         sp.subject,
				StartTime:       "08:50",
				EndTime:         "09:35",
				DurationMinutes: 45,
				ClassRooms:      sp.rooms,
			})
		}
	}

	slots, err := svc.GenerateAndSave(cfg, specs)
	if err != nil {
		t.Fatalf("다중일 지필고사 통합 편성 실패: %v", err)
	}
	if len(slots) != 36 {
		t.Fatalf("예상 슬롯 개수 불일치: got=%d, want=36", len(slots))
	}

	// 1. 슬롯 ID 유일성 및 순회교사(t11: 월·수·금 출강) 화/목 배제 검증
	seenIDs := make(map[string]bool, len(slots))
	seenDates := make(map[string]bool)
	for _, sl := range slots {
		if seenIDs[sl.ID] {
			t.Fatalf("중복된 시험감독 슬롯 ID 발견: %s", sl.ID)
		}
		seenIDs[sl.ID] = true
		seenDates[sl.ExamDate] = true
		if sl.TeacherID == "t11" && (sl.ExamDate == "2026-11-24" || sl.ExamDate == "2026-11-26") {
			t.Fatalf("순회교사(t11·월수금)가 비출강일(%s)에 감독 배정되었습니다: %+v", sl.ExamDate, sl)
		}
	}
	if len(seenDates) != 4 {
		t.Fatalf("4개 시험 일자에 분산 배정되지 않았습니다: dates=%v", seenDates)
	}

	// 2. 연강 감독 0건, 1일 최대 <= 2교시, 연간 시수 편차 <= 1 검증
	fairness, err := svc.CalculateFairnessSummary()
	if err != nil {
		t.Fatalf("공정성 통계 계산 실패: %v", err)
	}
	if fairness.ExamDayCount != 4 || fairness.ConsecutivePairCount != 0 || fairness.MaxDailyAssigned > 2 || fairness.MaxGap > 1 {
		t.Fatalf("다중일 제약·균등화 위반: days=%d, consec=%d, maxDaily=%d, maxGap=%d",
			fairness.ExamDayCount, fairness.ConsecutivePairCount, fairness.MaxDailyAssigned, fairness.MaxGap)
	}

	// 3. 일자별 부분 공개: 1일차만 공개 -> 일반 교사 조회 시 1일차만 보이고 2~4일차는 봉인 유지
	partCfg, err := svc.UnsealExamDate("2026-11-23", "박평가")
	if err != nil {
		t.Fatalf("1일차 부분 공개 실패: %v", err)
	}
	if !partCfg.IsSealed || !slices.Contains(partCfg.UnsealedDates, "2026-11-23") {
		t.Fatalf("1일차만 공개했는데 전체 봉인 상태가 깨졌습니다: %+v", partCfg)
	}
	teacherView, _, err := svc.ListSlotsForViewer(false)
	if err != nil {
		t.Fatalf("일반 교사 슬롯 조회 실패: %v", err)
	}
	for _, sl := range teacherView {
		if sl.ExamDate == "2026-11-23" && sl.ActualRoomHidden {
			t.Fatalf("공개된 1일차 슬롯이 여전히 마스킹되어 있습니다: %+v", sl)
		}
		if sl.ExamDate != "2026-11-23" && (!sl.ActualRoomHidden || !strings.Contains(sl.RoomName, "🔒")) {
			t.Fatalf("미공개 일자(%s) 고사장이 일반 교사에게 노출되었습니다: %+v", sl.ExamDate, sl)
		}
	}

	// 4. 나머지 3개 일자 순차 공개 완료 시 전체 IsSealed=false 전환 확인
	for _, d := range dates[1:] {
		partCfg, err = svc.UnsealExamDate(d, "박평가")
		if err != nil {
			t.Fatalf("%s 부분 공개 실패: %v", d, err)
		}
	}
	if partCfg.IsSealed {
		t.Fatalf("모든 시험일이 공개되었으나 IsSealed가 false로 전환되지 않았습니다")
	}
}

func TestPhase4PeriodStatsAggregation(t *testing.T) {
	store, _ := setupTestEnv(t)
	statsSvc := stats.NewService(store)

	// 1. 월·학기·학년도·윤년 2월 말일·직접지정 범위 계산 검증
	_, _, s1, e1 := stats.ResolvePeriodRange("SEMESTER_1", 2026, 0, "", "")
	if s1 != "2026-03-01" || e1 != "2026-08-31" {
		t.Fatalf("1학기 기간 오류: %s ~ %s", s1, e1)
	}
	_, _, s2Leap, e2Leap := stats.ResolvePeriodRange("SEMESTER_2", 2023, 0, "", "")
	if s2Leap != "2023-09-01" || e2Leap != "2024-02-29" {
		t.Fatalf("윤년 2학기 기간 오류: %s ~ %s", s2Leap, e2Leap)
	}
	_, _, s2Norm, e2Norm := stats.ResolvePeriodRange("SEMESTER_2", 2026, 0, "", "")
	if s2Norm != "2026-09-01" || e2Norm != "2027-02-28" {
		t.Fatalf("평년 2학기 기간 오류: %s ~ %s", s2Norm, e2Norm)
	}
	_, _, cS, cE := stats.ResolvePeriodRange("CUSTOM", 0, 0, "2026-10-20", "2026-10-05")
	if cS != "2026-10-05" || cE != "2026-10-20" {
		t.Fatalf("역전된 사용자 기간 보정 오류: %s ~ %s", cS, cE)
	}

	// 2. 격리된 기간(2026-10 vs 2026-04) 샘플 실적 생성 및 집계 검증
	if err := store.WithTx(func(tx *database.TxStore) error {
		if err := tx.ClearCollections("timetable_requests", "exam_slots", "external_exams", "school_events"); err != nil {
			return err
		}
		reqs := []protocol.TimetableChangeRequest{
			{
				ID: "rq-oct-swap", RequestType: "SWAP", TargetDate: "2026-10-12",
				RequesterID: "t06", PartnerID: "t07", Status: protocol.StatusApproved,
				SourceCell: protocol.TimetableCell{TeacherID: "t06", ClassName: "1-1", Period: 1, Subject: "국어"},
				TargetCell: protocol.TimetableCell{Subject: "수학"}, Reason: "교환",
			},
			{
				ID: "rq-oct-sub", RequestType: "ABSENCE_SUBSTITUTE", TargetDate: "2026-10-15",
				RequesterID: "t06", PartnerID: "t08", Status: protocol.StatusApproved,
				SourceCell: protocol.TimetableCell{TeacherID: "t06", ClassName: "1-2", Period: 2, Subject: "국어"},
				TargetCell: protocol.TimetableCell{Subject: "보강"}, Reason: "병가 대타",
			},
			{
				ID: "rq-oct-staging", RequestType: "SWAP", TargetDate: "2026-10-16",
				RequesterID: "t06", PartnerID: "t07", Status: protocol.StatusStaging,
				SourceCell: protocol.TimetableCell{TeacherID: "t06", ClassName: "1-1", Period: 3, Subject: "국어"},
			},
			{
				ID: "rq-apr-swap", RequestType: "SWAP", TargetDate: "2026-04-10",
				RequesterID: "t06", PartnerID: "t07", Status: protocol.StatusApproved,
				SourceCell: protocol.TimetableCell{TeacherID: "t06", ClassName: "1-1", Period: 1, Subject: "국어"},
			},
		}
		for _, rq := range reqs {
			if err := tx.SaveRecord("timetable_requests", rq.ID, rq); err != nil {
				return err
			}
		}
		if err := tx.SaveRecord("exam_slots", "ex-oct-1", protocol.ExamSlot{
			ID: "ex-oct-1", ExamDate: "2026-10-22", Period: 1, Grade: 1, Subject: "국어", SlotRole: "MAIN", TeacherID: "t08", TeacherName: "윤영어", RoomName: "1-1 고사장",
		}); err != nil {
			return err
		}
		if err := tx.SaveRecord("external_exams", "ext-oct-1", protocol.ExternalExamNotice{
			ID: "ext-oct-1", Title: "10월 모평", ExamDate: "2026-10-28", IsConfirmed: true, ConfirmedIDs: []string{"t07"},
		}); err != nil {
			return err
		}
		return tx.SaveRecord("school_events", "ev-oct-1", protocol.SchoolEvent{
			ID: "ev-oct-1", Title: "가을 현장학습", EventDate: "2026-10-29", Department: "교무부", LinkType: protocol.EventLinkSimpleNotice,
			Status: protocol.StatusApproved, LeadTeachers: []string{"t05"},
		})
	}); err != nil {
		t.Fatalf("집계 테스트 데이터 저장 실패: %v", err)
	}

	octReport, err := statsSvc.BuildPeriodReport("MONTH", 2026, 10, "", "")
	if err != nil {
		t.Fatalf("10월 정산 조회 실패: %v", err)
	}
	if octReport.TotalChanges != 2 || octReport.TotalSubstitutes != 1 || octReport.TotalInternal != 1 || octReport.TotalExternal != 1 || octReport.TotalEvents != 1 {
		t.Fatalf("10월 통합 요약 수치 불일치: %+v", octReport)
	}
	if len(octReport.MonthlyTrend) != 1 || octReport.MonthlyTrend[0].Month != "2026-10" {
		t.Fatalf("월별 추이 오류: %+v", octReport.MonthlyTrend)
	}

	byID := make(map[string]protocol.TeacherPeriodStat)
	for _, st := range octReport.TeacherStats {
		byID[st.TeacherID] = st
	}
	if byID["t06"].SwapCount != 1 || byID["t06"].SubstituteTakenCount != 1 {
		t.Fatalf("t06 실적 집계 오류: %+v", byID["t06"])
	}
	if byID["t08"].SubstituteGivenCount != 1 || byID["t08"].InternalMainCount != 1 {
		t.Fatalf("t08 보강/감독 집계 오류: %+v", byID["t08"])
	}
	if byID["t07"].SwapCount != 1 || byID["t07"].ExternalExamCount != 1 {
		t.Fatalf("t07 교환/외부파견 집계 오류: %+v", byID["t07"])
	}
	if byID["t05"].EventLeadCount != 1 {
		t.Fatalf("t05 행사 인솔 집계 오류: %+v", byID["t05"])
	}

	sem2Report, err := statsSvc.BuildPeriodReport("SEMESTER_2", 2026, 0, "", "")
	if err != nil || sem2Report.TotalChanges != 2 {
		t.Fatalf("2학기 정산 조회 오류(4월 데이터 제외 확인): err=%v, changes=%d", err, sem2Report.TotalChanges)
	}
}
