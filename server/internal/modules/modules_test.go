package modules_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
	"kkomi-timetable/server/internal/exporter"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/external_exam"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

func setupTestEnv(t *testing.T) (*database.Store, *eventbus.Bus) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("KKOMI_BASE_DIR", tmpDir)

	store, err := database.Open()
	if err != nil {
		t.Fatalf("테스트 DB 열기 실패: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := database.SeedIfEmpty(store); err != nil {
		t.Fatalf("테스트 시드 실패: %v", err)
	}
	return store, eventbus.New()
}

// TestSourceFileLineLimit500은 프로젝트 내 모든 소스 파일(.go, .jsx, .js, .css)이 500줄 이하 원칙을 준수하는지 검증합니다.
func TestSourceFileLineLimit500(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	dirs := []string{
		filepath.Join(root, "pkg"),
		filepath.Join(root, "server"),
		filepath.Join(root, "client", "internal"),
		filepath.Join(root, "client", "frontend", "src"),
	}

	var violations []string
	checkFile := func(path string) {
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".jsx" && ext != ".js" && ext != ".css" {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("파일 읽기 실패(%s): %v", path, err)
		}
		lines := bytes.Count(raw, []byte{'\n'}) + 1
		if lines > 500 {
			violations = append(violations, path)
			t.Errorf("500줄 초과 위반: %s (%d줄)", path, lines)
		}
	}

	checkFile(filepath.Join(root, "client", "main.go"))
	for _, d := range dirs {
		_ = filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				checkFile(path)
			}
			return nil
		})
	}
	if len(violations) > 0 {
		t.Fatalf("총 %d개 파일이 500줄 이하 원칙을 위반했습니다", len(violations))
	}
}

// TestInternalExamCorridorPriorityAndHomeroomExclusion은 담임 자기반 배제,
// 담당과목 시험 교시 복도감독 우선배치, 학부모 명예감독 자녀 학년/반 배제를 검증합니다.
func TestInternalExamCorridorPriorityAndHomeroomExclusion(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := internal_exam.NewService(store, bus)

	cfg := protocol.ExamConfig{
		ID:                   "exam-test",
		Title:                "테스트 지필고사",
		ExamDate:             "2026-11-20",
		UnsealTime:           "08:20",
		IsSealed:             true,
		DeputyMode:           true,
		HonoraryParentOn:     true,
		SubjectTeacherPolicy: protocol.SubjectModeCorridorFirst,
	}
	specs := []internal_exam.ExamPeriodSpec{
		{Period: 1, Grade: 1, Subject: "국어", ClassRooms: []string{"1-1", "1-2"}},
		{Period: 2, Grade: 2, Subject: "수학", ClassRooms: []string{"2-1", "2-2"}},
	}

	slots, err := svc.GenerateAndSave(cfg, specs)
	if err != nil {
		t.Fatalf("감독 배정 실패: %v", err)
	}

	teachers, _ := database.LoadCollection[protocol.Teacher](store, "teachers")
	tMap := map[string]protocol.Teacher{}
	for _, tc := range teachers {
		tMap[tc.ID] = tc
	}

	corridorBySubjectCount := 0
	for _, sl := range slots {
		tc := tMap[sl.TeacherID]
		// 1. 담임교사 자기 반 감독 원천 제외 확인
		if sl.SlotRole == "MAIN" || sl.SlotRole == "SUB" {
			roomClass := strings.Split(sl.RoomName, " ")[0]
			if tc.HomeroomClass == roomClass {
				t.Fatalf("담임교사(%s)가 자기 반(%s)에 배정됨", tc.Name, roomClass)
			}
			if strings.EqualFold(tc.Subject, sl.Subject) {
				t.Fatalf("담당과목 교사(%s/%s)가 고사장(%s)에 배정됨", tc.Name, tc.Subject, sl.RoomName)
			}
		}
		// 2. 담당과목 시험 시 복도감독 우선배치 확인
		if sl.SlotRole == "CORRIDOR" && strings.EqualFold(tc.Subject, sl.Subject) {
			corridorBySubjectCount++
		}
		// 3. 학부모 명예감독 자녀 학년 배제 확인
		if sl.SlotRole == "HONORARY" {
			childGrade := strings.Split(tc.ParentOfStudentClass, "-")[0]
			if childGrade == string(rune('0'+sl.Grade)) {
				t.Fatalf("학부모 명예감독(%s)이 자녀 학년(%d학년)에 배정됨", tc.Name, sl.Grade)
			}
		}
	}
	if corridorBySubjectCount < 2 {
		t.Fatalf("담당과목 시험 교시 복도감독 우선 배치가 충분히 적용되지 않음 (배정수: %d)", corridorBySubjectCount)
	}
}

// TestInternalExamSealAndUnseal은 사전 봉인 시 일반 교사 패킷 마스킹과 당일 개봉 후 공개를 검증합니다.
func TestInternalExamSealAndUnseal(t *testing.T) {
	store, bus := setupTestEnv(t)
	svc := internal_exam.NewService(store, bus)

	unsealedEventCount := 0
	bus.Subscribe(protocol.EventExamUnsealed, func(evt protocol.DomainEvent) {
		unsealedEventCount++
	})

	slotsBefore, _, err := svc.ListSlotsForViewer(false)
	if err != nil || len(slotsBefore) == 0 {
		t.Fatalf("봉인 조회 실패: %v", err)
	}
	for _, sl := range slotsBefore {
		if !sl.ActualRoomHidden || !strings.Contains(sl.RoomName, "공개") {
			t.Fatalf("봉인 상태에서 일반 교사에게 고사장이 노출됨: %+v", sl)
		}
	}

	if _, err := svc.UnsealExam("", "박평가"); err != nil {
		t.Fatalf("개봉 실패: %v", err)
	}
	if unsealedEventCount != 1 {
		t.Fatalf("EventExamUnsealed 발행 횟수 오류: %d", unsealedEventCount)
	}

	slotsAfter, _, _ := svc.ListSlotsForViewer(false)
	for _, sl := range slotsAfter {
		if sl.ActualRoomHidden || strings.Contains(sl.RoomName, "🔒") {
			t.Fatalf("개봉 후에도 고사장이 마스킹되어 있음: %+v", sl)
		}
	}
}

// TestExternalExamMultiCriteriaAndTimetableLink는 다단계 차출 조합기 정렬 및
// 평일 파견 확정 시 시간표 결보강 대기열(STAGING) 자동 이관을 검증합니다.
func TestExternalExamMultiCriteriaAndTimetableLink(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	extSvc := external_exam.NewService(store, bus)

	// 방식 B(순번/쿨타임형): 지원자(t06) 1순위 -> 이후 미파견자/오래된 파견자 우선, 면제자(t12,t13,t14) 후순위
	cands, _, err := extSvc.CalculateCandidates("ext-2026-mock")
	if err != nil || len(cands) == 0 {
		t.Fatalf("외부감독 후보 산정 실패: %v", err)
	}
	if cands[0].Teacher.ID != "t06" || !cands[0].IsVolunteer {
		t.Fatalf("1순위 자발적 지원자(t06) 정렬 실패: %+v", cands[0])
	}

	// 방식 C(최근 전입자 우선형)로 규칙 변경 후 검증 (t05, t09 등 2026-03-01 전입자가 상위권)
	if _, err := extSvc.UpdateRules("ext-2026-mock", "PRESET_RECENT_TRANSFER", nil, true); err != nil {
		t.Fatalf("규칙 변경 실패: %v", err)
	}
	candsC, _, _ := extSvc.CalculateCandidates("ext-2026-mock")
	if candsC[1].Teacher.TransferDate != "2026-03-01" {
		t.Fatalf("최근 전입자 우선형 정렬 실패: %s", candsC[1].Teacher.TransferDate)
	}

	// 평일(월요일 2026-11-16) 시험 파견 확정 시 t02(월요일 수업 보유) 결보강 대기열 자동 이관 확인
	notice, _ := extSvc.CreateNotice(protocol.ExternalExamNotice{
		ID:            "ext-weekday-link",
		Title:         "평일 외부시험 테스트",
		ExamCategory:  "모의평가",
		ExamDate:      "2026-11-16", // 월요일
		Location:      "테스트고",
		RequiredCount: 2,
		PresetMode:    "PRESET_MIXED",
		ExcludeExempt: true,
	})
	if _, err := extSvc.ConfirmDispatch(notice.ID, []string{"t02"}, "최외부"); err != nil {
		t.Fatalf("파견 확정 실패: %v", err)
	}
	reqs, _ := ttSvc.ListRequests()
	foundTrip := false
	for _, rq := range reqs {
		if rq.RequestType == "EXTERNAL_TRIP" && rq.RequesterID == "t02" && rq.Status == protocol.StatusStaging {
			foundTrip = true
			break
		}
	}
	if !foundTrip {
		t.Fatalf("평일 외부감독 파견 확정 시 시간표 결보강 대기열 자동 이관이 누락되었습니다")
	}
}

// TestTimetableStagingAndCommitAndNEIS는 대기열(STAGING)과 담당자 최종 선택(Commit) 분리를 검증합니다.
func TestTimetableStagingAndCommitAndNEIS(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	calSvc := event_calendar.NewService(store, bus)

	// 행사 연동 테스트 (행사 담당교사 확정 시 시간표 블록 반영)
	ev, err := calSvc.ProposeEvent(protocol.SchoolEvent{
		ID:           "evt-test-block",
		Title:        "1학년 진로특강",
		Department:   "진로부",
		EventDate:    "2026-11-16",
		StartPeriod:  1,
		EndPeriod:    1,
		TargetGrades: []int{1},
		LinkType:     protocol.EventLinkClassBlock,
	})
	if err != nil {
		t.Fatalf("행사 등록 실패: %v", err)
	}
	if _, err := calSvc.CommitEvent(ev.ID, "정행사", true); err != nil {
		t.Fatalf("행사 확정 실패: %v", err)
	}

	// STAGING 신청 직후에는 APPROVED 목록에 나타나지 않음
	beforeNEIS, _ := ttSvc.NEISHelperList()
	beforeCnt := len(beforeNEIS)

	if _, err := ttSvc.CommitRequest("req-seed-1", "이수업", true, ""); err != nil {
		t.Fatalf("수업교환 최종 선택 실패: %v", err)
	}
	afterNEIS, _ := ttSvc.NEISHelperList()
	if len(afterNEIS) != beforeCnt+1 {
		t.Fatalf("최종 선택 후 나이스 입력 도우미 목록에 반영되지 않았습니다 (이전 %d -> 이후 %d)", beforeCnt, len(afterNEIS))
	}
}

// TestDocumentExporters는 .hwpx 및 .xlsx 출력 엔진의 유효성을 검증합니다.
func TestDocumentExporters(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)
	extSvc := external_exam.NewService(store, bus)

	hwpxBytes, err := exporter.BuildHWPXBytes(exporter.HWPXDocumentData{
		SchoolName: "꼬미고등학교",
		DocTitle:   "주중 학사일정표",
		PeriodText: "2026년 11월 3주",
		Headers:    []string{"순번", "일자", "행사명"},
		Rows:       [][]string{{"1", "2026-11-16", "수능 예비소집"}},
	})
	if err != nil {
		t.Fatalf("HWPX 생성 실패: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(hwpxBytes), int64(len(hwpxBytes)))
	if err != nil || len(zr.File) < 4 {
		t.Fatalf("생성된 HWPX ZIP 구조 오류: %v", err)
	}

	reqs, _ := ttSvc.ListRequests()
	xlsxBytes, err := exporter.BuildTimetableChangesXLSX(reqs)
	if err != nil {
		t.Fatalf("결보강 XLSX 생성 실패: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(xlsxBytes))
	if err != nil {
		t.Fatalf("생성된 XLSX 열기 실패: %v", err)
	}
	_ = f.Close()

	cands, notice, _ := extSvc.CalculateCandidates("ext-2026-mock")
	extXlsx, err := exporter.BuildExternalExamDispatchXLSX(*notice, cands)
	if err != nil {
		t.Fatalf("외부감독 파견 명단 XLSX 생성 실패: %v", err)
	}
	if bytes.Contains(extXlsx, []byte("계좌")) || bytes.Contains(extXlsx, []byte("수당")) {
		t.Fatalf("제거되어야 할 계좌/수당 문자열이 엑셀 출력에 포함되어 있습니다")
	}
}

// TestAuthLoginAndPasswordHashSafety는 로그인 인증 및 반환 DTO의 비밀번호 해시 제거를 검증합니다.
func TestAuthLoginAndPasswordHashSafety(t *testing.T) {
	store, _ := setupTestEnv(t)
	authSvc := auth.NewService(store)

	user, err := authSvc.Authenticate("t06", "1234")
	if err != nil {
		t.Fatalf("초기 계정 로그인 실패: %v", err)
	}
	if user.PasswordHash != "" {
		t.Fatalf("API 응답용 Teacher 객체에 PasswordHash가 남아 있습니다")
	}
	if _, err := authSvc.Authenticate("t06", "wrong-pass"); err == nil {
		t.Fatalf("잘못된 비밀번호 로그인이 차단되지 않았습니다")
	}
}

// TestBaseTimetableBuilderAndAutoGenerate는 학년·학급별 기초 시간표 작성, 중복·순회교사 차단,
// 일괄 자동 편성 및 엑셀 템플릿 임포트를 검증합니다.
func TestBaseTimetableBuilderAndAutoGenerate(t *testing.T) {
	store, bus := setupTestEnv(t)
	ttSvc := timetable.NewService(store, bus)

	// 1. 순회교사(t11: 월·수·금만 출강) 화요일(2) 배치 시 차단 확인
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 1, ClassName: "1-3", Grade: 1, Subject: "정보", TeacherID: "t11",
	}); err == nil {
		t.Fatalf("순회교사 비출강일 배치가 차단되지 않았습니다")
	}

	// 2. 정상 셀 저장 및 삭제
	saved, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 5, ClassName: "1-3", Grade: 1, Subject: "국어", TeacherID: "t06",
	})
	if err != nil {
		t.Fatalf("기초 시간표 셀 저장 실패: %v", err)
	}
	// 같은 시간 다른 반(2-3)에 t06 중복 배치 시도 시 차단 확인
	if _, err := ttSvc.SaveCell(protocol.TimetableCell{
		DayOfWeek: 2, Period: 5, ClassName: "2-3", Grade: 2, Subject: "국어", TeacherID: "t06",
	}); err == nil {
		t.Fatalf("동시간대 교사 중복 배치가 차단되지 않았습니다")
	}
	if err := ttSvc.DeleteCell(saved.ID); err != nil {
		t.Fatalf("셀 삭제 실패: %v", err)
	}

	// 3. 학년별·학급별 기초 시간표 자동 편성 검증 (1~3학년 각 2학급)
	created, err := ttSvc.AutoGenerateBaseTimetable([]int{1, 2, 3}, 2, true)
	if err != nil || len(created) == 0 {
		t.Fatalf("학년·학급별 자동 편성 실패: len=%d, err=%v", len(created), err)
	}

	// 4. 기초 시간표 엑셀 템플릿 생성 및 재임포트 검증
	tplBytes, err := timetable.BuildBaseTemplateXLSX()
	if err != nil || len(tplBytes) == 0 {
		t.Fatalf("기초 시간표 엑셀 양식 생성 실패: %v", err)
	}
	imported, err := ttSvc.ImportTimetableXLSX(bytes.NewReader(tplBytes))
	if err != nil || imported == 0 {
		t.Fatalf("기초 시간표 엑셀 업로드 실패: imported=%d, err=%v", imported, err)
	}
}

// TestSystemResetAndPasswordChangeAndExamDuration은 admin/1234 최초 비밀번호 변경,
// 전체/학기 초기화, 교내 지필고사 과목별 시험시간 배정을 검증합니다.
func TestSystemResetAndPasswordChangeAndExamDuration(t *testing.T) {
	store, bus := setupTestEnv(t)
	authSvc := auth.NewService(store)
	examSvc := internal_exam.NewService(store, bus)

	// 1. admin / 1234 로그인 시 MustChangePassword=true 확인 후 변경 -> false 확인
	admin, err := authSvc.Authenticate("admin", "1234")
	if err != nil || !admin.MustChangePassword {
		t.Fatalf("초기 admin 로그인 또는 비밀번호 변경 플래그 오류: err=%v, admin=%+v", err, admin)
	}
	updated, err := authSvc.ChangePassword(admin.ID, "1234", "kkomi2026")
	if err != nil || updated.MustChangePassword {
		t.Fatalf("비밀번호 변경 실패: err=%v", err)
	}
	if err := authSvc.ResetTeacherPassword(admin.ID); err != nil {
		t.Fatalf("비밀번호 초기화 실패: %v", err)
	}

	// 2. 과목별 상이 시험시간(35분/50분) 반영 검증
	slots, err := examSvc.GenerateAndSave(protocol.ExamConfig{
		ID: "exam-dur-test", Title: "시험시간 테스트", ExamDate: "2026-11-26", InstantRevealMode: true,
	}, []internal_exam.ExamPeriodSpec{
		{Period: 1, Grade: 1, Subject: "국어", StartTime: "08:50", EndTime: "09:25", DurationMinutes: 35, ClassRooms: []string{"1-1"}},
		{Period: 2, Grade: 2, Subject: "수학", StartTime: "09:40", EndTime: "10:30", DurationMinutes: 50, ClassRooms: []string{"2-1"}},
	})
	if err != nil || len(slots) < 2 {
		t.Fatalf("과목별 시험시간 배정 실패: %v", err)
	}
	if slots[0].DurationMinutes != 35 || slots[0].StartTime != "08:50" {
		t.Fatalf("1교시 시험시간 미반영: %+v", slots[0])
	}

	// 3. 학기 초기화 -> 교사 유지 + 시간표 삭제, 완전 초기화 -> admin만 남음
	if _, err := store.ResetSystem(database.ResetModeAcademicTerm); err != nil {
		t.Fatalf("학기 초기화 실패: %v", err)
	}
	teachersAfterTerm, _ := authSvc.ListTeachers()
	if len(teachersAfterTerm) <= 1 {
		t.Fatalf("학기 초기화 후 교사가 유지되지 않았습니다: %d", len(teachersAfterTerm))
	}
	if _, err := store.ResetSystem(database.ResetModeFullFactory); err != nil {
		t.Fatalf("완전 초기화 실패: %v", err)
	}
	teachersAfterFull, _ := authSvc.ListTeachers()
	if len(teachersAfterFull) != 1 || teachersAfterFull[0].Username != "admin" {
		t.Fatalf("완전 초기화 후 admin 단독 계정 상태가 아닙니다: %+v", teachersAfterFull)
	}
}
