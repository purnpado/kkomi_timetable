package database

import (
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
)

// SeedIfEmpty는 DB가 비어 있을 때 중·고등학교 교무실 시나리오에 맞는 초기 데이터를 시딩합니다.
func SeedIfEmpty(s *Store) error {
	cnt, err := s.CountCollection("teachers")
	if err != nil || cnt > 0 {
		return err
	}

	hashBytes, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.MinCost)
	if err != nil {
		return err
	}
	defaultHash := string(hashBytes)

	teachers := buildSeedTeachers(defaultHash)
	for _, t := range teachers {
		if err := s.SaveRecord("teachers", t.ID, t); err != nil {
			return err
		}
	}

	for _, b := range DefaultBellSchedule() {
		id := fmt.Sprintf("period-%d", b.Period)
		if err := s.SaveRecord("bell_schedule", id, b); err != nil {
			return err
		}
	}

	cfg := DefaultSchoolConfig()
	if err := s.SaveRecord("school_configs", cfg.ID, cfg); err != nil {
		return err
	}
	for _, ld := range buildSeedTeachingLoads() {
		if err := s.SaveRecord("teaching_loads", ld.ID, ld); err != nil {
			return err
		}
	}

	for _, cell := range buildSeedTimetable() {
		if err := s.SaveRecord("timetable_cells", cell.ID, cell); err != nil {
			return err
		}
	}

	for _, req := range buildSeedTimetableRequests() {
		if err := s.SaveRecord("timetable_requests", req.ID, req); err != nil {
			return err
		}
	}

	today := time.Now().Format("2006-01-02")
	examCfg := protocol.ExamConfig{
		ID:                   "exam-2026-2-final",
		Title:                "2026학년도 2학기 기말고사",
		ExamDate:             today,
		UnsealTime:           "08:20",
		IsSealed:             true,
		InstantRevealMode:    false,
		DeputyMode:           true,
		BalanceMode:          "TOTAL_AND_ROLE",
		HonoraryParentOn:     true,
		SubjectTeacherPolicy: protocol.SubjectModeCorridorFirst,
	}
	if err := s.SaveRecord("exam_configs", examCfg.ID, examCfg); err != nil {
		return err
	}

	for _, ext := range buildSeedExternalExams(today) {
		if err := s.SaveRecord("external_exams", ext.ID, ext); err != nil {
			return err
		}
	}

	for _, ev := range buildSeedSchoolEvents(today) {
		if err := s.SaveRecord("school_events", ev.ID, ev); err != nil {
			return err
		}
	}

	return nil
}

func buildSeedTeachers(pwHash string) []protocol.Teacher {
	return []protocol.Teacher{
		{
			ID: "t01", Username: "admin", PasswordHash: pwHash, MustChangePassword: true, Name: "김교무", Position: "교무부장",
			IsDepartmentHead: true, Subject: "국어", HomeroomClass: "", Phone: "010-2100-0001",
			Roles: []protocol.Role{protocol.RoleMaster, protocol.RoleTimetableAdmin, protocol.RoleInternalExamAdmin, protocol.RoleExternalExamAdmin, protocol.RoleEventAdmin, protocol.RoleTeacher},
			TransferDate: "2022-03-01", LastExternalDispatch: "2024-11-14", ExternalExamCount: 2, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 2,
		},
		{
			ID: "t02", Username: "timetable", PasswordHash: pwHash, MustChangePassword: true, Name: "이수업", Position: "교무기획(수업계)",
			Subject: "수학", HomeroomClass: "2-1", Phone: "010-2100-0002",
			Roles: []protocol.Role{protocol.RoleTimetableAdmin, protocol.RoleTeacher},
			TransferDate: "2025-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 5,
		},
		{
			ID: "t03", Username: "internal", PasswordHash: pwHash, MustChangePassword: true, Name: "박평가", Position: "연구부장(평가계)",
			IsDepartmentHead: true, Subject: "영어", HomeroomClass: "", Phone: "010-2100-0003",
			Roles: []protocol.Role{protocol.RoleInternalExamAdmin, protocol.RoleTeacher},
			TransferDate: "2023-03-01", LastExternalDispatch: "2025-11-13", ExternalExamCount: 1, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 1,
		},
		{
			ID: "t04", Username: "external", PasswordHash: pwHash, MustChangePassword: true, Name: "최외부", Position: "교무기획(대외계)",
			Subject: "과학", ShortSubject: "과학", DefaultRoom: "과학실1", HomeroomClass: "3-1", Phone: "010-2100-0004",
			Roles: []protocol.Role{protocol.RoleExternalExamAdmin, protocol.RoleTeacher},
			TransferDate: "2024-03-01", LastExternalDispatch: "2024-06-04", ExternalExamCount: 2, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 3,
		},
		{
			ID: "t05", Username: "event", PasswordHash: pwHash, MustChangePassword: true, Name: "정행사", Position: "교무기획(행사계)",
			Subject: "사회", HomeroomClass: "1-1", Phone: "010-2100-0005",
			Roles: []protocol.Role{protocol.RoleEventAdmin, protocol.RoleTeacher},
			TransferDate: "2026-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 2,
		},
		{
			ID: "t06", Username: "t06", PasswordHash: pwHash, MustChangePassword: true, Name: "강국어", Position: "평교사",
			Subject: "국어", HomeroomClass: "1-2", Phone: "010-2100-0006",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2026-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 4,
		},
		{
			ID: "t07", Username: "t07", PasswordHash: pwHash, Name: "조수학", Position: "평교사",
			Subject: "수학", HomeroomClass: "2-2", Phone: "010-2100-0007",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2023-03-01", LastExternalDispatch: "2023-11-16", ExternalExamCount: 1, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 3,
		},
		{
			ID: "t08", Username: "t08", PasswordHash: pwHash, Name: "윤영어", Position: "평교사",
			Subject: "영어", HomeroomClass: "3-2", Phone: "010-2100-0008",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2025-03-01", LastExternalDispatch: "2025-04-05", ExternalExamCount: 1, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 2,
		},
		{
			ID: "t09", Username: "t09", PasswordHash: pwHash, Name: "한과학", Position: "평교사",
			Subject: "과학", ShortSubject: "과학", DefaultRoom: "과학실1", HomeroomClass: "", Phone: "010-2100-0009",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2026-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 1,
		},
		{
			ID: "t10", Username: "t10", PasswordHash: pwHash, Name: "오역사", Position: "평교사",
			Subject: "역사", ShortSubject: "역사", HomeroomClass: "", Phone: "010-2100-0010",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2022-03-01", LastExternalDispatch: "2023-06-01", ExternalExamCount: 2, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 6,
		},
		{
			ID: "t11", Username: "t11", PasswordHash: pwHash, Name: "서정보", Position: "순회교사",
			Subject: "정보", ShortSubject: "정보", DefaultRoom: "컴퓨터실", HomeroomClass: "", Phone: "010-2100-0011",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2025-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 0,
			ItinerantDays: []int{1, 3, 5}, // 월·수·금만 출강
		},
		{
			ID: "t12", Username: "t12", PasswordHash: pwHash, Name: "송체육", Position: "생활안전부장",
			IsDepartmentHead: true, Subject: "체육", ShortSubject: "체육", DefaultRoom: "체육관", HomeroomClass: "", Phone: "010-2100-0012",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2021-03-01", LastExternalDispatch: "2024-11-14", ExternalExamCount: 3, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 2,
			ExemptReasons: []string{"원로교사"},
		},
		{
			ID: "t13", Username: "t13", PasswordHash: pwHash, Name: "배음악", Position: "평교사",
			Subject: "음악", ShortSubject: "음악", DefaultRoom: "음악실", HomeroomClass: "", Phone: "010-2100-0013",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2024-03-01", LastExternalDispatch: "", ExternalExamCount: 0, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 1,
			ExemptReasons: []string{"임산부"},
		},
		{
			ID: "t14", Username: "t14", PasswordHash: pwHash, Name: "임미술", Position: "평교사",
			Subject: "미술", ShortSubject: "미술", DefaultRoom: "미술실", HomeroomClass: "", Phone: "010-2100-0014",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2024-09-01", LastExternalDispatch: "2025-09-03", ExternalExamCount: 1, InternalExamCount: 2, AnnualMainCount: 1, AnnualSubCount: 1, SubstitutionCount: 2,
			ExemptReasons: []string{"수험생 자녀"},
		},
		{
			ID: "t15", Username: "t15", PasswordHash: pwHash, Name: "박특수", Position: "특수교사",
			Subject: "특수학급", ShortSubject: "특수", HomeroomClass: "", Phone: "010-2100-0015",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2023-03-01", ExternalExamCount: 0, InternalExamCount: 0,
			InternalExamExempt: true, InternalExamExemptReason: "특수반(개별고사장 전담)",
			ExemptReasons: []string{"특수반전담"},
		},
		{
			ID: "t16", Username: "t16", PasswordHash: pwHash, Name: "박별도", Position: "별도고사실 전담교사",
			Subject: "별도고사실", ShortSubject: "별도", HomeroomClass: "", Phone: "010-2100-0016",
			Roles: []protocol.Role{protocol.RoleTeacher},
			TransferDate: "2023-03-01", ExternalExamCount: 0, InternalExamCount: 0,
			InternalExamExempt: true, InternalExamExemptReason: "별도 고사실(독감·감염병 등 분리시험)",
			ExemptReasons: []string{"별도고사실전담"},
		},
		{
			ID: "p01", Username: "parent1", PasswordHash: pwHash, Name: "김학부모(명예)", Position: "학부모 명예감독",
			Subject: "명예감독", HomeroomClass: "", Phone: "010-3300-1001",
			Roles: []protocol.Role{protocol.RoleTeacher},
			IsParentHonorary: true, ParentOfStudentClass: "2-1",
		},
		{
			ID: "p02", Username: "parent2", PasswordHash: pwHash, Name: "이학부모(명예)", Position: "학부모 명예감독",
			Subject: "명예감독", HomeroomClass: "", Phone: "010-3300-1002",
			Roles: []protocol.Role{protocol.RoleTeacher},
			IsParentHonorary: true, ParentOfStudentClass: "1-2",
		},
	}
}

// DefaultBellSchedule은 기본 1~7교시 일과시간표를 반환합니다.
func DefaultBellSchedule() []protocol.BellPeriod {
	return []protocol.BellPeriod{
		{Period: 1, Label: "1교시", StartTime: "08:40", EndTime: "09:30"},
		{Period: 2, Label: "2교시", StartTime: "09:40", EndTime: "10:30"},
		{Period: 3, Label: "3교시", StartTime: "10:40", EndTime: "11:30"},
		{Period: 4, Label: "4교시", StartTime: "11:40", EndTime: "12:30"},
		{Period: 5, Label: "5교시", StartTime: "13:30", EndTime: "14:20"},
		{Period: 6, Label: "6교시", StartTime: "14:30", EndTime: "15:20"},
		{Period: 7, Label: "7교시", StartTime: "15:30", EndTime: "16:20"},
	}
}

func buildSeedTimetable() []protocol.TimetableCell {
	return []protocol.TimetableCell{
		// 월요일(1) - 1-1, 1-2, 2-1, 2-2, 3-1
		{ID: "c-1-1-11", DayOfWeek: 1, Period: 1, ClassName: "1-1", Grade: 1, Subject: "국어", SubjectCode: "KOR101", TeacherID: "t06", Room: "1-1교실"},
		{ID: "c-1-2-11", DayOfWeek: 1, Period: 2, ClassName: "1-1", Grade: 1, Subject: "사회", SubjectCode: "SOC101", TeacherID: "t05", Room: "1-1교실"},
		{ID: "c-1-3-11", DayOfWeek: 1, Period: 3, ClassName: "1-1", Grade: 1, Subject: "정보", SubjectCode: "INF101", TeacherID: "t11", Room: "컴퓨터실"},
		{ID: "c-1-4-11", DayOfWeek: 1, Period: 4, ClassName: "1-1", Grade: 1, Subject: "과학", SubjectCode: "SCI101", TeacherID: "t09", CoTeacherID: "t04", Room: "과학실1"},
		{ID: "c-1-5-11", DayOfWeek: 1, Period: 5, ClassName: "1-1", Grade: 1, Subject: "수학", SubjectCode: "MAT101", TeacherID: "t02", Room: "1-1교실"},
		{ID: "c-1-6-11", DayOfWeek: 1, Period: 6, ClassName: "1-1", Grade: 1, Subject: "영어", SubjectCode: "ENG101", TeacherID: "t08", Room: "1-1교실"},

		{ID: "c-1-1-21", DayOfWeek: 1, Period: 1, ClassName: "2-1", Grade: 2, Subject: "수학", SubjectCode: "MAT201", TeacherID: "t02", Room: "2-1교실"},
		{ID: "c-1-2-21", DayOfWeek: 1, Period: 2, ClassName: "2-1", Grade: 2, Subject: "영어", SubjectCode: "ENG201", TeacherID: "t03", Room: "2-1교실"},
		{ID: "c-1-3-21", DayOfWeek: 1, Period: 3, ClassName: "2-1", Grade: 2, Subject: "국어", SubjectCode: "KOR201", TeacherID: "t01", Room: "2-1교실"},
		{ID: "c-1-4-21", DayOfWeek: 1, Period: 4, ClassName: "2-1", Grade: 2, Subject: "역사", SubjectCode: "HIS201", TeacherID: "t10", Room: "2-1교실"},
		{ID: "c-1-5-21", DayOfWeek: 1, Period: 5, ClassName: "2-1", Grade: 2, Subject: "선택A(심화수학)", SubjectCode: "ELM201", TeacherID: "t07", Room: "세미나실A", ElectiveGroup: "G2-EL-A"},
		{ID: "c-1-6-21", DayOfWeek: 1, Period: 6, ClassName: "2-1", Grade: 2, Subject: "체육", SubjectCode: "PHY201", TeacherID: "t12", Room: "체육관"},

		// 화~금 주요 시간표
		{ID: "c-2-1-21", DayOfWeek: 2, Period: 1, ClassName: "2-1", Grade: 2, Subject: "국어", SubjectCode: "KOR201", TeacherID: "t01", Room: "2-1교실"},
		{ID: "c-2-2-21", DayOfWeek: 2, Period: 2, ClassName: "2-1", Grade: 2, Subject: "수학", SubjectCode: "MAT201", TeacherID: "t02", Room: "2-1교실"},
		{ID: "c-2-3-21", DayOfWeek: 2, Period: 3, ClassName: "2-1", Grade: 2, Subject: "과학", SubjectCode: "SCI201", TeacherID: "t04", Room: "과학실2"},
		{ID: "c-2-4-21", DayOfWeek: 2, Period: 4, ClassName: "2-1", Grade: 2, Subject: "음악", SubjectCode: "MUS201", TeacherID: "t13", Room: "음악실"},

		{ID: "c-3-1-31", DayOfWeek: 3, Period: 1, ClassName: "3-1", Grade: 3, Subject: "과학", SubjectCode: "SCI301", TeacherID: "t04", Room: "3-1교실"},
		{ID: "c-3-2-31", DayOfWeek: 3, Period: 2, ClassName: "3-1", Grade: 3, Subject: "영어", SubjectCode: "ENG301", TeacherID: "t08", Room: "3-1교실"},
		{ID: "c-3-3-31", DayOfWeek: 3, Period: 3, ClassName: "3-1", Grade: 3, Subject: "수학", SubjectCode: "MAT301", TeacherID: "t07", Room: "3-1교실"},
		{ID: "c-3-4-31", DayOfWeek: 3, Period: 4, ClassName: "3-1", Grade: 3, Subject: "국어", SubjectCode: "KOR301", TeacherID: "t06", Room: "3-1교실"},
		{ID: "c-4-2-22", DayOfWeek: 4, Period: 2, ClassName: "2-2", Grade: 2, Subject: "수학", SubjectCode: "MAT201", TeacherID: "t07", Room: "2-2교실"},
		{ID: "c-5-3-12", DayOfWeek: 5, Period: 3, ClassName: "1-2", Grade: 1, Subject: "국어", SubjectCode: "KOR101", TeacherID: "t06", Room: "1-2교실"},
		{ID: "c-5-4-12", DayOfWeek: 5, Period: 4, ClassName: "1-2", Grade: 1, Subject: "정보", SubjectCode: "INF101", TeacherID: "t11", Room: "컴퓨터실"},
	}
}

func buildSeedTimetableRequests() []protocol.TimetableChangeRequest {
	today := time.Now().Format("2006-01-02")
	return []protocol.TimetableChangeRequest{
		{
			ID:            "req-seed-1",
			RequestType:   "SWAP",
			TargetDate:    today,
			RequesterID:   "t02",
			RequesterName: "이수업",
			PartnerID:     "t03",
			PartnerName:   "박평가",
			PartnerAgreed: true,
			SourceCell:    protocol.TimetableCell{ID: "c-1-1-21", DayOfWeek: 1, Period: 1, ClassName: "2-1", Grade: 2, Subject: "수학", SubjectCode: "MAT201", TeacherID: "t02", Room: "2-1교실"},
			TargetCell:    protocol.TimetableCell{ID: "c-1-2-21", DayOfWeek: 1, Period: 2, ClassName: "2-1", Grade: 2, Subject: "영어", SubjectCode: "ENG201", TeacherID: "t03", Room: "2-1교실"},
			Reason:        "2-1 수학 평가 사전 협의",
			Status:        protocol.StatusStaging,
			CreatedAt:     time.Now().Format("2006-01-02 08:05:00"),
		},
	}
}

func buildSeedExternalExams(today string) []protocol.ExternalExamNotice {
	return []protocol.ExternalExamNotice{
		{
			ID:            "ext-2027-csat",
			Title:         "2027학년도 대학수학능력시험 감독관 파견",
			ExamCategory:  "수능",
			ExamDate:      "2026-11-19",
			Location:      "관내 제1시험장(중앙고)",
			RequiredCount: 6,
			PresetMode:    "PRESET_MIXED",
			PriorityOrder: []protocol.ExternalExamCriteria{
				protocol.CriteriaVolunteer,
				protocol.CriteriaLowCount,
				protocol.CriteriaLateTransfer,
				protocol.CriteriaOldestDispatch,
			},
			ExcludeExempt: true,
			VolunteerIDs:  []string{"t09", "t05"},
		},
		{
			ID:            "ext-2026-mock",
			Title:         "11월 고2 전국연합학력평가 교외 파견",
			ExamCategory:  "모의평가",
			ExamDate:      today,
			Location:      "관내 협력고등학교",
			RequiredCount: 3,
			PresetMode:    "PRESET_COOLDOWN",
			PriorityOrder: []protocol.ExternalExamCriteria{
				protocol.CriteriaVolunteer,
				protocol.CriteriaOldestDispatch,
				protocol.CriteriaLateTransfer,
				protocol.CriteriaLowCount,
			},
			ExcludeExempt: true,
			VolunteerIDs:  []string{"t06"},
		},
	}
}

func buildSeedSchoolEvents(today string) []protocol.SchoolEvent {
	return []protocol.SchoolEvent{
		{
			ID:           "evt-seed-1",
			Title:        "2학년 진로체험 및 창의융합 박람회",
			Department:   "진로진학부",
			EventDate:    today,
			StartPeriod:  5,
			EndPeriod:    6,
			TargetGrades: []int{2},
			LeadTeachers: []string{"t07", "t10"},
			LinkType:     protocol.EventLinkClassBlock,
			Description:  "2학년 5~6교시 정규 수업 블록 후 강당 집결",
			Status:       protocol.StatusApproved,
			ProposedBy:   "정행사",
			CommittedBy:  "정행사",
		},
		{
			ID:           "evt-seed-2",
			Title:        "학생생활안전부 교내 흡연예방 교육(대기열)",
			Department:   "학생생활안전부",
			EventDate:    today,
			StartPeriod:  7,
			EndPeriod:    7,
			TargetGrades: []int{1},
			LeadTeachers: []string{"t12"},
			LinkType:     protocol.EventLinkTeacherTrip,
			Description:  "행사 담당교사 최종 선택 대기 중",
			Status:       protocol.StatusStaging,
			ProposedBy:   "송체육",
		},
	}
}

// DefaultSchoolConfig는 기본 학년별 학급수·요일별 기준시수·교과교실 설정을 반환합니다.
func DefaultSchoolConfig() protocol.SchoolConfig {
	return protocol.SchoolConfig{
		ID:                 "default",
		SchoolName:         "꼬미중·고등학교",
		AcademicYear:       2026,
		GradeClassCounts:   map[int]int{1: 2, 2: 2, 3: 2},
		VirtualClassCounts: map[int]int{1: 0, 2: 1, 3: 0},
		DailyPeriodLimits: map[int]map[int]int{
			1: {1: 6, 2: 6, 3: 6, 4: 6, 5: 6},
			2: {1: 6, 2: 6, 3: 6, 4: 6, 5: 6},
			3: {1: 6, 2: 6, 3: 6, 4: 6, 5: 6},
		},
		SpecialRooms: []protocol.SpecialRoom{
			{ID: "rm-sci1", Name: "과학실1", Subject: "과학", Capacity: 35},
			{ID: "rm-comp", Name: "컴퓨터실", Subject: "정보", Capacity: 32},
			{ID: "rm-music", Name: "음악실", Subject: "음악", Capacity: 35},
			{ID: "rm-art", Name: "미술실", Subject: "미술", Capacity: 35},
			{ID: "rm-gym", Name: "체육관", Subject: "체육", Capacity: 70},
			{ID: "rm-math1", Name: "수학전용1실", Subject: "수학", Capacity: 30},
		},
	}
}

func buildSeedTeachingLoads() []protocol.TeachingLoad {
	return []protocol.TeachingLoad{
		{ID: "ld-1-kor", TeacherID: "t06", TeacherName: "강국어", Subject: "국어", ShortSubject: "국어", SubjectCode: "KOR101", Grade: 1, ClassHours: map[string]int{"1-1": 4, "1-2": 4}, TotalHours: 8, BlockHours: 1},
		{ID: "ld-1-mat", TeacherID: "t02", TeacherName: "이수업", Subject: "수학", ShortSubject: "수학", SubjectCode: "MAT101", Grade: 1, ClassHours: map[string]int{"1-1": 4, "1-2": 4}, TotalHours: 8, BlockHours: 1, DedicatedRoom: "수학전용1실"},
		{ID: "ld-1-eng", TeacherID: "t08", TeacherName: "윤영어", Subject: "영어", ShortSubject: "영어", SubjectCode: "ENG101", Grade: 1, ClassHours: map[string]int{"1-1": 4, "1-2": 4}, TotalHours: 8, BlockHours: 1},
		{ID: "ld-1-soc", TeacherID: "t05", TeacherName: "정행사", Subject: "사회", ShortSubject: "사회", SubjectCode: "SOC101", Grade: 1, ClassHours: map[string]int{"1-1": 3, "1-2": 3}, TotalHours: 6, BlockHours: 1},
		{ID: "ld-1-sci", TeacherID: "t09", TeacherName: "한과학", Subject: "과학", ShortSubject: "과학", SubjectCode: "SCI101", Grade: 1, ClassHours: map[string]int{"1-1": 4, "1-2": 4}, TotalHours: 8, BlockHours: 1, DedicatedRoom: "과학실1"},
		{ID: "ld-1-inf", TeacherID: "t11", TeacherName: "서정보", Subject: "정보", ShortSubject: "정보", SubjectCode: "INF101", Grade: 1, ClassHours: map[string]int{"1-1": 2, "1-2": 2}, TotalHours: 4, BlockHours: 2, DedicatedRoom: "컴퓨터실"},
		{ID: "ld-1-his", TeacherID: "t10", TeacherName: "오역사", Subject: "역사", ShortSubject: "역사", SubjectCode: "HIS101", Grade: 1, ClassHours: map[string]int{"1-1": 3, "1-2": 3}, TotalHours: 6, BlockHours: 1},
		{ID: "ld-1-pe", TeacherID: "t12", TeacherName: "송체육", Subject: "체육", ShortSubject: "체육", SubjectCode: "PHY101", Grade: 1, ClassHours: map[string]int{"1-1": 3, "1-2": 3}, TotalHours: 6, BlockHours: 1, DedicatedRoom: "체육관"},
		{ID: "ld-1-mus", TeacherID: "t13", TeacherName: "배음악", Subject: "음악", ShortSubject: "음악", SubjectCode: "MUS101", Grade: 1, ClassHours: map[string]int{"1-1": 2, "1-2": 2}, TotalHours: 4, BlockHours: 1, DedicatedRoom: "음악실"},
		{ID: "ld-1-art", TeacherID: "t14", TeacherName: "임미술", Subject: "미술", ShortSubject: "미술", SubjectCode: "ART101", Grade: 1, ClassHours: map[string]int{"1-1": 1, "1-2": 1}, TotalHours: 2, BlockHours: 1, DedicatedRoom: "미술실"},
		{ID: "ld-2-kor", TeacherID: "t01", TeacherName: "김교무", Subject: "국어", ShortSubject: "국어", SubjectCode: "KOR201", Grade: 2, ClassHours: map[string]int{"2-1": 4, "2-2": 4}, TotalHours: 8, BlockHours: 1},
		{ID: "ld-2-mat", TeacherID: "t07", TeacherName: "조수학", Subject: "수학", ShortSubject: "수학", SubjectCode: "MAT201", Grade: 2, ClassHours: map[string]int{"2-1": 4, "2-2": 4}, TotalHours: 8, BlockHours: 1, DedicatedRoom: "수학전용1실"},
		{ID: "ld-2-eng", TeacherID: "t03", TeacherName: "박평가", Subject: "영어", ShortSubject: "영어", SubjectCode: "ENG201", Grade: 2, ClassHours: map[string]int{"2-1": 4, "2-2": 4}, TotalHours: 8, BlockHours: 1},
		{ID: "ld-2-sci", TeacherID: "t04", TeacherName: "최외부", Subject: "과학", ShortSubject: "과학", SubjectCode: "SCI201", Grade: 2, ClassHours: map[string]int{"2-1": 4, "2-2": 4}, TotalHours: 8, BlockHours: 1, DedicatedRoom: "과학실1"},
	}
}
