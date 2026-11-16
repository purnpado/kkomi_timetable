package protocol

// Role은 업무분장(RBAC) 권한 상수를 정의합니다. 1인의 교사가 여러 권한을 겸임할 수 있습니다.
type Role string

const (
	RoleMaster            Role = "MASTER"              // 시스템 총괄 (마스터)
	RoleTimetableAdmin    Role = "TIMETABLE_ADMIN"     // 시간표 담당교사
	RoleInternalExamAdmin Role = "INTERNAL_EXAM_ADMIN" // 교내 시험감독 담당교사
	RoleExternalExamAdmin Role = "EXTERNAL_EXAM_ADMIN" // 외부 시험감독 담당교사
	RoleEventAdmin        Role = "EVENT_ADMIN"         // 행사 담당교사
	RoleTeacher           Role = "TEACHER"             // 일반 교사
)

// ApprovalStatus는 교사 신청이 대기열(STAGING)인지 담당교사 최종 선택(COMMITTED)인지 나타냅니다.
type ApprovalStatus string

const (
	StatusStaging  ApprovalStatus = "STAGING"
	StatusApproved ApprovalStatus = "APPROVED"
	StatusRejected ApprovalStatus = "REJECTED"
)

// Teacher는 교사 기초 인사 및 업무분장 모델입니다.
// 주의: 수당 지급, 은행명, 계좌번호 등 금융정보 필드는 학교 실무 요구에 따라 일체 취급하지 않습니다.
type Teacher struct {
	ID                   string   `json:"id"`
	Username             string   `json:"username"`
	PasswordHash         string   `json:"passwordHash,omitempty"`
	MustChangePassword   bool     `json:"mustChangePassword"`
	Name                 string   `json:"name"`
	Position             string   `json:"position"`               // 직위 (예: 교무부장, 연구부장, 평교사)
	IsDepartmentHead     bool     `json:"isDepartmentHead"`       // 부장교사 여부
	Subject              string   `json:"subject"`                // 주 담당 과목 (예: 수학, 영어, 국어)
	HomeroomClass        string   `json:"homeroomClass"`          // 담임 학급 (예: "2-3", 비담임이면 "")
	Phone                string   `json:"phone"`                  // 교내 내선 또는 연락처 (외부감독 파견 명단용)
	Roles                []Role   `json:"roles"`                  // 다중 겸임 가능한 RBAC 권한 목록
	TransferDate         string   `json:"transferDate"`           // 현임교 전입일 (YYYY-MM-DD)
	LastExternalDispatch string   `json:"lastExternalDispatch"`   // 마지막 외부감독 파견일 (YYYY-MM-DD, 미파견 시 "")
	ExternalExamCount    int      `json:"externalExamCount"`      // 최근 1~3년 외부감독 누적 파견 횟수
	InternalExamCount    int      `json:"internalExamCount"`      // 올해 교내 시험감독 누적 횟수
	SubstitutionCount    int      `json:"substitutionCount"`      // 올해 결보강/대타 횟수
	ExemptReasons        []string `json:"exemptReasons"`          // 외부감독 면제 사유 (예: 임산부, 수험생 자녀, 원로교사)
	ItinerantDays        []int    `json:"itinerantDays"`          // 순회교사 출강 가능 요일 (1=월 ~ 5=금, 빈 슬라이스면 전일 근무)
	ParentOfStudentClass string   `json:"parentOfStudentClass"`   // 학부모 명예감독일 경우 자녀 학년-반 (예: "1-2")
	IsParentHonorary     bool     `json:"isParentHonorary"`       // 학부모 명예감독 여부
}

// TimetableCell은 주간 정규/일일 시간표의 단일 수업 셀을 나타냅니다.
type TimetableCell struct {
	ID            string `json:"id"`
	DayOfWeek     int    `json:"dayOfWeek"`     // 1(월)~5(금)
	Period        int    `json:"period"`        // 1~7교시
	ClassName     string `json:"className"`     // 학급 또는 선택반명 (예: "2-3", "고교학점제-심화수학A")
	Grade         int    `json:"grade"`         // 1, 2, 3
	Subject       string `json:"subject"`       // 과목명
	SubjectCode   string `json:"subjectCode"`   // 나이스(NEIS) 과목 코드
	TeacherID     string `json:"teacherId"`     // 주 담당 교사 ID
	CoTeacherID   string `json:"coTeacherId"`   // 코티칭 협력 교사 ID (선택)
	Room          string `json:"room"`          // 교실 또는 이동수업 전용실
	ElectiveGroup string `json:"electiveGroup"` // 고교학점제 이동수업 동시간대 블록 그룹 ID
	BlockGroupID  string `json:"blockGroupID"`  // 연속 블록타임 묶음 ID
	BlockedBy     string `json:"blockedBy"`     // 행사 등으로 수업 블록 처리된 사유
}

// EvaluateMoveRequest는 시간표 Canvas 드래그 앤 드롭 시 신호등 판정을 요청하는 구조체입니다.
type EvaluateMoveRequest struct {
	SourceCellID  string `json:"sourceCellId"`
	TargetDay     int    `json:"targetDay"`
	TargetPeriod  int    `json:"targetPeriod"`
	TargetClass   string `json:"targetClass"`
	TargetTeacher string `json:"targetTeacher"`
}

// MoveEvaluationResult는 신호등 판정 결과(GREEN/YELLOW/RED)를 담습니다.
type MoveEvaluationResult struct {
	Signal       string   `json:"signal"`       // "GREEN", "YELLOW", "RED"
	Mode         string   `json:"mode"`         // "MOVE", "SWAP", "TRIANGLE", "BLOCKED"
	Reason       string   `json:"reason"`       // 한글 안내 또는 충돌 불가 사유
	TrianglePath []string `json:"trianglePath"` // 삼각 교환 가능 시 A -> B -> C 셀 ID 경로
}

// TimetableChangeRequest는 교사 간 수업 교환 및 결보강 대기열(STAGING) 요청입니다.
type TimetableChangeRequest struct {
	ID              string         `json:"id"`
	RequestType     string         `json:"requestType"`     // "SWAP"(맞교환), "SUBSTITUTE"(결보강), "EXTERNAL_TRIP"(외부감독출장), "EVENT_TRIP"(행사출장)
	TargetDate      string         `json:"targetDate"`      // 변경 적용 일자 (YYYY-MM-DD)
	RequesterID     string         `json:"requesterId"`
	RequesterName   string         `json:"requesterName"`
	PartnerID       string         `json:"partnerId"`       // 맞교환/보강 교사 ID
	PartnerName     string         `json:"partnerName"`
	PartnerAgreed   bool           `json:"partnerAgreed"`   // 상대 교사 동의 여부
	SourceCell      TimetableCell  `json:"sourceCell"`      // 변경 전 셀
	TargetCell      TimetableCell  `json:"targetCell"`      // 변경 후/대상 셀
	Reason          string         `json:"reason"`          // 신청 사유 또는 비고
	Status          ApprovalStatus `json:"status"`          // STAGING, APPROVED, REJECTED
	RejectReason    string         `json:"rejectReason"`    // 반려 사유 칩
	ReviewedBy      string         `json:"reviewedBy"`
	CreatedAt       string         `json:"createdAt"`
	CommittedAt     string         `json:"committedAt"`
}

// SubjectHandlingMode는 담당과목 시험 교시 배정 옵션을 정의합니다.
type SubjectHandlingMode string

const (
	SubjectModeCorridorFirst SubjectHandlingMode = "CORRIDOR_FIRST" // 기본값: 해당 학년 층 복도감독 우선 배치
	SubjectModeHQStandby     SubjectHandlingMode = "HQ_STANDBY"     // 평가본부 대기 / 공강 처리
)

// ExamConfig는 교내 지필고사 감독 배정 옵션 설정입니다.
type ExamConfig struct {
	ID                   string              `json:"id"`
	Title                string              `json:"title"`                // 예: "2026학년도 2학기 기말고사"
	ExamDate             string              `json:"examDate"`             // 시험 일자 (YYYY-MM-DD)
	UnsealTime           string              `json:"unsealTime"`           // 당일 고사장 자동 공개 시각 (기본 "08:20")
	IsSealed             bool                `json:"isSealed"`             // 사전 봉인 여부 (true면 고사장 비공개)
	InstantRevealMode    bool                `json:"instantRevealMode"`    // 학교 설정: 즉시 공개 모드 여부
	DeputyMode           bool                `json:"deputyMode"`           // 정·부감독 2인 선택 운영 (false면 1인 단독)
	HonoraryParentOn     bool                `json:"honoraryParentOn"`     // 학부모 명예감독 선택적(On/Off) 투입
	SubjectTeacherPolicy SubjectHandlingMode `json:"subjectTeacherPolicy"` // 담당과목 시험 교시 처리 (CORRIDOR_FIRST 기본)
}

// ExamSlot은 지필고사 교시별 고사장/복도감독 배정 항목입니다.
type ExamSlot struct {
	ID               string `json:"id"`
	ExamID           string `json:"examId"`
	ExamDate         string `json:"examDate"`
	Period           int    `json:"period"`
	StartTime        string `json:"startTime"`       // 예: "08:50" (과목별 시작 시각)
	EndTime          string `json:"endTime"`         // 예: "09:35" (과목별 종료 시각)
	DurationMinutes  int    `json:"durationMinutes"` // 예: 45 (과목별 시험 시간 분)
	Grade            int    `json:"grade"`
	Subject          string `json:"subject"`
	SlotRole         string `json:"slotRole"`         // "MAIN"(정감독), "SUB"(부감독), "CORRIDOR"(복도감독), "HONORARY"(학부모명예)
	RoomName         string `json:"roomName"`         // 봉인 중일 때 일반 교사에게는 "🔒 당일 08:20 공개"로 마스킹됨
	ActualRoomHidden bool   `json:"actualRoomHidden"` // 마스킹 표시 여부
	TeacherID        string `json:"teacherId"`
	TeacherName      string `json:"teacherName"`
	Note             string `json:"note"`
}

// ExternalExamCriteria는 학교별 외부 시험감독 차출 정렬 조건 키워드입니다.
type ExternalExamCriteria string

const (
	CriteriaVolunteer      ExternalExamCriteria = "VOLUNTEER"       // 해당 시험 자발적 지원자 우선
	CriteriaLowCount       ExternalExamCriteria = "LOW_COUNT"       // 누적 외부감독 횟수 적은 순
	CriteriaLateTransfer   ExternalExamCriteria = "LATE_TRANSFER"   // 현임교 전입일 늦은 순 (최근 전입자 우선)
	CriteriaOldestDispatch ExternalExamCriteria = "OLDEST_DISPATCH" // 이전 외부차출이 오래된 순 (미파견 최우선)
	CriteriaNonHomeroom    ExternalExamCriteria = "NON_HOMEROOM"    // 비담임 우선
	CriteriaNonDeptHead    ExternalExamCriteria = "NON_DEPT_HEAD"   // 비부장 우선
)

// ExternalExamNotice는 수능·모의평가·검정고시·임용고사 등 대외 시험 공고입니다.
type ExternalExamNotice struct {
	ID              string                 `json:"id"`
	Title           string                 `json:"title"`           // 예: "2027학년도 대학수학능력시험 감독관 파견"
	ExamCategory    string                 `json:"examCategory"`    // 수능, 모의평가, 검정고시, 교원임용, 기타
	ExamDate        string                 `json:"examDate"`        // 시험 일자 (YYYY-MM-DD)
	Location        string                 `json:"location"`        // 파견 고사장 또는 관내 학교
	RequiredCount   int                    `json:"requiredCount"`   // 필요 차출 인원
	PresetMode      string                 `json:"presetMode"`      // PRESET_MIXED, PRESET_COOLDOWN, PRESET_RECENT_TRANSFER, CUSTOM
	PriorityOrder   []ExternalExamCriteria `json:"priorityOrder"`   // 다단계 차출 우선순위 배열
	ExcludeExempt   bool                   `json:"excludeExempt"`   // 면제 대상자(임산부/수험생자녀/원로교사 등) 제외
	VolunteerIDs    []string               `json:"volunteerIds"`    // 자발적 지원 교사 ID 목록
	ConfirmedIDs    []string               `json:"confirmedIds"`    // 담당교사 최종 선택(파견 확정) 교사 ID 목록
	IsConfirmed     bool                   `json:"isConfirmed"`
	LinkedToTimetable bool                 `json:"linkedToTimetable"` // 평일 시험 시 결보강 대기열 이관 완료 여부
}

// ExternalExamCandidate는 차출 순번 산정기가 계산한 교사별 순위와 근거입니다.
type ExternalExamCandidate struct {
	Rank           int     `json:"rank"`
	Teacher        Teacher `json:"teacher"`
	IsVolunteer    bool    `json:"isVolunteer"`
	IsExempt       bool    `json:"isExempt"`
	IsSelected     bool    `json:"isSelected"`
	ReasonBadge    string  `json:"reasonBadge"`    // 예: "[1순위: 자발적 지원]", "[차출후보 1번: 2026.03.01 전입 · 미파견]"
	DaysSinceLast  int     `json:"daysSinceLast"`  // 마지막 파견일로부터 경과 일수 (미파견 시 99999)
}

// EventLinkType은 주중·월중 행사의 시간표/시종 연동 속성입니다.
type EventLinkType string

const (
	EventLinkSimpleNotice EventLinkType = "SIMPLE_NOTICE" // [단순안내]
	EventLinkClassBlock   EventLinkType = "CLASS_BLOCK"   // [학년/전교 수업블록]
	EventLinkTeacherTrip  EventLinkType = "TEACHER_TRIP"  // [특정교사 출장/결보강]
	EventLinkBellChange   EventLinkType = "BELL_CHANGE"   // [시종시간 변경]
)

// SchoolEvent는 주중·월중 행사 취합 및 확정 항목입니다.
type SchoolEvent struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Department    string         `json:"department"`    // 제안 부서 (예: 학생생활안전부, 교무기획부)
	EventDate     string         `json:"eventDate"`     // YYYY-MM-DD
	StartPeriod   int            `json:"startPeriod"`   // 시작 교시 (0이면 종일/해당없음)
	EndPeriod     int            `json:"endPeriod"`
	TargetGrades  []int          `json:"targetGrades"`  // 대상 학년 (예: [1,2] 또는 [1,2,3])
	TargetClasses []string       `json:"targetClasses"` // 특정 학급 지정 시
	LeadTeachers  []string       `json:"leadTeachers"`  // 인솔/담당 교사 ID 목록
	LinkType      EventLinkType  `json:"linkType"`      // 4종 연동 속성
	BellOverride  []BellPeriod   `json:"bellOverride"`  // [시종시간 변경] 시 적용할 교시별 시각
	Description   string         `json:"description"`
	Status        ApprovalStatus `json:"status"`        // STAGING -> APPROVED(행사 담당교사 최종 선택)
	ProposedBy    string         `json:"proposedBy"`
	CommittedBy   string         `json:"committedBy"`
}

// BellPeriod는 교시별 시종시간(수업 중 방해 금지 모드 판정 기준)입니다.
type BellPeriod struct {
	Period    int    `json:"period"`
	Label     string `json:"label"`     // 예: "1교시", "아침조회"
	StartTime string `json:"startTime"` // "08:40"
	EndTime   string `json:"endTime"`   // "09:30"
}
