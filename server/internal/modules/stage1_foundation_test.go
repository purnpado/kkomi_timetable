package modules_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

// TestStage1NonSubjectTeachersAndSubstituteExclusion은 비교과 교원 유형 분리, 수업 유무·대타 제외 설정,
// admin/비교과 교사의 대타 추천 및 기초 시간표 자동 편성 배제, 교사 명렬표 엑셀 왕복을 검증합니다.
func TestStage1NonSubjectTeachersAndSubstituteExclusion(t *testing.T) {
	store, bus := setupTestEnv(t)
	authSvc := auth.NewService(store, bus)
	ttSvc := timetable.NewService(store, bus)

	// 1. 다양한 비교과 교원 및 명시적 예외 설정 교원 등록
	extraTeachers := []protocol.Teacher{
		{ID: "t-coun", Username: "coun1", Name: "최상담", Subject: "상담", Position: "전문상담교사", TeacherType: protocol.TeacherTypeCounselor, Roles: []protocol.Role{protocol.RoleTeacher}},
		{ID: "t-lib", Username: "lib1", Name: "장사서", Subject: "문헌정보", Position: "사서교사", Roles: []protocol.Role{protocol.RoleTeacher}}, // 직위 기반 자동 추론
		{ID: "t-nut", Username: "nut1", Name: "한영양", Subject: "영양", Position: "영양교사", Roles: []protocol.Role{protocol.RoleTeacher}},
		{ID: "t-hlth-sub", Username: "hlth2", Name: "이보건", Subject: "보건", Position: "보건교사", TeacherType: protocol.TeacherTypeHealth, HasClasses: protocol.BoolPtr(true), SubstituteExempt: protocol.BoolPtr(false), Roles: []protocol.Role{protocol.RoleTeacher}},
		{ID: "t-subj-ex", Username: "subjex", Name: "윤제외", Subject: "국어", Position: "평교사", TeacherType: protocol.TeacherTypeSubject, HasClasses: protocol.BoolPtr(true), SubstituteExempt: protocol.BoolPtr(true), Roles: []protocol.Role{protocol.RoleTeacher}},
	}
	for _, tc := range extraTeachers {
		if _, err := authSvc.SaveTeacher(tc, "1234"); err != nil {
			t.Fatalf("비교과 테스트 교원(%s) 저장 실패: %v", tc.Name, err)
		}
	}

	loadedLib, err := authSvc.GetTeacher("t-lib")
	if err != nil || loadedLib.TeacherType != protocol.TeacherTypeLibrarian || protocol.HasRegularClasses(*loadedLib) || !protocol.IsSubstituteExcluded(*loadedLib) {
		t.Fatalf("사서교사 자동 추론 기본값 오류: err=%v, teacher=%+v", err, loadedLib)
	}

	// 2. 주중 미출근 대책(2026-11-18 수요일 강국어 t06): 대타 후보에서 admin·t15·t16·t-coun·t-lib·t-nut·t-subj-ex 모두 제외, t-hlth-sub는 허용 확인
	plans, err := ttSvc.PlanTeacherAbsence(protocol.AbsencePlanRequest{
		TeacherID: "t06", StartDate: "2026-11-18", EndDate: "2026-11-18", StartPeriod: 1, EndPeriod: 7, Reason: "병가",
	})
	if err != nil || len(plans) == 0 {
		t.Fatalf("결근 대책 수립 실패: len=%d, err=%v", len(plans), err)
	}
	excludedIDs := map[string]bool{
		"t01":       true, // admin
		"t15":       true, // 박특수
		"t16":       true, // 김보건
		"t-coun":    true, // 최상담
		"t-lib":     true, // 장사서
		"t-nut":     true, // 한영양
		"t-subj-ex": true, // 대타 제외 지정 교과교사
	}
	foundHlthSub := false
	for _, pl := range plans {
		for _, sub := range pl.SubstituteOptions {
			if excludedIDs[sub.TeacherID] {
				t.Fatalf("대타 제외 대상(%s/%s)이 결보강 대타 후보에 포함되었습니다", sub.TeacherID, sub.TeacherName)
			}
			if sub.TeacherID == "t-hlth-sub" {
				foundHlthSub = true
			}
		}
	}
	if !foundHlthSub {
		t.Fatalf("대타 허용으로 설정한 교원(t-hlth-sub)이 대타 후보에 포함되지 않았습니다")
	}

	// 3. 기초 시간표 직접 배정(SaveCell)·시수표(SaveTeachingLoad) admin 차단 및 자동편성 비교과(수업없음) 배제 검증
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 1, Period: 7, ClassName: "1-1", Grade: 1, Subject: "국어", TeacherID: "t01",
	}); err == nil {
		t.Fatalf("admin 계정의 기초 시간표 셀 저장이 차단되지 않았습니다")
	}
	if _, err := ttSvc.SaveTeachingLoad(protocol.TeachingLoad{
		TeacherID: "t01", Subject: "국어", Grade: 1, ClassHours: map[string]int{"1-1": 2},
	}); err == nil {
		t.Fatalf("admin 계정의 시수표 저장이 차단되지 않았습니다")
	}
	created, err := ttSvc.AutoGenerateBaseTimetable([]int{1, 2, 3}, 2, true)
	if err != nil || len(created) == 0 {
		t.Fatalf("기초 시간표 자동 편성 실패: %v", err)
	}
	noClassIDs := map[string]bool{"t01": true, "t15": true, "t16": true, "t-coun": true, "t-lib": true, "t-nut": true}
	for _, c := range created {
		if noClassIDs[c.TeacherID] || noClassIDs[c.CoTeacherID] {
			t.Fatalf("수업 없는 계정/비교과 교원(%s)이 정규 시간표 자동 편성에 배정되었습니다: %+v", c.TeacherID, c)
		}
	}

	// 4. 교사 명렬표 엑셀 내보내기(admin 제외 + 교원유형/수업유무/결보강대타제외 열 포함) 및 재업로드 검증
	xBytes, err := ttSvc.ExportTeachersXLSX()
	if err != nil || len(xBytes) == 0 {
		t.Fatalf("교사 명렬표 엑셀 생성 실패: %v", err)
	}
	fx, err := excelize.OpenReader(bytes.NewReader(xBytes))
	if err != nil {
		t.Fatalf("엑셀 열기 실패: %v", err)
	}
	rows, _ := fx.GetRows("교사명렬표")
	_ = fx.Close()
	if len(rows) < 2 || !strings.Contains(strings.Join(rows[0], ","), "교원유형") || !strings.Contains(strings.Join(rows[0], ","), "결보강대타제외") {
		t.Fatalf("교사 명렬표 엑셀 헤더에 교원유형·결보강대타제외 열이 없습니다: %+v", rows[0])
	}
	for _, r := range rows[1:] {
		if len(r) > 1 && strings.EqualFold(strings.TrimSpace(r[1]), "admin") {
			t.Fatalf("교사 명렬표 엑셀에 시스템 admin 계정이 노출되었습니다: %+v", r)
		}
	}

	imported, err := ttSvc.ImportTeachersXLSX(bytes.NewReader(xBytes))
	if err != nil || imported == 0 {
		t.Fatalf("교사 명렬표 엑셀 재업로드 실패: imported=%d, err=%v", imported, err)
	}

	// 5. 교원유형="상담", 수업유무="N", 결보강대타제외="Y" 엑셀 신규 임포트 검증
	fCustom := excelize.NewFile()
	sCustom := fCustom.GetSheetName(0)
	for c, h := range []string{"아이디", "성명", "담당과목", "직위", "교원유형", "수업유무(Y/N)", "결보강대타제외(Y/N)"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = fCustom.SetCellValue(sCustom, cell, h)
	}
	for c, v := range []interface{}{"coun_xls", "박상담", "상담", "교사", "상담", "N", "Y"} {
		cell, _ := excelize.CoordinatesToCellName(c+1, 2)
		_ = fCustom.SetCellValue(sCustom, cell, v)
	}
	var bufCustom bytes.Buffer
	_ = fCustom.Write(&bufCustom)
	_ = fCustom.Close()
	if cntC, err := ttSvc.ImportTeachersXLSX(bytes.NewReader(bufCustom.Bytes())); err != nil || cntC != 1 {
		t.Fatalf("비교과 엑셀 임포트 실패: cnt=%d, err=%v", cntC, err)
	}
	importedCoun, err := authSvc.GetTeacher("t-coun_xls")
	if err != nil || importedCoun.TeacherType != protocol.TeacherTypeCounselor || protocol.HasRegularClasses(*importedCoun) || !protocol.IsSubstituteExcluded(*importedCoun) {
		t.Fatalf("비교과 엑셀 속성 파싱 오류: err=%v, teacher=%+v", err, importedCoun)
	}
}

// TestStage1SpecialClassExamRoomPriorityAssignment은 특수학급고사실 개설 시 특수교사 최우선 전담 배정,
// 복수 특수교사 균등 배정, 특수교사 부재 시 일반교사 폴백 및 긴급 대타 특수교사 1순위 추천을 검증합니다.
func TestStage1SpecialClassExamRoomPriorityAssignment(t *testing.T) {
	store, bus := setupTestEnv(t)
	authSvc := auth.NewService(store, bus)
	examSvc := internal_exam.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)

	if _, err := examSvc.UpdateTeacherExamPolicy(internal_exam.TeacherExamPolicyUpdate{ResetAllBaseCounts: true}); err != nil {
		t.Fatalf("기실시 0회 초기화 실패: %v", err)
	}

	// 1. 1인 특수교사(t15 박특수) 환경: 3개 교시에 특수학급고사실 + 별도고사실 + 일반 학급 동시 편성
	cfg := protocol.ExamConfig{
		ID:                 "exam-special-1",
		Title:              "특수학급고사실 전담 배정 검증",
		ExamDate:           "2026-11-23",
		DeputyMode:         true,
		AvoidConsecutive:   true,
		MaxDailyPerTeacher: 2,
	}
	specs := []internal_exam.ExamPeriodSpec{
		{ExamDate: "2026-11-23", Period: 1, Grade: 1, Subject: "국어", ClassRooms: []string{"1-1", "1-2", "특수학급고사실", "별도고사실(독감·감염병)"}},
		{ExamDate: "2026-11-23", Period: 2, Grade: 2, Subject: "수학", ClassRooms: []string{"2-1", "2-2", "특수학급고사실"}},
		{ExamDate: "2026-11-23", Period: 3, Grade: 3, Subject: "영어", ClassRooms: []string{"3-1", "3-2", "특수학급고사실"}},
	}
	slots, err := examSvc.GenerateAndSave(cfg, specs)
	if err != nil {
		t.Fatalf("시험 배정 실패: %v", err)
	}

	specialCount := 0
	for _, sl := range slots {
		if strings.Contains(sl.RoomName, "특수학급고사실") {
			specialCount++
			if sl.SlotRole != "MAIN" {
				t.Fatalf("특수학급고사실에 정감독 이외 역할이 배정되었습니다: %+v", sl)
			}
			if sl.TeacherID != "t15" {
				t.Fatalf("특수학급고사실에 특수교사(t15) 대신 다른 교사(%s)가 배정되었습니다: %+v", sl.TeacherName, sl)
			}
		} else if sl.TeacherID == "t15" {
			t.Fatalf("특수교사(t15)가 일반/복도/별도고사실에 배정되었습니다: %+v", sl)
		}
		if strings.Contains(sl.RoomName, "별도고사실") && sl.TeacherID == "t15" {
			t.Fatalf("별도고사실(독감·감염병)은 일반 교사가 순환 배정되어야 합니다: %+v", sl)
		}
	}
	if specialCount != 3 {
		t.Fatalf("특수학급고사실 배정 건수 오류: got=%d, want=3", specialCount)
	}

	fairness, err := examSvc.CalculateFairnessSummary()
	if err != nil {
		t.Fatalf("균등 통계 산출 실패: %v", err)
	}
	if fairness.MaxGap > 1 || fairness.ConsecutivePairCount != 0 {
		t.Fatalf("특수학급고사실 포함 시 일반 교원 균형/연강방지가 깨졌습니다: maxGap=%d, consec=%d", fairness.MaxGap, fairness.ConsecutivePairCount)
	}

	// 2. 제2 특수교사(t-spec2) 추가 후 4교시 편성 -> 특수학급고사실 균등 분산 및 대타 추천 1순위 확인
	if _, err := authSvc.SaveTeacher(protocol.Teacher{
		ID: "t-spec2", Username: "spec2", Name: "김특수", Subject: "특수학급", Position: "특수교사",
		TeacherType: protocol.TeacherTypeSpecial, Roles: []protocol.Role{protocol.RoleTeacher},
	}, "1234"); err != nil {
		t.Fatalf("추가 특수교사 등록 실패: %v", err)
	}
	specs4 := append(specs, internal_exam.ExamPeriodSpec{
		ExamDate: "2026-11-23", Period: 4, Grade: 1, Subject: "사회", ClassRooms: []string{"1-1", "1-2", "특수학급고사실"},
	})
	slots4, err := examSvc.GenerateAndSave(cfg, specs4)
	if err != nil {
		t.Fatalf("복수 특수교사 시험 배정 실패: %v", err)
	}
	specCounts := map[string]int{}
	var firstSpecialSlotID string
	for _, sl := range slots4 {
		if strings.Contains(sl.RoomName, "특수학급고사실") {
			specCounts[sl.TeacherID]++
			if firstSpecialSlotID == "" {
				firstSpecialSlotID = sl.ID
			}
		}
	}
	if specCounts["t15"] != 2 || specCounts["t-spec2"] != 2 {
		t.Fatalf("복수 특수교사 균등 분배 실패: %+v", specCounts)
	}

	recs, err := examSvc.RecommendSubstitutes(firstSpecialSlotID)
	if err != nil || len(recs) == 0 || (recs[0].ID != "t15" && recs[0].ID != "t-spec2") {
		t.Fatalf("특수학급고사실 긴급 대타 추천 1순위가 특수교사가 아닙니다: err=%v, recs=%+v", err, recs)
	}
	if _, err := examSvc.ApplySubstitute(firstSpecialSlotID, recs[0].ID, "박평가"); err != nil {
		t.Fatalf("특수학급고사실 특수교사 대타 교체 실패: %v", err)
	}

	// 3. 특수교사 전원 시험일 부재 시 일반교사 폴백 배정 검증
	evTrip, _ := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID: "evt-spec-trip", Title: "특수교육 연수 출장", Department: "특수교육부",
		EventDate: "2026-11-24", StartPeriod: 1, EndPeriod: 4,
		LeadTeachers: []string{"t15", "t-spec2"}, LinkType: protocol.EventLinkTeacherTrip,
	})
	if _, err := calSvc.CommitEvent(evTrip.ID, "정행사", true); err != nil {
		t.Fatalf("특수교사 출장 확정 실패: %v", err)
	}
	fbSlots, err := examSvc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-special-fallback", Title: "특수교사 부재 폴백 검증", ExamDate: "2026-11-24",
	}, []internal_exam.ExamPeriodSpec{
		{ExamDate: "2026-11-24", Period: 1, Grade: 1, Subject: "국어", ClassRooms: []string{"1-1", "특수학급고사실"}},
	})
	if err != nil {
		t.Fatalf("폴백 배정 실패: %v", err)
	}
	foundFallback := false
	for _, sl := range fbSlots {
		if strings.Contains(sl.RoomName, "특수학급고사실") {
			foundFallback = true
			if sl.TeacherID == "t15" || sl.TeacherID == "t-spec2" || sl.TeacherID == "" {
				t.Fatalf("부재 중인 특수교사가 배정되거나 미배정되었습니다: %+v", sl)
			}
			if !strings.Contains(sl.Note, "일반교사 대체") {
				t.Fatalf("폴백 비고 안내가 누락되었습니다: %+v", sl)
			}
		}
	}
	if !foundFallback {
		t.Fatalf("특수학급고사실 폴백 슬롯이 생성되지 않았습니다")
	}
	_ = database.ResetModeAcademicTerm
}
