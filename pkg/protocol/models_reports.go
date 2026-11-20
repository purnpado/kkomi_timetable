package protocol

// TeacherPeriodStat은 지정된 월·학기·학년도 기간 내 교원 1명의 결보강·수업교환·내외부 시험감독·행사 참여 누적 실적입니다.
// 학교 실무 보안 및 간편 정산 원칙에 따라 계좌·수당 등 금융 정보 필드는 포함하지 않습니다.
type TeacherPeriodStat struct {
	TeacherID            string `json:"teacherId"`
	TeacherName          string `json:"teacherName"`
	Subject              string `json:"subject"`
	Position             string `json:"position"`
	HomeroomClass        string `json:"homeroomClass"`
	SwapCount            int    `json:"swapCount"`            // 맞교환·연쇄교환·주차간 교환 확정 건수
	SubstituteGivenCount int    `json:"substituteGivenCount"` // 대타/보강 수행 건수
	SubstituteTakenCount int    `json:"substituteTakenCount"` // 결근/출장으로 보강을 받은 건수
	InternalMainCount    int    `json:"internalMainCount"`    // 기간 내 교내 정감독
	InternalSubCount     int    `json:"internalSubCount"`     // 기간 내 교내 부감독
	InternalCorrCount    int    `json:"internalCorrCount"`    // 기간 내 교내 복도감독
	InternalTotal        int    `json:"internalTotal"`        // 기간 내 교내 감독 합계
	ExternalExamCount    int    `json:"externalExamCount"`    // 기간 내 외부 시험감독 파견
	EventLeadCount       int    `json:"eventLeadCount"`       // 기간 내 확정 행사 인솔 횟수
	TotalActivity        int    `json:"totalActivity"`        // 전체 업무 실적 합계
}

// MonthlyTrendRow는 월(YYYY-MM) 단위 교무 업무 실적 요약 행입니다.
type MonthlyTrendRow struct {
	Month              string `json:"month"` // YYYY-MM
	TimetableChanges   int    `json:"timetableChanges"`
	SubstituteSessions int    `json:"substituteSessions"`
	InternalExamSlots  int    `json:"internalExamSlots"`
	ExternalDispatches int    `json:"externalDispatches"`
	ApprovedEvents     int    `json:"approvedEvents"`
}

// PeriodStatsReport는 월별·학기별·학년도별 통합 통계 조회 및 공문(`.hwpx`/`.xlsx`) 생성을 위한 통합 보고서 모델입니다.
type PeriodStatsReport struct {
	PeriodMode       string                   `json:"periodMode"` // MONTH | SEMESTER_1 | SEMESTER_2 | YEAR | CUSTOM | ALL
	PeriodLabel      string                   `json:"periodLabel"`
	StartDate        string                   `json:"startDate"`
	EndDate          string                   `json:"endDate"`
	SchoolName       string                   `json:"schoolName"`
	GeneratedAt      string                   `json:"generatedAt"`
	TotalChanges     int                      `json:"totalChanges"`
	TotalSubstitutes int                      `json:"totalSubstitutes"`
	TotalInternal    int                      `json:"totalInternal"`
	TotalExternal    int                      `json:"totalExternal"`
	TotalEvents      int                      `json:"totalEvents"`
	TeacherStats     []TeacherPeriodStat      `json:"teacherStats"`
	MonthlyTrend     []MonthlyTrendRow        `json:"monthlyTrend"`
	ApprovedChanges  []TimetableChangeRequest `json:"approvedChanges"`
	ApprovedEvents   []SchoolEvent            `json:"approvedEvents"`
}
