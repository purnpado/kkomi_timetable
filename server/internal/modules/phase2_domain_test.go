package modules_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/external_exam"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

func TestPhase2EventBellAndTimetableSeparation(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)

	// 1. 1일 수업블록 행사(TargetClasses: 1-1, 2026-11-18 수요일) 확정 시 정규 시간표는 무변경
	evBlock, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-p2-blk", Title: "1-1 진로체험", Department: "진로부",
		EventDate: "2026-11-18", StartPeriod: 1, EndPeriod: 2,
		TargetClasses: []string{"1-1"}, LinkType: protocol.EventLinkClassBlock,
	})
	if err != nil {
		t.Fatalf("수업블록 행사 등록 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(evBlock.ID, "정행사", true); err != nil {
		t.Fatalf("행사 확정 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(evBlock.ID, "정행사", true); err == nil {
		t.Fatalf("이중 행사 확정이 차단되지 않았습니다")
	}

	baseCells, _ := ttSvc.ListCells()
	for _, c := range baseCells {
		if c.BlockedBy == "1-1 진로체험" {
			t.Fatalf("단일 일자 행사가 정규 시간표 DB를 오염시켰습니다: %+v", c)
		}
	}
	wCur, _ := ttSvc.ListWeekCells("2026-11-16")
	blkHit := 0
	for _, c := range wCur {
		if c.DayOfWeek == 3 && (c.Period == 1 || c.Period == 2) && c.ClassName == "1-1" {
			if c.BlockedBy != "1-1 진로체험" {
				t.Fatalf("해당 주차 1-1 수요일 수업에 행사블록이 표시되지 않았습니다: %+v", c)
			}
			blkHit++
		} else if c.BlockedBy == "1-1 진로체험" {
			t.Fatalf("비대상 학급·교시(%s %d요일 %d교시)에 블록이 적용되었습니다", c.ClassName, c.DayOfWeek, c.Period)
		}
	}
	wNext, _ := ttSvc.ListWeekCells("2026-11-23")
	for _, c := range wNext {
		if c.BlockedBy == "1-1 진로체험" {
			t.Fatalf("다음 주 시간표에 지난주 1일 행사가 남아 있습니다")
		}
	}

	// 2. 1일 시종시간 변경 행사 확정 시 정규 bell_schedule 보존 & EffectiveBellSchedule만 동적 반영
	evBell, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-p2-bell", Title: "수능일 단축시종", Department: "교무부",
		EventDate: "2026-11-18", StartPeriod: 1, EndPeriod: 7,
		LinkType:     protocol.EventLinkBellChange,
		BellOverride: []protocol.BellPeriod{{Period: 1, Label: "1교시", StartTime: "09:00", EndTime: "09:40"}},
	})
	if err != nil {
		t.Fatalf("시종 변경 행사 등록 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(evBell.ID, "정행사", true); err != nil {
		t.Fatalf("시종 변경 행사 확정 실패: %v", err)
	}
	baseBells, _ := calSvc.ListBellSchedule()
	if baseBells[0].StartTime != "08:40" {
		t.Fatalf("1일 시종 변경 행사가 정규 시종표 마스터를 덮어썼습니다: %+v", baseBells[0])
	}
	effWed, _ := calSvc.EffectiveBellSchedule("2026-11-18")
	if effWed[0].StartTime != "09:00" {
		t.Fatalf("2026-11-18 실효 시종시간 미반영: %+v", effWed[0])
	}
	effThu, _ := calSvc.EffectiveBellSchedule("2026-11-19")
	if effThu[0].StartTime != "08:40" {
		t.Fatalf("다른 일자(2026-11-19) 시종시간이 오염되었습니다: %+v", effThu[0])
	}

	// 3. 인솔 출장 행사 확정(Scope=DATE_ONLY) 및 삭제 시 STAGING 대기열 자동 정리
	evTrip, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-p2-trip", Title: "체육대회 인솔", Department: "체육부",
		EventDate: "2026-11-16", StartPeriod: 1, EndPeriod: 1,
		LeadTeachers: []string{"t06"}, LinkType: protocol.EventLinkTeacherTrip,
	})
	if err != nil {
		t.Fatalf("출장 행사 등록 실패: %v", err)
	}
	_, _ = calSvc.CommitEvent(evTrip.ID, "정행사", true)
	reqs, _ := ttSvc.ListRequests()
	foundEvtTrip := false
	for _, rq := range reqs {
		if strings.HasPrefix(rq.ID, "evt-trip-evt-p2-trip-") {
			foundEvtTrip = true
			if rq.Scope != "DATE_ONLY" {
				t.Fatalf("행사 출장 이관 요청 Scope가 DATE_ONLY가 아닙니다: %+v", rq)
			}
		}
	}
	if !foundEvtTrip {
		t.Fatalf("행사 인솔 출장 결보강 대기열이 자동 생성되지 않았습니다")
	}
	if err := calSvc.DeleteEvent(evTrip.ID, "정행사"); err != nil {
		t.Fatalf("행사 삭제 실패: %v", err)
	}
	reqsAfterDel, _ := ttSvc.ListRequests()
	for _, rq := range reqsAfterDel {
		if strings.HasPrefix(rq.ID, "evt-trip-evt-p2-trip-") {
			t.Fatalf("삭제된 행사의 미승인 출장 대기열이 정리되지 않았습니다: %+v", rq)
		}
	}

	// 4. CommitRequest 이중 승인 차단 검증
	if _, err := ttSvc.CommitRequest("req-seed-1", "이수업", true, ""); err != nil {
		t.Fatalf("첫 승인 실패: %v", err)
	}
	if _, err := ttSvc.CommitRequest("req-seed-1", "이수업", true, ""); err == nil {
		t.Fatalf("이미 승인된 요청의 이중 승인이 차단되지 않았습니다")
	}
}

func TestPhase2CurriculumSimGroupLockedAndAbsenceSafety(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	// 동시수업 그룹 주 3시간 + 코티칭·2연강 + 고정 셀(IsLocked) 테스트
	_ = store.ClearCollections("teaching_loads", "timetable_cells")
	_ = store.SaveRecord("timetable_cells", "locked-11-mon1", protocol.TimetableCell{
		ID: "locked-11-mon1", DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1,
		Subject: "국어", TeacherID: "t06", Room: "1-1교실", IsLocked: true,
	})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{
		ID: "ld-locked-kor", TeacherID: "t06", Subject: "국어", Grade: 1,
		ClassHours: map[string]int{"1-1": 3}, BlockHours: 1,
	})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{
		ID: "ld-sim-a", TeacherID: "t02", Subject: "선택수학", Grade: 2,
		ClassHours: map[string]int{"2-1": 3}, BlockHours: 1, SimultaneousGroup: "G2-SYNC",
	})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{
		ID: "ld-sim-b", TeacherID: "t08", Subject: "선택영어", Grade: 2,
		ClassHours: map[string]int{"2-2": 3}, BlockHours: 1, SimultaneousGroup: "G2-SYNC",
	})
	_, _ = ttSvc.SaveTeachingLoad(protocol.TeachingLoad{
		ID: "ld-blk2", TeacherID: "t09", CoTeacherID: "t04", Subject: "과학탐구", Grade: 1,
		ClassHours: map[string]int{"1-2": 4}, BlockHours: 2, DedicatedRoom: "과학실1",
	})

	res, err := ttSvc.AutoGenerateFromCurriculumDetailed(true)
	if err != nil {
		t.Fatalf("상세 자동 편성 실패: %v", err)
	}
	if res.UnplacedCount != 0 {
		t.Fatalf("미배정 시수가 발생했습니다: %d (%v)", res.UnplacedCount, res.Warnings)
	}
	allCells, _ := ttSvc.ListCells()
	kor11Count := 0
	sim21Slots := map[string]bool{}
	sim22Slots := map[string]bool{}
	for _, c := range allCells {
		if c.TeacherID == "t01" || c.CoTeacherID == "t01" {
			t.Fatalf("시스템 기본 관리자(admin)가 시간표에 배정되었습니다")
		}
		if c.ClassName == "1-1" && c.Subject == "국어" {
			kor11Count++
		}
		if c.ClassName == "2-1" && c.ElectiveGroup == "G2-SYNC" {
			sim21Slots[fmt.Sprintf("%d-%d", c.DayOfWeek, c.Period)] = true
		}
		if c.ClassName == "2-2" && c.ElectiveGroup == "G2-SYNC" {
			sim22Slots[fmt.Sprintf("%d-%d", c.DayOfWeek, c.Period)] = true
		}
	}
	if kor11Count != 3 {
		t.Fatalf("IsLocked 셀 시수 차감 오류 (want 3, got %d)", kor11Count)
	}
	if len(sim21Slots) != 3 || len(sim22Slots) != 3 {
		t.Fatalf("동시수업 그룹 주 3시간이 손실되었습니다: 2-1=%d, 2-2=%d", len(sim21Slots), len(sim22Slots))
	}
	for slot := range sim21Slots {
		if !sim22Slots[slot] {
			t.Fatalf("동시수업 그룹 2-1(%s)과 2-2가 같은 교시에 배치되지 않았습니다", slot)
		}
	}

	// 결근 대책 수립 시 admin 추천 제외 + 자기 자신 주차교환 차단 + 일괄 적용 중복 대타 차단
	_ = store.SaveRecord("timetable_cells", "c-3-1-11", protocol.TimetableCell{
		ID: "c-3-1-11", DayOfWeek: 3, Period: 1, ClassName: "1-1", Grade: 1, Subject: "국어", TeacherID: "t06", Room: "1-1교실",
	})
	_ = store.SaveRecord("timetable_cells", "c-3-2-11", protocol.TimetableCell{
		ID: "c-3-2-11", DayOfWeek: 3, Period: 2, ClassName: "1-1", Grade: 1, Subject: "수학", TeacherID: "t02", Room: "1-1교실",
	})
	_ = store.SaveRecord("timetable_cells", "c-4-3-11", protocol.TimetableCell{
		ID: "c-4-3-11", DayOfWeek: 4, Period: 3, ClassName: "1-1", Grade: 1, Subject: "국어", TeacherID: "t06", Room: "1-1교실",
	})
	plans, err := ttSvc.PlanTeacherAbsence(protocol.AbsencePlanRequest{
		TeacherID: "t06", StartDate: "2026-11-18", EndDate: "2026-11-18", StartPeriod: 1, EndPeriod: 7,
	})
	if err != nil || len(plans) == 0 {
		t.Fatalf("결근 대책 수립 실패: %v", err)
	}
	for _, cw := range plans[0].CrossWeekOptions {
		if cw.TargetCell.TeacherID == "t06" {
			t.Fatalf("결근 교사 자기 자신과의 다음주 맞교환이 추천되었습니다: %+v", cw)
		}
	}
	for _, sub := range plans[0].SubstituteOptions {
		if sub.TeacherID == "t01" {
			t.Fatalf("결근 대타 후보에 admin 계정이 포함되었습니다")
		}
	}
	// 같은 날짜·교시 동일 대타 중복 일괄 적용 차단
	dupCell := plans[0].Cell
	if _, err := ttSvc.ApplyAbsenceResolutions(protocol.AbsenceApplyRequest{
		RequesterID: "t06", RequesterName: "강국어",
		Items: []protocol.AbsenceApplyItem{
			{Mode: "SUBSTITUTE", SourceDate: "2026-11-18", TargetDate: "2026-11-18", SourceCell: dupCell, PartnerID: "t03", PartnerName: "박평가"},
			{Mode: "SUBSTITUTE", SourceDate: "2026-11-18", TargetDate: "2026-11-18", SourceCell: dupCell, PartnerID: "t03", PartnerName: "박평가"},
		},
	}, "이수업", true); err == nil {
		t.Fatalf("동일 일자·교시 대타 교사 중복 배정이 차단되지 않았습니다")
	}
}

func TestPhase2ExamConcurrencyUnavailMaskAndIdempotency(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	extSvc := external_exam.NewService(store, bus)
	examSvc := internal_exam.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)
	_ = ttSvc

	// 2026-11-17 화요일: t11(순회 월·수·금만 출강), t07(외부감독 확정), t08(행사 인솔 출장 확정)
	extN, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-tue", Title: "화요일 모의고사", ExamCategory: "모의평가",
		ExamDate: "2026-11-17", RequiredCount: 1,
	})
	if _, err := extSvc.ConfirmDispatch(extN.ID, []string{"t07"}, "최외부"); err != nil {
		t.Fatalf("외부시험 파견 확정 실패: %v", err)
	}
	evTrip, _ := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-tue-trip", Title: "화요일 출장", Department: "연구부",
		EventDate: "2026-11-17", StartPeriod: 1, EndPeriod: 2,
		LeadTeachers: []string{"t08"}, LinkType: protocol.EventLinkTeacherTrip,
	})
	_, _ = calSvc.CommitEvent(evTrip.ID, "정행사", true)

	// 다학년 동시 1교시 배정 시 동일 교사 중복 배정 0건 & 부재 교사 3인(t07, t08, t11) 배제 검증
	slots, err := examSvc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-multi-grade", Title: "동시 1교시 시험", ExamDate: "2026-11-17", IsSealed: true, UnsealTime: "08:20", DeputyMode: false,
	}, []internal_exam.ExamPeriodSpec{
		{Period: 1, Grade: 1, Subject: "국어", ClassRooms: []string{"1-1", "1-2"}},
		{Period: 1, Grade: 2, Subject: "수학", ClassRooms: []string{"2-1", "2-2"}},
	})
	if err != nil || len(slots) < 4 {
		t.Fatalf("동시 교시 감독 배정 실패: len=%d, err=%v", len(slots), err)
	}
	seenP1 := map[string]bool{}
	for _, sl := range slots {
		if sl.TeacherID == "t07" || sl.TeacherID == "t08" || sl.TeacherID == "t11" {
			t.Fatalf("시험 당일 부재 교사(%s)가 감독에 배정되었습니다: %+v", sl.TeacherName, sl)
		}
		if seenP1[sl.TeacherID] {
			t.Fatalf("동일 날짜 1교시 다학년 시험에 교사(%s)가 중복 배정되었습니다", sl.TeacherName)
		}
		seenP1[sl.TeacherID] = true
	}

	// 봉인 중 긴급 대타 시 이벤트의 RoomName 마스킹 확인
	var capturedSub *protocol.DomainEvent
	bus.Subscribe(protocol.EventExamSubstituted, func(evt protocol.DomainEvent) {
		c := evt
		capturedSub = &c
	})
	recs, err := examSvc.RecommendSubstitutes(slots[0].ID)
	if err != nil || len(recs) == 0 {
		t.Fatalf("대타 추천 실패: err=%v, recs=%d", err, len(recs))
	}
	if _, err := examSvc.ApplySubstitute(slots[0].ID, recs[0].ID, "박평가"); err != nil {
		t.Fatalf("긴급 대타 교체 실패: %v", err)
	}
	if capturedSub == nil || !strings.Contains(capturedSub.Title, "공개") {
		t.Fatalf("봉인 중 긴급대타 알림에서 고사장 마스킹이 누락되었습니다: %+v", capturedSub)
	}

	// 연간 누적 반영 멱등성: 2회 연속 반영 차단
	if _, err := examSvc.UpdateTeacherExamPolicy(internal_exam.TeacherExamPolicyUpdate{CommitCurrentToBase: true}); err != nil {
		t.Fatalf("1차 연간 누적 반영 실패: %v", err)
	}
	if _, err := examSvc.UpdateTeacherExamPolicy(internal_exam.TeacherExamPolicyUpdate{CommitCurrentToBase: true}); err == nil {
		t.Fatalf("연간 누적 반영 이중 실행이 차단되지 않았습니다")
	}

	// 학기 초기화 직후 GetConfig/ListSlotsForViewer 정상 응답(500 미발생) 확인
	if _, err := store.ResetSystem(database.ResetModeAcademicTerm); err != nil {
		t.Fatalf("학기 초기화 실패: %v", err)
	}
	afterResetSlots, afterResetCfg, err := examSvc.ListSlotsForViewer(false)
	if err != nil || len(afterResetSlots) != 0 || afterResetCfg.ID == "" {
		t.Fatalf("학기 초기화 직후 교내 시험 조회 오류: len=%d, cfg=%+v, err=%v", len(afterResetSlots), afterResetCfg, err)
	}

	// 외부시험: 주말 시험 LinkedToTimetable=false, 재확정 Diff, 확정 후 지원 변경 차단 검증
	extSat, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-sat", Title: "토요일 검정고시", ExamCategory: "검정고시",
		ExamDate: "2026-11-21", RequiredCount: 1,
	})
	confirmedSat, err := extSvc.ConfirmDispatch(extSat.ID, []string{"t05"}, "최외부")
	if err != nil || confirmedSat.LinkedToTimetable {
		t.Fatalf("주말 외부시험이 평일 시간표 이관(true)으로 표시되었습니다: %+v, err=%v", confirmedSat, err)
	}
	if _, err := extSvc.ToggleVolunteer(extSat.ID, "t06"); err == nil {
		t.Fatalf("확정 완료된 외부시험의 자발적 지원 변경이 차단되지 않았습니다")
	}
	// 평일 시험 재확정 Diff(t02 -> t06 교체) 시 누적 횟수 + STAGING 대기열 정리 확인
	extMon, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-mon", Title: "월요일 외부시험", ExamCategory: "모의평가",
		ExamDate: "2026-11-16", RequiredCount: 1,
	})
	_, _ = extSvc.ConfirmDispatch(extMon.ID, []string{"t02"}, "최외부")
	if _, err := extSvc.ConfirmDispatch(extMon.ID, []string{"t06"}, "최외부"); err != nil {
		t.Fatalf("외부시험 재확정 실패: %v", err)
	}
	t02After, _ := database.LoadRecord[protocol.Teacher](store, "teachers", "t02")
	if t02After.ExternalExamCount != 0 {
		t.Fatalf("재확정 시 제외된 교사(t02)의 누적 파견 횟수가 차감되지 않았습니다: %d", t02After.ExternalExamCount)
	}
	allReqs, _ := ttSvc.ListRequests()
	for _, rq := range allReqs {
		if strings.HasPrefix(rq.ID, "ext-trip-ext-mon-t02-") {
			t.Fatalf("재확정에서 제외된 교사의 미승인 외부출장 대기가 정리되지 않았습니다: %+v", rq)
		}
	}
}

func TestPhase2RealSchoolMessyExcelRoundTrip(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	// 1. 실학교 교사 명렬표 (상단 2행 배너 + 순서 변경 + 한글 요일/날짜/학급)
	fT := excelize.NewFile()
	sT := fT.GetSheetName(0)
	_ = fT.SetCellValue(sT, "A1", "2026학년도 교직원 명렬표")
	_ = fT.SetCellValue(sT, "A2", "담당: 교무기획부")
	for c, h := range []string{"성명", "담당과목", "아이디", "담임학급", "전입일", "출강요일"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 3)
		_ = fT.SetCellValue(sT, cell, h)
	}
	for c, v := range []interface{}{"서정보", "정보", "t11", "1학년 2반", "2025.03.01", "월,수,금"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 4)
		_ = fT.SetCellValue(sT, cell, v)
	}
	var bufT bytes.Buffer
	_ = fT.Write(&bufT)
	_ = fT.Close()

	cntT, err := ttSvc.ImportTeachersXLSX(bytes.NewReader(bufT.Bytes()))
	if err != nil || cntT != 1 {
		t.Fatalf("실학교 명렬표 업로드 실패: cnt=%d, err=%v", cntT, err)
	}
	t11, _ := database.LoadRecord[protocol.Teacher](store, "teachers", "t11")
	if t11.AnnualMainCount != 1 || t11.HomeroomClass != "1-2" || t11.TransferDate != "2025-03-01" || len(t11.ItinerantDays) != 3 {
		t.Fatalf("명렬표 정규화 또는 기존 시수 보존 실패: %+v", t11)
	}

	// 2. 컴시간 2단 병합 헤더 시수표 업로드 + =SUM() 수식 내보내기 검증
	fL := excelize.NewFile()
	sL := fL.GetSheetName(0)
	_ = fL.SetCellValue(sL, "A1", "꼬미고 시수 배당표")
	_ = fL.SetCellValue(sL, "E2", "1학년")
	_ = fL.MergeCell(sL, "E2", "F2")
	for c, h := range []string{"학년", "과목", "교사명", "동시그룹", "1반", "2반"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 3)
		_ = fL.SetCellValue(sL, cell, h)
	}
	for c, v := range []interface{}{"1학년", "국어", "강국어", "", "4.0", "3시간"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 4)
		_ = fL.SetCellValue(sL, cell, v)
	}
	var bufL bytes.Buffer
	_ = fL.Write(&bufL)
	_ = fL.Close()

	cntL, err := ttSvc.ImportTeachingLoadsXLSX(bytes.NewReader(bufL.Bytes()))
	if err != nil || cntL != 1 {
		t.Fatalf("병합 헤더 시수표 업로드 실패: cnt=%d, err=%v", cntL, err)
	}
	expL, err := ttSvc.ExportTeachingLoadsXLSX()
	if err != nil {
		t.Fatalf("시수표 엑셀 내보내기 실패: %v", err)
	}
	fxL, _ := excelize.OpenReader(bytes.NewReader(expL))
	formula, _ := fxL.GetCellFormula("교사별시수표", "O2")
	_ = fxL.Close()
	if !strings.HasPrefix(formula, "SUM(") {
		t.Fatalf("시수표 계 열에 =SUM() 수식이 적용되지 않았습니다: %q", formula)
	}

	// 3. 동명이인 교사 시수표 충돌 시 조용한 덮어쓰기 대신 명시적 에러 차단 확인
	_ = store.SaveRecord("teachers", "t-dup-math", protocol.Teacher{
		ID: "t-dup-math", Username: "dup_k", Name: "강국어", Subject: "수학",
	})
	fDup := excelize.NewFile()
	sDup := fDup.GetSheetName(0)
	for c, h := range []string{"학년", "과목", "교사명", "1-1"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = fDup.SetCellValue(sDup, cell, h)
	}
	for c, v := range []interface{}{1, "미술", "강국어", 2} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 2)
		_ = fDup.SetCellValue(sDup, cell, v)
	}
	var bufDup bytes.Buffer
	_ = fDup.Write(&bufDup)
	_ = fDup.Close()
	if _, err := ttSvc.ImportTeachingLoadsXLSX(bytes.NewReader(bufDup.Bytes())); err == nil || !strings.Contains(err.Error(), "동명이인") {
		t.Fatalf("동명이인 모호성 에러가 차단되지 않았습니다: err=%v", err)
	}
}
