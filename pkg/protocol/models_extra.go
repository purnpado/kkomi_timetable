package protocol

// CrossWeekCandidate는 이번 주와 다음 주·다다음 주 간 맞교환 가능한 수업 후보입니다.
type CrossWeekCandidate struct {
	TargetDate string          `json:"targetDate"` // 교환 대상 주차의 실제 수업 일자 (YYYY-MM-DD)
	TargetCell TimetableCell   `json:"targetCell"` // 상대 교사/과목 셀
	ChainSteps []ChainSwapStep `json:"chainSteps"` // 1단계(이번 주) ↔ 2단계(대상 주) 이동 요약
	Reason     string          `json:"reason"`
}

// AbsencePlanRequest는 주중 미출근/결근 교사의 기간별 대책 수립 요청입니다.
type AbsencePlanRequest struct {
	TeacherID   string `json:"teacherId"`
	StartDate   string `json:"startDate"`   // YYYY-MM-DD
	EndDate     string `json:"endDate"`     // YYYY-MM-DD
	StartPeriod int    `json:"startPeriod"` // 1~7 (0이면 1교시부터)
	EndPeriod   int    `json:"endPeriod"`   // 1~7 (0이면 7교시까지)
	Reason      string `json:"reason"`      // 예: "갑작스러운 병가", "긴급 경조사"
}

// AbsenceSubCandidate는 미출근 교시와 동시간대에 공강인 대타 보강 교사 후보입니다.
type AbsenceSubCandidate struct {
	TeacherID         string `json:"teacherId"`
	TeacherName       string `json:"teacherName"`
	Subject           string `json:"subject"`
	HomeroomClass     string `json:"homeroomClass"`
	SubstitutionCount int    `json:"substitutionCount"`
	PriorityBadge     string `json:"priorityBadge"` // 예: "[1순위: 동일교과·공강]"
}

// AbsenceSlotPlan은 미출근으로 비는 단일 수업과 추천 대책(주차간 교환 + 당일 공강교사 보강) 목록입니다.
type AbsenceSlotPlan struct {
	LessonDate        string                `json:"lessonDate"`
	Cell              TimetableCell         `json:"cell"`
	CrossWeekOptions  []CrossWeekCandidate  `json:"crossWeekOptions"`
	SubstituteOptions []AbsenceSubCandidate `json:"substituteOptions"`
}

// AbsenceApplyItem은 교시별로 선택된 대책 1건(다음주 맞교환 또는 당일 공강교사 보강)입니다.
type AbsenceApplyItem struct {
	Mode        string        `json:"mode"` // "CROSS_WEEK_SWAP" | "SUBSTITUTE"
	SourceDate  string        `json:"sourceDate"`
	TargetDate  string        `json:"targetDate"`
	SourceCell  TimetableCell `json:"sourceCell"`
	TargetCell  TimetableCell `json:"targetCell"`
	PartnerID   string        `json:"partnerId"`
	PartnerName string        `json:"partnerName"`
	Reason      string        `json:"reason"`
}

// AbsenceApplyRequest는 주중 미출근 대책 일괄 등록 또는 즉시 확정 요청입니다.
type AbsenceApplyRequest struct {
	RequesterID   string             `json:"requesterId"`
	RequesterName string             `json:"requesterName"`
	Items         []AbsenceApplyItem `json:"items"`
	AutoApprove   bool               `json:"autoApprove"` // 담당자/마스터 권한일 경우 즉시 확정 여부
}

// ExamFairnessStat은 교사 1명의 1년간 시험감독 역할별 누적·이번 시험·연간 합계 시수 현황입니다.
type ExamFairnessStat struct {
	TeacherID         string `json:"teacherId"`
	TeacherName       string `json:"teacherName"`
	Subject           string `json:"subject"`
	Position          string `json:"position"`
	HomeroomClass     string `json:"homeroomClass"`
	IsExempt          bool   `json:"isExempt"`
	ExemptReason      string `json:"exemptReason"`
	BaseMain          int    `json:"baseMain"`
	BaseSub           int    `json:"baseSub"`
	BaseCorridor      int    `json:"baseCorridor"`
	BaseTotal         int    `json:"baseTotal"`
	CurrentMain       int    `json:"currentMain"`
	CurrentSub        int    `json:"currentSub"`
	CurrentCorridor   int    `json:"currentCorridor"`
	CurrentTotal      int    `json:"currentTotal"`
	ProjectedMain     int    `json:"projectedMain"`
	ProjectedSub      int    `json:"projectedSub"`
	ProjectedCorridor int    `json:"projectedCorridor"`
	ProjectedTotal    int    `json:"projectedTotal"`
}

// ExamFairnessSummary는 1년간 전교원 감독 횟수 균등 배정 통계 및 교사별 시수표입니다.
type ExamFairnessSummary struct {
	EligibleCount int                `json:"eligibleCount"`
	ExemptCount   int                `json:"exemptCount"`
	DeputyMode    bool               `json:"deputyMode"`
	BalanceMode   string             `json:"balanceMode"`
	MinTotal      int                `json:"minTotal"`
	MaxTotal      int                `json:"maxTotal"`
	MaxGap        int                `json:"maxGap"`
	MinMain       int                `json:"minMain"`
	MaxMain       int                `json:"maxMain"`
	MainGap       int                `json:"mainGap"`
	Stats         []ExamFairnessStat `json:"stats"`
}
