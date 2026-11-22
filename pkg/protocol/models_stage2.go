package protocol

import "strings"

// AllCellTeachers는 수업 셀에 참여하는 주 교사 및 모든 협력(코티칭) 교사 ID를 중복 없이 반환합니다.
func AllCellTeachers(c TimetableCell) []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(c.TeacherID)
	add(c.CoTeacherID)
	for _, id := range c.CoTeacherIDs {
		add(id)
	}
	return out
}

// AllLoadTeachers는 시수 배당 행에 포함된 주 교사 및 모든 협력(코티칭) 교사 ID를 중복 없이 반환합니다.
func AllLoadTeachers(l TeachingLoad) []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(l.TeacherID)
	add(l.CoTeacherID)
	for _, id := range l.CoTeacherIDs {
		add(id)
	}
	return out
}

// EffectiveMaxConcurrent는 특별실의 동시 수용 가능 학급수(기본 1)를 반환합니다.
func EffectiveMaxConcurrent(r SpecialRoom) int {
	if r.MaxConcurrent <= 0 {
		return 1
	}
	return r.MaxConcurrent
}

// IsSubjectClassroomEnabled는 학교 설정상 교과교실제 활성화 여부를 반환합니다.
func IsSubjectClassroomEnabled(cfg SchoolConfig) bool {
	if cfg.SubjectClassroomMode == nil {
		return true
	}
	return *cfg.SubjectClassroomMode
}

// RoomMaxConcurrent는 강의실 이름(또는 ID)에 해당하는 동시 수용 학급수 상한을 반환합니다.
func RoomMaxConcurrent(cfg SchoolConfig, roomName string) int {
	trimmed := strings.TrimSpace(roomName)
	if trimmed == "" {
		return 1
	}
	for _, r := range cfg.SpecialRooms {
		if strings.EqualFold(strings.TrimSpace(r.Name), trimmed) || strings.EqualFold(strings.TrimSpace(r.ID), trimmed) {
			return EffectiveMaxConcurrent(r)
		}
	}
	return 1
}

// CreativeActivitySlot은 동시 또는 학년·전교 단위로 고정 편성하는 창의적 체험활동 슬롯입니다.
type CreativeActivitySlot struct {
	ID         string `json:"id"`
	Title      string `json:"title"`      // 예: "창체(자치·적응)", "창체(동아리)"
	DayOfWeek  int    `json:"dayOfWeek"`  // 1(월)~5(금)
	Period     int    `json:"period"`     // 1~7교시
	Grade      int    `json:"grade"`      // 0이면 전학년, 1~3이면 해당 학년
	Locked     bool   `json:"locked"`     // 이동 금지 고정 여부
	Simultaneous bool `json:"simultaneous"` // 학년 전체 동시 편성 여부
}

// SwapOption은 수업 교체 전수 탐색 결과의 단일 후보 경로를 나타냅니다.
type SwapOption struct {
	OptionID       string          `json:"optionId"`
	OptionType     string          `json:"optionType"`     // "MOVE", "SWAP", "TRIANGLE", "QUAD", "CROSS_WEEK", "BLOCK_MOVE"
	Scope          string          `json:"scope"`          // "DATE_ONLY" 또는 ""
	Title          string          `json:"title"`          // 요약 제목
	Description    string          `json:"description"`    // 상세 경로 설명
	Score          int             `json:"score"`          // 종합 품질 점수 (높을수록 우수)
	Complexity     int             `json:"complexity"`     // 연쇄 단계/이동 셀 수
	WeekDelta      int             `json:"weekDelta"`      // 다주간 교환 주차 차이 (0=동일주)
	SourceDate     string          `json:"sourceDate,omitempty"`
	TargetDate     string          `json:"targetDate,omitempty"`
	PartnerID      string          `json:"partnerId,omitempty"`
	PartnerName    string          `json:"partnerName,omitempty"`
	SoftWarnings   []string        `json:"softWarnings,omitempty"`
	Steps          []ChainSwapStep `json:"steps"`
	BlockCellCount int             `json:"blockCellCount,omitempty"` // 선택블록/연강 동시 이동 셀 수
}

// ExploreSwapsRequest는 특정 수업에 대한 가능한 모든 교체/이동 경우의 수 탐색 요청입니다.
type ExploreSwapsRequest struct {
	SourceCellID   string `json:"sourceCellId"`
	Scope          string `json:"scope,omitempty"`          // "DATE_ONLY"(기본) 또는 ""(정규)
	WeekStart      string `json:"weekStart,omitempty"`      // 주차 기준 월요일 (YYYY-MM-DD)
	IncludeCrossWk bool   `json:"includeCrossWk,omitempty"` // 다주간(+1~4주) 포함 여부
	MaxCrossWeeks  int    `json:"maxCrossWeeks,omitempty"`  // 탐색할 최대 이후 주수 (기본 4)
	IncludeBlocks  bool   `json:"includeBlocks,omitempty"`  // 선택블록·연강 통째 이동 포함 여부
	MaxResults     int    `json:"maxResults,omitempty"`     // 최대 반환 개수 (기본 80)
}

// ExploreSwapsResponse는 수업 교체 전수 탐색 결과와 유형별 요약을 담습니다.
type ExploreSwapsResponse struct {
	SourceCell TimetableCell  `json:"sourceCell"`
	Scope      string         `json:"scope"`
	TotalCount int            `json:"totalCount"`
	Counts     map[string]int `json:"counts"` // 유형별 건수 ("MOVE", "SWAP", ...)
	Options    []SwapOption   `json:"options"`
}

// UnplacedDiagnosis는 자동 편성에서 배치 실패한 시수의 원인 역추적 결과입니다.
type UnplacedDiagnosis struct {
	UnitID            string   `json:"unitId"`
	ClassName         string   `json:"className"`
	Subject           string   `json:"subject"`
	TeacherID         string   `json:"teacherId"`
	TeacherName       string   `json:"teacherName"`
	BlockHours        int      `json:"blockHours"`
	SimultaneousGroup string   `json:"simultaneousGroup,omitempty"`
	DedicatedRoom     string   `json:"dedicatedRoom,omitempty"`
	PrimaryCause      string   `json:"primaryCause"`
	DetailReasons     []string `json:"detailReasons"`
	Suggestions       []string `json:"suggestions"`
}

const (
	WorkflowCollecting  = "COLLECTING"
	WorkflowDraftReview = "DRAFT_REVIEW"
	WorkflowLocked      = "LOCKED"
)

// TimetableWorkflowState는 시간표 편성 3단계 상태(시수 취합 → 가편성 검토 → 최종 확정)를 나타냅니다.
type TimetableWorkflowState struct {
	ID         string `json:"id"`
	Phase      string `json:"phase"`      // COLLECTING, DRAFT_REVIEW, LOCKED
	UpdatedBy  string `json:"updatedBy"`
	UpdatedAt  string `json:"updatedAt"`
	Note       string `json:"note"`
	LockedAt   string `json:"lockedAt,omitempty"`
	LockedBy   string `json:"lockedBy,omitempty"`
}

// DeptLoadSubmission은 교과부장별 배당 시수표 제출 및 취합 상태입니다.
type DeptLoadSubmission struct {
	ID          string `json:"id"`
	Department  string `json:"department"`
	SubmitterID string `json:"submitterId"`
	Submitter   string `json:"submitter"`
	GradeScope  string `json:"gradeScope"`
	LoadCount   int    `json:"loadCount"`
	TotalHours  int    `json:"totalHours"`
	Status      string `json:"status"` // DRAFT, SUBMITTED, APPROVED, REVISION_REQUESTED
	Note        string `json:"note"`
	ReviewNote  string `json:"reviewNote,omitempty"`
	SubmittedAt string `json:"submittedAt"`
	ReviewedBy  string `json:"reviewedBy,omitempty"`
	ReviewedAt  string `json:"reviewedAt,omitempty"`
}

// DraftFeedback은 가편성 검토 단계에서 교사가 남기는 시간표 수정 의견입니다.
type DraftFeedback struct {
	ID         string `json:"id"`
	AuthorID   string `json:"authorId"`
	AuthorName string `json:"authorName"`
	TargetRef  string `json:"targetRef"` // 예: "2-3 월 4교시" 또는 "수학교과"
	Category   string `json:"category"`  // "연강", "기피교시", "특별실", "기타"
	Message    string `json:"message"`
	Status     string `json:"status"` // OPEN, RESOLVED
	Resolution string `json:"resolution,omitempty"`
	CreatedAt  string `json:"createdAt"`
	ResolvedBy string `json:"resolvedBy,omitempty"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

// SemesterLedgerConfig는 학기당 필수 시수 원장 계산 기준 설정입니다.
type SemesterLedgerConfig struct {
	ID             string `json:"id"`
	SemesterName   string `json:"semesterName"`   // 예: "2026학년도 1학기"
	TotalWeeks     int    `json:"totalWeeks"`     // 기준 수업 주수 (기본 17주)
	MinRatioPct    int    `json:"minRatioPct"`    // 최소 확보율 경고 기준 (기본 95%)
	IncludeCurrent bool   `json:"includeCurrent"` // 현재 일일/주차 변동 포함 여부
}

// SemesterLedgerRow는 학급·과목별 학기 목표 시수 대비 실제 확보 시수 행입니다.
type SemesterLedgerRow struct {
	ClassName    string `json:"className"`
	Grade        int    `json:"grade"`
	Subject      string `json:"subject"`
	TeacherID    string `json:"teacherId"`
	TeacherName  string `json:"teacherName"`
	WeeklyHours  int    `json:"weeklyHours"`
	TargetHours  int    `json:"targetHours"`  // 주당시수 * TotalWeeks
	BlockedHours int    `json:"blockedHours"` // 시험·행사 등으로 결손된 시수
	AddedHours   int    `json:"addedHours"`   // 보강·교환으로 보충된 시수
	ActualHours  int    `json:"actualHours"`  // 실제 확보 시수
	DeficitHours int    `json:"deficitHours"` // 부족 시수 (양수면 미보충 필요)
	AchievedPct  int    `json:"achievedPct"`
	Status       string `json:"status"` // "충족", "주의", "미달"
}

// MakeupDebtItem은 행사·결강으로 발생하여 보강이 필요한 미보충 수업 부채 항목입니다.
type MakeupDebtItem struct {
	ID          string `json:"id"`
	CellID      string `json:"cellId"`
	Date        string `json:"date"`
	DayOfWeek   int    `json:"dayOfWeek"`
	Period      int    `json:"period"`
	ClassName   string `json:"className"`
	Subject     string `json:"subject"`
	TeacherID   string `json:"teacherId"`
	TeacherName string `json:"teacherName"`
	Reason      string `json:"reason"`
	Status      string `json:"status"` // "UNRESOLVED", "RESOLVED"
}

// TeacherMakeupBalance는 교사별 결강(차감)·대강/보강(가산) 순보강 부채 잔고입니다.
type TeacherMakeupBalance struct {
	TeacherID     string `json:"teacherId"`
	TeacherName   string `json:"teacherName"`
	MissedHours   int    `json:"missedHours"`
	CoveredHours  int    `json:"coveredHours"`
	NetBalance    int    `json:"netBalance"` // Covered - Missed (음수면 보충 필요)
	PendingDebts  int    `json:"pendingDebts"`
}

// SemesterLedgerReport는 학기 시수 원장 및 미보충 부채 통합 보고서입니다.
type SemesterLedgerReport struct {
	Config          SemesterLedgerConfig   `json:"config"`
	Summary         map[string]int         `json:"summary"`
	Rows            []SemesterLedgerRow    `json:"rows"`
	Debts           []MakeupDebtItem       `json:"debts"`
	TeacherBalances []TeacherMakeupBalance `json:"teacherBalances"`
}
