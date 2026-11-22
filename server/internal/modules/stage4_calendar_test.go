package modules_test

import (
	"slices"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/timetable"
)

func TestStage4MultiDayBellPresetsHolidayLedgerConflictsAndRecurrence(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)

	// 1. 단축수업 프리셋 3종(40분·45분·오전행사) 및 다일(EndDate) 시종시간 변경 검증
	presets := event_calendar.DefaultBellPresets()
	if len(presets) < 3 {
		t.Fatalf("단축수업 프리셋은 최소 3종이어야 합니다: %d", len(presets))
	}
	bellEv, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		Title:       "기말 단축운영 주간",
		Department:  "교무기획부",
		EventDate:   "2026-12-08",
		EndDate:     "2026-12-10",
		StartPeriod: 1,
		EndPeriod:   7,
		LinkType:    protocol.EventLinkBellChange,
		BellPreset:  "SHORT_40",
		ProposedBy:  "정행사",
	})
	if err != nil {
		t.Fatalf("다일 시종변경 행사 등록 실패: %v", err)
	}
	if len(bellEv.BellOverride) != 7 || bellEv.BellOverride[0].EndTime != "09:20" {
		t.Fatalf("SHORT_40 프리셋 자동 채움이 올바르지 않습니다: %+v", bellEv.BellOverride)
	}
	if _, err := calSvc.CommitEvent(bellEv.ID, "정행사", true); err != nil {
		t.Fatalf("다일 시종변경 확정 실패: %v", err)
	}
	for _, d := range []string{"2026-12-08", "2026-12-09", "2026-12-10"} {
		eff, err := calSvc.EffectiveBellSchedule(d)
		if err != nil || len(eff) == 0 || eff[0].EndTime != "09:20" {
			t.Fatalf("다일 시종변경 미적용(%s): %+v (err=%v)", d, eff, err)
		}
	}
	outsideBell, _ := calSvc.EffectiveBellSchedule("2026-12-11")
	if len(outsideBell) > 0 && outsideBell[0].EndTime == "09:20" {
		t.Fatalf("종료일 이후 일자(2026-12-11)에는 단축시종이 적용되면 안 됩니다: %+v", outsideBell[0])
	}

	// 2. 공휴일·재량휴업일(HOLIDAY) 확정 시 주간 블록 반영 + 학기 시수 원장 차감(교사 결강 부채 미생성) 검증
	baseLedger, err := ttSvc.CalculateSemesterLedger()
	if err != nil {
		t.Fatalf("기본 시수 원장 계산 실패: %v", err)
	}
	baseHolidayDeducted := baseLedger.Summary["holidayDeductedHours"]
	baseDebtCount := len(baseLedger.Debts)

	holidayEv, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		Title:       "12월 재량휴업일",
		Department:  "교무기획부",
		EventDate:   "2026-12-07", // 월요일
		LinkType:    protocol.EventLinkHoliday,
		HolidayKind: "DISCRETIONARY",
		Location:    "전교",
		ProposedBy:  "정행사",
	})
	if err != nil {
		t.Fatalf("재량휴업일 등록 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(holidayEv.ID, "정행사", true); err != nil {
		t.Fatalf("재량휴업일 확정 실패: %v", err)
	}

	afterLedger, err := ttSvc.CalculateSemesterLedger()
	if err != nil {
		t.Fatalf("휴업일 반영 후 원장 계산 실패: %v", err)
	}
	if afterLedger.Summary["holidayDeductedHours"] <= baseHolidayDeducted {
		t.Fatalf("휴업일 시수 자동 차감이 반영되지 않았습니다: before=%d, after=%d", baseHolidayDeducted, afterLedger.Summary["holidayDeductedHours"])
	}
	if len(afterLedger.Debts) != baseDebtCount {
		t.Fatalf("공휴일·휴업일에는 교사 미보충 부채가 생성되면 안 됩니다: before=%d, after=%d", baseDebtCount, len(afterLedger.Debts))
	}
	weekCells, err := ttSvc.ListWeekCells("2026-12-07")
	if err != nil {
		t.Fatalf("휴업일 주차 조회 실패: %v", err)
	}
	foundHolidayCell := false
	for _, c := range weekCells {
		if c.DayOfWeek == 1 && c.BlockedBy == holidayEv.Title && strings.HasPrefix(c.ChangeNote, "[휴업일]") {
			foundHolidayCell = true
			break
		}
	}
	if !foundHolidayCell {
		t.Fatalf("주간 시간표에 휴업일 블록([휴업일])이 반영되지 않았습니다")
	}

	// 3. 장소 중복(LOCATION) 및 동일 학년·교시 충돌(GRADE_PERIOD) 감지 검증
	baseEv, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		Title:        "1학년 진로특강",
		Department:   "진로상담부",
		EventDate:    "2026-12-15",
		StartPeriod:  3,
		EndPeriod:    4,
		TargetGrades: []int{1},
		Location:     "시청각실",
		LinkType:     protocol.EventLinkClassBlock,
		ProposedBy:   "김진로",
	})
	if err != nil {
		t.Fatalf("기준 행사 등록 실패: %v", err)
	}
	conflicts, err := calSvc.CheckConflicts(protocol.SchoolEvent{
		Title:        "1·2학년 안전교육",
		EventDate:    "2026-12-14",
		EndDate:      "2026-12-16",
		StartPeriod:  4,
		EndPeriod:    5,
		TargetGrades: []int{1, 2},
		Location:     "시청각실",
		LinkType:     protocol.EventLinkClassBlock,
	})
	if err != nil {
		t.Fatalf("충돌 검사 실패: %v", err)
	}
	var cTypes []string
	for _, c := range conflicts {
		cTypes = append(cTypes, c.Type)
	}
	if !slices.Contains(cTypes, "LOCATION") || !slices.Contains(cTypes, "GRADE_PERIOD") {
		t.Fatalf("장소(LOCATION) 및 학년/교시(GRADE_PERIOD) 충돌이 모두 감지되어야 합니다: %+v", conflicts)
	}
	selfConflicts, _ := calSvc.CheckConflicts(baseEv)
	for _, c := range selfConflicts {
		if c.EventID == baseEv.ID {
			t.Fatalf("자기 자신 수정 시 자기 자신과 충돌로 판정되면 안 됩니다: %+v", c)
		}
	}

	// 4. 매주 반복 일정 생성, 다일 인솔출장 결보강 이관 및 수정 권한 검증
	recEv, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		Title:         "동아리 정기활동",
		Department:    "창의체험부",
		EventDate:     "2026-12-04",
		StartPeriod:   6,
		EndPeriod:     7,
		TargetGrades:  []int{1},
		LinkType:      protocol.EventLinkSimpleNotice,
		Recurrence:    "WEEKLY",
		RecurrenceEnd: "2026-12-18",
		ProposedBy:    "김교사",
	})
	if err != nil {
		t.Fatalf("반복 일정 등록 실패: %v", err)
	}
	allEvents, _ := calSvc.ListEvents()
	recCount := 0
	for _, ev := range allEvents {
		if ev.RecurrenceGroup != "" && ev.RecurrenceGroup == recEv.RecurrenceGroup {
			recCount++
		}
	}
	if recCount != 3 {
		t.Fatalf("매주 반복 일정(12/04~12/18)은 3건이 생성되어야 합니다: got=%d", recCount)
	}

	// 일반 교사 본인 대기 행사 수정 허용 / 타인 행사 수정 거부
	recEv.Location = "다목적실"
	if _, err := calSvc.UpdateEvent(recEv, "강국어", false); err == nil {
		t.Fatalf("타인의 대기 행사 수정은 거부되어야 합니다")
	}
	updatedRec, err := calSvc.UpdateEvent(recEv, "김교사", false)
	if err != nil || updatedRec.Location != "다목적실" {
		t.Fatalf("본인의 대기 행사 수정이 실패했습니다: %v", err)
	}

	// 다일 인솔출장 확정 시 해당 기간 평일 수업에 대해 날짜별 EVENT_TRIP 대기열 생성 확인
	tripEv, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		Title:        "전국대회 인솔",
		Department:   "학생부",
		EventDate:    "2026-12-08",
		EndDate:      "2026-12-09",
		StartPeriod:  0,
		EndPeriod:    0,
		LeadTeachers: []string{"t06"},
		LinkType:     protocol.EventLinkTeacherTrip,
		ProposedBy:   "강국어",
	})
	if err != nil {
		t.Fatalf("다일 출장 등록 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(tripEv.ID, "정행사", true); err != nil {
		t.Fatalf("다일 출장 확정 실패: %v", err)
	}
	reqs, _ := ttSvc.ListRequests()
	tripDates := make(map[string]bool)
	for _, rq := range reqs {
		if strings.HasPrefix(rq.ID, "evt-trip-"+tripEv.ID+"-") {
			tripDates[rq.TargetDate] = true
		}
	}
	if !tripDates["2026-12-08"] && !tripDates["2026-12-09"] {
		t.Fatalf("다일 인솔출장 확정 시 결보강 대기열이 날짜별로 생성되어야 합니다: %+v", tripDates)
	}
}
