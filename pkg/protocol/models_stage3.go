package protocol

// ExternalExamCategoryDef는 외부 시험감독 종목(기본 3종 + 담당자 자유 추가) 및 가중치 정의입니다.
type ExternalExamCategoryDef struct {
	Name                string   `json:"name"`                          // 예: "검정고시", "수능감독", "임용고사"
	Weight              float64  `json:"weight"`                        // 가중치 점수 (예: 1.0, 2.0, 1.5)
	DefaultExcludeRules []string `json:"defaultExcludeRules,omitempty"` // 해당 종목 기본 제외 규칙 코드
}

// ExternalExamSchoolConfig는 학교별 외부 시험감독 기본 내규 프리셋 및 종목 정의입니다.
type ExternalExamSchoolConfig struct {
	ID                       string                    `json:"id"`
	Categories               []ExternalExamCategoryDef `json:"categories"`
	DefaultPresetMode        string                    `json:"defaultPresetMode"`        // PRESET_MIXED, PRESET_COOLDOWN, PRESET_RECENT_TRANSFER, CUSTOM
	DefaultScoringMode       string                    `json:"defaultScoringMode"`       // TOTAL_COUNT, WEIGHTED_SCORE, SEPARATE_TRACK
	DefaultPriorityOrder     []ExternalExamCriteria    `json:"defaultPriorityOrder"`     // 기본 다단계 우선순위
	DefaultTransferGraceYear bool                      `json:"defaultTransferGraceYear"` // 전입 1년차 유예 기본값
	DefaultTieBreaker        string                    `json:"defaultTieBreaker"`        // NAME_ASC, AGE_YOUNG, AGE_OLD, SALARY_LOW, SALARY_HIGH, LOTTERY
	DefaultVolunteerOverflow string                    `json:"defaultVolunteerOverflow"` // PRIORITY_ORDER, LOW_COUNT_FIRST, FIRST_COME, LOTTERY
	DefaultExcludeExempt     bool                      `json:"defaultExcludeExempt"`     // 면제/종목 제외자 후순위 제외 기본값
}

// DefaultExternalExamSchoolConfig는 기본 3종 외부 시험(검정고시·수능감독·임용고사) 및 표준 내규 프리셋을 반환합니다.
func DefaultExternalExamSchoolConfig() ExternalExamSchoolConfig {
	return ExternalExamSchoolConfig{
		ID: "default",
		Categories: []ExternalExamCategoryDef{
			{Name: "검정고시", Weight: 1.0},
			{Name: "수능감독", Weight: 2.0, DefaultExcludeRules: []string{"GRADE3_HOMEROOM", "EXAM_CHILD", "CONTRACT_TEACHER"}},
			{Name: "임용고사", Weight: 1.5},
		},
		DefaultPresetMode:        "PRESET_MIXED",
		DefaultScoringMode:       "TOTAL_COUNT",
		DefaultPriorityOrder:     []ExternalExamCriteria{CriteriaVolunteer, CriteriaLowCount, CriteriaLateTransfer, CriteriaOldestDispatch},
		DefaultTransferGraceYear: false,
		DefaultTieBreaker:        "NAME_ASC",
		DefaultVolunteerOverflow: "PRIORITY_ORDER",
		DefaultExcludeExempt:     true,
	}
}

// PersonalExamSchedule은 교사 개인별 교내 시험감독 시간표 조회 결과입니다.
type PersonalExamSchedule struct {
	TeacherID   string     `json:"teacherId"`
	TeacherName string     `json:"teacherName"`
	ExamTitle   string     `json:"examTitle"`
	IsSealed    bool       `json:"isSealed"`
	Slots       []ExamSlot `json:"slots"`
}
