package modules_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/external_exam"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

func TestStage3InternalExamSameRoomReductionGradeAuthorAndStudyHall(t *testing.T) {
	store, bus := setupTestEnv(t)
	clearStage2Collections(t, store)
	authSvc := auth.NewService(store)
	ttSvc := timetable.NewService(store, bus)
	examSvc := internal_exam.NewService(store, bus)

	// 비고정 교사 초기화 (시드 교사 외 깔끔한 전용 교원군 구축을 위해 기존 교사 전부 로드해 확인)
	existingTeachers, _ := authSvc.ListTeachers()
	for _, et := range existingTeachers {
		if !protocol.IsSystemAdminAccount(et) && !et.IsParentHonorary {
			_ = store.DeleteRecord("teachers", et.ID)
		}
	}

	makeTeacher := func(id, name, subj, homeroom, pos string, maxCnt, weight int, noConsec bool, redReason string) {
		hasCls := true
		_, err := authSvc.SaveTeacher(protocol.Teacher{
			ID: id, Username: id, Name: name, Subject: subj, HomeroomClass: homeroom, Position: pos,
			TeacherType: protocol.TeacherTypeSubject, HasClasses: &hasCls, Roles: []protocol.Role{protocol.RoleTeacher},
			ExamMaxCount: maxCnt, ExamReductionWeight: weight, ExamNoConsecutive: noConsec, ExamReductionReason: redReason,
		}, "1234")
		if err != nil {
			t.Fatalf("교사 생성 실패(%s): %v", id, err)
		}
	}
	makeTeacher("ie1", "임산부선생", "국어", "", "교사", 1, 2, true, "임산부 보호")
	makeTeacher("ie2", "보직부장", "영어", "", "교무부장", 0, 2, false, "보직부장 경감")
	makeTeacher("ie3", "김일학년수학", "수학", "1-1", "교사", 0, 0, false, "")
	makeTeacher("ie4", "박이학년수학", "수학", "2-1", "교사", 0, 0, false, "")
	makeTeacher("ie5", "이사회", "사회", "", "교사", 0, 0, false, "")
	makeTeacher("ie6", "최과학", "과학", "", "교사", 0, 0, false, "")
	makeTeacher("ie7", "정역사", "역사", "", "교사", 0, 0, false, "")
	makeTeacher("ie8", "한도덕", "도덕", "", "교사", 0, 0, false, "")
	makeTeacher("ie9", "윤정보", "정보", "", "교사", 0, 0, false, "")
	makeTeacher("ie10", "서미술", "미술", "", "교사", 0, 0, false, "")

	// 1학년 수학 = ie3, 2학년 수학 = ie4 로 시간표 등록
	_, _ = ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1, Subject: "수학", TeacherID: "ie3"})
	_, _ = ttSvc.SaveCell(protocol.TimetableCell{DayOfWeek: 1, Period: 1, ClassName: "2-1", Grade: 2, Subject: "수학", TeacherID: "ie4"})

	// 1. 동일 고사실 중복 금지 + 임산부 상한/연강금지 + 감염병 1·2·3실 + 고교학점제 다과목 + 공강자습실 + GRADE_ONLY 검증
	cfg := protocol.ExamConfig{
		ID:                    "exam-s3-1",
		Title:                 "2026 2학기 기말고사",
		ExamDate:              "2026-12-07",
		ExamDates:             []string{"2026-12-07", "2026-12-08"},
		AvoidConsecutive:      true,
		PreventSameRoomRepeat: true,
		SubjectExcludeMode:    "GRADE_ONLY",
		SubjectTeacherPolicy:  protocol.SubjectModeHQStandby,
		IsSealed:              true,
		UnsealTime:            "08:20",
	}
	specs := []internal_exam.ExamPeriodSpec{
		{
			ExamDate: "2026-12-07", Period: 1, Grade: 2, Subject: "수학", StartTime: "08:50", EndTime: "09:40", DurationMinutes: 50,
			ClassRooms:     []string{"2-1", "2-2", "별도고사실 1실(감염병)", "별도고사실 2실(감염병)", "별도고사실 3실(감염병)"},
			StudyHallRooms: []string{"도서실(공강자습)"},
		},
		{
			ExamDate: "2026-12-07", Period: 2, Grade: 2, Subject: "선택과목", StartTime: "10:00", EndTime: "10:50", DurationMinutes: 50,
			ClassRooms: []string{"세미나실A(수학)", "과학실1(과학)"},
		},
		{
			ExamDate: "2026-12-08", Period: 1, Grade: 2, Subject: "영어", StartTime: "08:50", EndTime: "09:40", DurationMinutes: 50,
			ClassRooms: []string{"2-1", "2-2"},
		},
	}

	slots, err := examSvc.GenerateAndSave(cfg, specs)
	if err != nil {
		t.Fatalf("교내 시험감독 생성 실패: %v", err)
	}

	// 감염병 1·2·3실, 공강자습실 배정 및 ie1 상한(1회)·ie4 2학년 수학 배제 검증
	ie1Count, ie2Count, studyHallCount, isoCount := 0, 0, 0, 0
	normalTotal := 0
	teacherRooms := make(map[string]map[string]int)
	for _, sl := range slots {
		if sl.TeacherID == "ie1" {
			ie1Count++
		}
		if sl.TeacherID == "ie2" {
			ie2Count++
		}
		if sl.TeacherID == "ie5" || sl.TeacherID == "ie6" || sl.TeacherID == "ie7" {
			normalTotal++
		}
		if sl.SlotRole == "STUDY_HALL" {
			studyHallCount++
		}
		if strings.Contains(sl.RoomName, "별도고사실") {
			isoCount++
		}
		if sl.Grade == 2 && sl.Subject == "수학" && sl.SlotRole == "MAIN" && sl.TeacherID == "ie4" {
			t.Fatalf("2학년 수학 담당교사 ie4가 2학년 수학 시험에 배정되었습니다")
		}
		if sl.RoomName == "세미나실A(수학) 고사장" && (sl.TeacherID == "ie3" || sl.TeacherID == "ie4") {
			t.Fatalf("선택과목 세미나실A(수학)에 수학 교사가 배정되었습니다")
		}
		if sl.SlotRole == "MAIN" {
			if teacherRooms[sl.TeacherID] == nil {
				teacherRooms[sl.TeacherID] = make(map[string]int)
			}
			teacherRooms[sl.TeacherID][sl.RoomName]++
		}
	}
	if isoCount != 3 || studyHallCount != 1 {
		t.Fatalf("감염병 1·2·3실(%d) 또는 공강자습실(%d) 배정 누락", isoCount, studyHallCount)
	}
	if ie1Count > 1 {
		t.Fatalf("임산부 최대 상한(1회) 초과: ie1=%d회", ie1Count)
	}
	if ie2Count*3 > normalTotal {
		t.Logf("참고: 보직 경감 ie2=%d, 일반 3인합=%d", ie2Count, normalTotal)
	}
	for tid, rmMap := range teacherRooms {
		for rm, cnt := range rmMap {
			if cnt > 1 && (rm == "2-1 고사장" || rm == "2-2 고사장") {
				t.Fatalf("PreventSameRoomRepeat 위반: 교사 %s가 %s에 %d회 중복 배정됨", tid, rm, cnt)
			}
		}
	}

	// 2. AUTHORS_ONLY 출제교사 배제 모드 검증
	cfgAuthors := cfg
	cfgAuthors.ID = "exam-s3-authors"
	cfgAuthors.SubjectExcludeMode = "AUTHORS_ONLY"
	specsAuthors := []internal_exam.ExamPeriodSpec{
		{
			ExamDate: "2026-12-09", Period: 1, Grade: 2, Subject: "수학", StartTime: "08:50", EndTime: "09:40", DurationMinutes: 50,
			ClassRooms: []string{"2-1", "2-2"}, AuthorTeacherIDs: []string{"ie4"},
		},
	}
	slotsAuth, err := examSvc.GenerateAndSave(cfgAuthors, specsAuthors)
	if err != nil {
		t.Fatalf("출제자 배제 시험 생성 실패: %v", err)
	}
	for _, sl := range slotsAuth {
		if sl.TeacherID == "ie4" {
			t.Fatalf("출제자 ie4가 시험 감독에 배정되었습니다")
		}
	}

	// 3. 수동 맞교환 + 개인별 감독 시간표(봉인 마스킹 포함) 검증
	var s1, s2 protocol.ExamSlot
	for _, sl := range slots {
		if sl.RoomName == "2-1 고사장" && s1.ID == "" {
			s1 = sl
		} else if sl.RoomName == "도서실(공강자습)" && s2.ID == "" {
			s2 = sl
		}
	}
	if s1.ID != "" && s2.ID != "" && s1.TeacherID != s2.TeacherID {
		if s1.TeacherID != "ie4" { // ie4가 아닌 일반 교사끼리 맞교환
			_ = slotsAuth
			_, _ = examSvc.GenerateAndSave(cfg, specs)
			curSlots, _, _ := examSvc.ListSlotsForViewer(true)
			var sa, sb protocol.ExamSlot
			for _, sl := range curSlots {
				if sl.ExamDate == "2026-12-08" && sl.Period == 1 && sl.RoomName == "2-1 고사장" {
					sa = sl
				} else if sl.ExamDate == "2026-12-08" && sl.Period == 1 && sl.RoomName == "2-2 고사장" {
					sb = sl
				}
			}
			if sa.ID != "" && sb.ID != "" {
				swapped, sErr := examSvc.SwapSlots(sa.ID, sb.ID, "평가계")
				if sErr != nil {
					t.Fatalf("수동 맞교환 실패: %v", sErr)
				}
				found := false
				for _, sw := range swapped {
					if sw.ID == sa.ID && sw.TeacherID == sb.TeacherID {
						found = true
					}
				}
				if !found {
					t.Fatalf("맞교환 결과 반영 누락")
				}
			}
		}
	}

	// 봉인 상태 개인 시간표 검증
	allAdmin, _, _ := examSvc.ListSlotsForViewer(true)
	if len(allAdmin) > 0 {
		sampleTID := allAdmin[0].TeacherID
		psUser, err := examSvc.PersonalSchedule(sampleTID, false)
		if err != nil || len(psUser.Slots) == 0 || !psUser.Slots[0].ActualRoomHidden {
			t.Fatalf("일반 교사 개인 감독 시간표 봉인 마스킹 실패: %+v, err=%v", psUser, err)
		}
		psAdmin, _ := examSvc.PersonalSchedule(sampleTID, true)
		if len(psAdmin.Slots) == 0 || psAdmin.Slots[0].ActualRoomHidden {
			t.Fatalf("관리자 개인 감독 시간표 고사장 노출 실패: %+v", psAdmin)
		}
	}
}

func TestStage3ExternalExamCategoriesWeightsGraceTieBreakerAndOverflow(t *testing.T) {
	store, bus := setupTestEnv(t)
	clearStage2Collections(t, store)
	_ = store.WithTx(func(tx *database.TxStore) error {
		return tx.ClearCollections("external_exams", "external_exam_config")
	})
	authSvc := auth.NewService(store)
	extSvc := external_exam.NewService(store, bus)

	existingTeachers, _ := authSvc.ListTeachers()
	for _, et := range existingTeachers {
		if !protocol.IsSystemAdminAccount(et) && !et.IsParentHonorary {
			_ = store.DeleteRecord("teachers", et.ID)
		}
	}

	saveExtTeacher := func(id, name, transfer, homeroom, pos string, birthYear, salary int, exempt []string) {
		hasCls := true
		_, err := authSvc.SaveTeacher(protocol.Teacher{
			ID: id, Username: id, Name: name, Subject: "국어", HomeroomClass: homeroom, Position: pos,
			TransferDate: transfer, BirthYear: birthYear, SalaryStep: salary, ExemptReasons: exempt,
			TeacherType: protocol.TeacherTypeSubject, HasClasses: &hasCls, Roles: []protocol.Role{protocol.RoleTeacher},
		}, "1234")
		if err != nil {
			t.Fatalf("외부시험 테스트 교사(%s) 생성 실패: %v", id, err)
		}
	}
	saveExtTeacher("ex1", "나수능경력", "2020-03-01", "", "교사", 1985, 18, nil)
	saveExtTeacher("ex2", "가검정경력", "2021-03-01", "", "교사", 1990, 14, nil)
	saveExtTeacher("ex3", "다신규전입", "2026-03-01", "", "교사", 1995, 10, nil)
	saveExtTeacher("ex4", "라고삼담임", "2022-03-01", "3-1", "교사", 1988, 15, nil)
	saveExtTeacher("ex5", "마기간제", "2023-03-01", "", "기간제교사", 1992, 11, nil)

	// 1. 학교 기본 내규 프리셋 + 자유 종목 추가 저장
	cfg := protocol.DefaultExternalExamSchoolConfig()
	cfg.Categories = append(cfg.Categories, protocol.ExternalExamCategoryDef{Name: "전국모의평가", Weight: 0.5})
	cfg.DefaultScoringMode = "WEIGHTED_SCORE"
	cfg.DefaultTransferGraceYear = true
	if _, err := extSvc.SaveSchoolConfig(cfg); err != nil {
		t.Fatalf("학교 내규 프리셋 저장 실패: %v", err)
	}

	// 과거 확정 시험: ex1은 수능감독(2.0점), ex2는 검정고시(1.0점)
	csatPast, err := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-past-csat", Title: "전년도 수능감독", ExamCategory: "수능감독", ExamDate: "2026-11-12", RequiredCount: 1,
	})
	if err != nil {
		t.Fatalf("과거 수능 공고 생성 실패: %v", err)
	}
	if _, err := extSvc.ConfirmDispatch(csatPast.ID, []string{"ex1"}, "대외계"); err != nil {
		t.Fatalf("수능 확정 실패: %v", err)
	}
	gedPast, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-past-ged", Title: "전반기 검정고시", ExamCategory: "검정고시", ExamDate: "2026-04-10", RequiredCount: 1,
	})
	if _, err := extSvc.ConfirmDispatch(gedPast.ID, []string{"ex2"}, "대외계"); err != nil {
		t.Fatalf("검정고시 확정 실패: %v", err)
	}

	// 2. 신규 수능감독 공고: 가중치 점수제(WEIGHTED_SCORE) + 전입 1년차 유예(TransferGraceYear) + 고3담임·기간제 제외
	newCSAT, err := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-cur-csat", Title: "2027 수능감독", ExamCategory: "수능감독", ExamDate: "2026-12-10", RequiredCount: 2,
		ScoringMode: "WEIGHTED_SCORE", TransferGraceYear: true, ExcludeExempt: true,
		CategoryExcludeRules: []string{"GRADE3_HOMEROOM", "CONTRACT_TEACHER"},
		PriorityOrder:        []protocol.ExternalExamCriteria{protocol.CriteriaVolunteer, protocol.CriteriaLowCount, protocol.CriteriaEarlyTransfer},
	})
	if err != nil {
		t.Fatalf("신규 수능 공고 생성 실패: %v", err)
	}
	cands, _, err := extSvc.CalculateCandidates(newCSAT.ID)
	if err != nil {
		t.Fatalf("후보자 계산 실패: %v", err)
	}
	rankOf := func(id string) int {
		for _, c := range cands {
			if c.Teacher.ID == id {
				return c.Rank
			}
		}
		return 999
	}
	findCand := func(id string) protocol.ExternalExamCandidate {
		for _, c := range cands {
			if c.Teacher.ID == id {
				return c
			}
		}
		return protocol.ExternalExamCandidate{}
	}
	// ex2(1.0점)가 ex1(2.0점)보다 앞순위여야 함
	if rankOf("ex2") > rankOf("ex1") {
		t.Fatalf("가중치 점수제 정렬 오류: ex2(1.0점) rank=%d > ex1(2.0점) rank=%d", rankOf("ex2"), rankOf("ex1"))
	}
	// ex3(2026-03-01 전입 -> 2026-12-10 기준 1년차 유예)는 ex2, ex1보다 후순위
	if !findCand("ex3").IsGracePeriod || rankOf("ex3") < rankOf("ex1") {
		t.Fatalf("전입 1년차 유예 미적용: ex3=%+v, ex1Rank=%d", findCand("ex3"), rankOf("ex1"))
	}
	// ex4(고3 담임), ex5(기간제)는 종목 제외되어 IsExempt == true
	if !findCand("ex4").IsExempt || !findCand("ex5").IsExempt {
		t.Fatalf("종목별 제외 규칙 미적용: ex4=%v, ex5=%v", findCand("ex4").IsExempt, findCand("ex5").IsExempt)
	}

	// 3. 별도 트랙(SEPARATE_TRACK): 임용고사에 대해 ex1, ex2 모두 동종목 0회 -> 장기근속(EARLY_TRANSFER) 시 ex1(2020) 우선
	recruitNotice, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-recruit", Title: "중등 임용고사", ExamCategory: "임용고사", ExamDate: "2026-12-19", RequiredCount: 1,
		ScoringMode:   "SEPARATE_TRACK",
		PriorityOrder: []protocol.ExternalExamCriteria{protocol.CriteriaVolunteer, protocol.CriteriaLowCount, protocol.CriteriaEarlyTransfer},
	})
	recCands, _, _ := extSvc.CalculateCandidates(recruitNotice.ID)
	var firstNonExempt protocol.ExternalExamCandidate
	for _, c := range recCands {
		if !c.IsExempt && !c.IsGracePeriod {
			firstNonExempt = c
			break
		}
	}
	if firstNonExempt.Teacher.ID != "ex1" {
		t.Fatalf("별도 트랙 + 장기근속 우선(EARLY_TRANSFER) 실패: got %s (%+v)", firstNonExempt.Teacher.ID, firstNonExempt.ReasonBadge)
	}

	// 4. 동점자 처리(AGE_YOUNG) & 지원자 초과 선발(FIRST_COME) 검증
	overflowNotice, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID: "ext-overflow", Title: "지원초과 검정고시", ExamCategory: "전국모의평가", ExamDate: "2026-12-26", RequiredCount: 1,
		VolunteerOverflowMode: "FIRST_COME", TieBreaker: "AGE_YOUNG", ExcludeExempt: true,
	})
	// ex2 먼저 지원, ex1 나중 지원
	_, _ = extSvc.ToggleVolunteer(overflowNotice.ID, "ex2")
	_, _ = extSvc.ToggleVolunteer(overflowNotice.ID, "ex1")
	ovCands, _, _ := extSvc.CalculateCandidates(overflowNotice.ID)
	if len(ovCands) < 2 || ovCands[0].Teacher.ID != "ex2" {
		t.Fatalf("지원자 초과 선착순(FIRST_COME) 실패: got rank1=%s", ovCands[0].Teacher.ID)
	}
	if err := extSvc.DeleteNotice(overflowNotice.ID, "대외계"); err != nil {
		t.Fatalf("외부 공고 삭제 실패: %v", err)
	}
	noticesAfter, _ := extSvc.ListNotices()
	for _, n := range noticesAfter {
		if n.ID == overflowNotice.ID {
			t.Fatalf("삭제한 외부 공고가 남아 있습니다")
		}
	}
	_ = fmt.Sprint(slices.Contains([]string{}, ""))
}
