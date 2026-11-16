package protocol

// EventType은 서버 이벤트 버스 및 WebSocket으로 전파되는 도메인 이벤트 유형입니다.
type EventType string

const (
	EventTimetableRequested    EventType = "TimetableRequested"
	EventTimetableApproved     EventType = "TimetableApproved"
	EventTimetableRejected     EventType = "TimetableRejected"
	EventExamSealed            EventType = "ExamSealed"
	EventExamUnsealed          EventType = "ExamUnsealed"
	EventExamSubstituted       EventType = "ExamSubstituted"
	EventExternalExamConfirmed EventType = "ExternalExamConfirmed"
	EventSchoolEventPublished  EventType = "SchoolEventPublished"
	EventBellScheduleChanged   EventType = "BellScheduleChanged"
	EventSystemUpdateAvailable EventType = "SystemUpdateAvailable"
)

// DomainEvent는 모듈 간 및 서버-클라이언트 간 실시간 전달되는 이벤트 봉투입니다.
type DomainEvent struct {
	ID         string      `json:"id"`
	Type       EventType   `json:"type"`
	Title      string      `json:"title"`
	Message    string      `json:"message"`
	Urgent     bool        `json:"urgent"`    // true면 수업 중 방해 금지 모드를 무시하고 즉시 최상단 토스트 표시
	TargetIDs  []string    `json:"targetIds"` // 비어 있으면 전 교원 브로드캐스트, 지정 시 해당 교사 ID 대상
	Payload    interface{} `json:"payload"`
	ActorID    string      `json:"actorId"`
	ActorName  string      `json:"actorName"`
	OccurredAt string      `json:"occurredAt"`
}

// DashboardSummary는 오늘의 교무 대시보드 초기 로딩 및 갱신용 통합 응답입니다.
type DashboardSummary struct {
	ServerVersion      string                   `json:"serverVersion"`
	ServerIP           string                   `json:"serverIp"`
	DatabaseMode       string                   `json:"databaseMode"` // "MariaDB (Portable TDE)" 또는 "Embedded AES-256 Store"
	TodayDate          string                   `json:"todayDate"`
	TodayWeekday       int                      `json:"todayWeekday"`
	BellSchedule       []BellPeriod             `json:"bellSchedule"`
	MyTodayLessons     []TimetableCell          `json:"myTodayLessons"`
	HomeroomLessons    []TimetableCell          `json:"homeroomLessons"`
	TodayEvents        []SchoolEvent            `json:"todayEvents"`
	WeekEvents         []SchoolEvent            `json:"weekEvents"`
	MyExamSlots        []ExamSlot               `json:"myExamSlots"`
	PendingApprovals   []TimetableChangeRequest `json:"pendingApprovals"`
	PendingEvents      []SchoolEvent            `json:"pendingEvents"`
	OpenExternalExams  []ExternalExamNotice     `json:"openExternalExams"`
	AdminPendingCounts map[string]int           `json:"adminPendingCounts"`
}
