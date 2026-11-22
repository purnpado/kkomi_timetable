package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/event_calendar"
	"kkomi-timetable/server/internal/modules/external_exam"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/stats"
	"kkomi-timetable/server/internal/modules/timetable"
	"kkomi-timetable/server/internal/ws"
)

// ServerApp은 모든 도메인 모듈과 실시간 허브를 하나로 묶는 서버 애플리케이션 컨테이너입니다.
type ServerApp struct {
	Store        *database.Store
	Bus          *eventbus.Bus
	Hub          *ws.Hub
	Auth         *auth.Service
	Timetable    *timetable.Service
	InternalExam *internal_exam.Service
	ExternalExam *external_exam.Service
	Calendar     *event_calendar.Service
	Stats        *stats.Service
	OnShutdown   func()
}

// NewServerApp은 모든 도메인 모듈을 초기화하고 이벤트 버스·WebSocket 토큰 검증을 연결합니다.
func NewServerApp(store *database.Store, onShutdown func()) *ServerApp {
	bus := eventbus.New()
	hub := ws.NewHub()
	authSvc := auth.NewService(store, bus)

	hub.TokenVerifier = func(token string) (string, bool) {
		t, err := authSvc.VerifySessionToken(token)
		if err != nil || t == nil {
			return "", false
		}
		return t.ID, true
	}

	// 모든 도메인 이벤트를 감사 로그에 저장하고 실시간 WebSocket 클라이언트로 브로드캐스트
	bus.SubscribeAll(func(evt protocol.DomainEvent) {
		_ = store.AppendAuditLog(evt)
		hub.BroadcastEvent(evt)
	})

	app := &ServerApp{
		Store:        store,
		Bus:          bus,
		Hub:          hub,
		Auth:         authSvc,
		Timetable:    timetable.NewService(store, bus),
		InternalExam: internal_exam.NewService(store, bus),
		ExternalExam: external_exam.NewService(store, bus),
		Calendar:     event_calendar.NewService(store, bus),
		Stats:        stats.NewService(store),
		OnShutdown:   onShutdown,
	}
	app.InternalExam.StartScheduler()
	return app
}

// Handler는 HTTP REST API, WebSocket, 정적 UI 서빙이 통합된 http.Handler를 반환합니다.
func (a *ServerApp) Handler(webFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	// 실시간 WebSocket 및 헬스체크
	mux.HandleFunc("/ws", a.Hub.ServeWS)
	mux.HandleFunc("/api/health", a.handleHealth)

	// 인증 및 교사/권한 관리
	mux.HandleFunc("/api/auth/directory", a.handleAuthDirectory)
	mux.HandleFunc("/api/auth/login", a.handleLogin)
	mux.HandleFunc("/api/auth/logout", a.handleLogout)
	mux.HandleFunc("/api/auth/change-password", a.handleChangePassword)
	mux.HandleFunc("/api/auth/reset-password", a.handleResetPassword)
	mux.HandleFunc("/api/teachers", a.handleTeachers)
	mux.HandleFunc("/api/teachers/import-xlsx", a.handleImportTeachersXLSX)
	mux.HandleFunc("/api/teachers/roles", a.handleUpdateRoles)
	mux.HandleFunc("/api/teachers/handover", a.handleRoleHandover)

	// 대시보드 및 검색(Ctrl+K)
	mux.HandleFunc("/api/dashboard", a.handleDashboard)
	mux.HandleFunc("/api/events/recent", a.handleRecentEvents)

	// 정규·일일 시간표, 시수표·교과교실 설정, 기초 시간표 작성 및 결보강 승인
	mux.HandleFunc("/api/timetable/config", a.handleSchoolConfig)
	mux.HandleFunc("/api/timetable/loads", a.handleTeachingLoads)
	mux.HandleFunc("/api/timetable/import-loads-xlsx", a.handleImportTeachingLoadsXLSX)
	mux.HandleFunc("/api/timetable/cells", a.handleTimetableCells)
	mux.HandleFunc("/api/timetable/cross-week-candidates", a.handleCrossWeekCandidates)
	mux.HandleFunc("/api/timetable/absence-plan", a.handleAbsencePlan)
	mux.HandleFunc("/api/timetable/absence-apply", a.handleAbsenceApply)
	mux.HandleFunc("/api/timetable/auto-generate", a.handleAutoGenerateBaseTimetable)
	mux.HandleFunc("/api/timetable/import-xlsx", a.handleImportTimetableXLSX)
	mux.HandleFunc("/api/timetable/evaluate", a.handleEvaluateMove)
	mux.HandleFunc("/api/timetable/explore-swaps", a.handleExploreSwaps)
	mux.HandleFunc("/api/timetable/requests", a.handleTimetableRequests)
	mux.HandleFunc("/api/timetable/requests/agree", a.handleAgreeTimetableRequest)
	mux.HandleFunc("/api/timetable/commit", a.handleCommitTimetableRequest)
	mux.HandleFunc("/api/timetable/neis", a.handleNEISHelper)
	mux.HandleFunc("/api/timetable/creative-activities", a.handleCreativeActivities)
	mux.HandleFunc("/api/timetable/workflow", a.handleTimetableWorkflow)
	mux.HandleFunc("/api/timetable/workflow/phase", a.handleTimetableWorkflowPhase)
	mux.HandleFunc("/api/timetable/dept-submissions", a.handleDeptSubmissions)
	mux.HandleFunc("/api/timetable/dept-submissions/review", a.handleReviewDeptSubmission)
	mux.HandleFunc("/api/timetable/feedbacks", a.handleDraftFeedbacks)
	mux.HandleFunc("/api/timetable/feedbacks/resolve", a.handleResolveDraftFeedback)
	mux.HandleFunc("/api/timetable/ledger", a.handleSemesterLedger)
	mux.HandleFunc("/api/timetable/ledger/config", a.handleSemesterLedgerConfig)
	mux.HandleFunc("/api/timetable/ledger/debt", a.handleSemesterLedgerDebt)
	mux.HandleFunc("/api/timetable/constraints", a.handleTimetableConstraints)
	mux.HandleFunc("/api/timetable/constraints/batch", a.handleTimetableConstraintsBatch)
	mux.HandleFunc("/api/timetable/scenarios", a.handleTimetableScenarios)
	mux.HandleFunc("/api/timetable/scenarios/apply", a.handleApplyTimetableScenario)
	mux.HandleFunc("/api/timetable/history", a.handleTimetableHistory)
	mux.HandleFunc("/api/timetable/undo", a.handleTimetableUndo)
	mux.HandleFunc("/api/timetable/redo", a.handleTimetableRedo)

	// 교내 지필고사 시험감독 (봉인/당일 발표/긴급 대타)
	mux.HandleFunc("/api/internal-exam/slots", a.handleInternalExamSlots)
	mux.HandleFunc("/api/internal-exam/generate", a.handleGenerateInternalExam)
	mux.HandleFunc("/api/internal-exam/unseal", a.handleUnsealInternalExam)
	mux.HandleFunc("/api/internal-exam/substitute/recommend", a.handleRecommendSubstitute)
	mux.HandleFunc("/api/internal-exam/substitute/apply", a.handleApplySubstitute)
	mux.HandleFunc("/api/internal-exam/teacher-policy", a.handleInternalExamTeacherPolicy)

	// 외부 시험감독 (수능·모평·검정고시 다단계 차출)
	mux.HandleFunc("/api/external-exam/notices", a.handleExternalExamNotices)
	mux.HandleFunc("/api/external-exam/volunteer", a.handleExternalExamVolunteer)
	mux.HandleFunc("/api/external-exam/rules", a.handleExternalExamRules)
	mux.HandleFunc("/api/external-exam/candidates", a.handleExternalExamCandidates)
	mux.HandleFunc("/api/external-exam/confirm", a.handleExternalExamConfirm)

	// 주중·월중 행사 취합 및 시종시간 연동
	mux.HandleFunc("/api/calendar/events", a.handleCalendarEvents)
	mux.HandleFunc("/api/calendar/commit", a.handleCommitCalendarEvent)
	mux.HandleFunc("/api/calendar/bells", a.handleBellSchedule)

	// 문서 출력(.hwpx / .xlsx / 고대비 인쇄 HTML), 월별/학기별 통합 정산 및 원격 서버 관리
	mux.HandleFunc("/api/stats/summary", a.handleStatsSummary)
	mux.HandleFunc("/api/export/hwpx", a.handleExportHWPX)
	mux.HandleFunc("/api/export/xlsx", a.handleExportXLSX)
	mux.HandleFunc("/api/export/print", a.handleExportPrint)
	mux.HandleFunc("/api/system/backups", a.handleSystemBackups)
	mux.HandleFunc("/api/system/backups/restore", a.handleRestoreBackup)
	mux.HandleFunc("/api/system/reset", a.handleSystemReset)
	mux.HandleFunc("/api/system/client-patch", a.handleClientPatch)
	mux.HandleFunc("/api/system/client-patch/meta", a.handleClientPatchMeta)
	mux.HandleFunc("/api/system/server-update", a.handleServerSelfUpdate)
	mux.HandleFunc("/api/system/github-release", a.handleGitHubRelease)
	mux.HandleFunc("/api/system/github-update", a.handleGitHubUpdate)

	if webFS != nil {
		fileServer := http.FileServer(http.FS(webFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "존재하지 않는 API 경로입니다")
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Teacher-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// currentActor는 반드시 서명된 세션 토큰을 검증하여 인증된 교사를 반환합니다.
func (a *ServerApp) currentActor(r *http.Request) *protocol.Teacher {
	tok := extractBearerToken(r)
	if tok == "" {
		return nil
	}
	t, err := a.Auth.VerifySessionToken(tok)
	if err != nil {
		return nil
	}
	return t
}

func (a *ServerApp) requireAuth(w http.ResponseWriter, r *http.Request) (*protocol.Teacher, bool) {
	actor := a.currentActor(r)
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "로그인이 필요합니다 (유효한 인증 토큰 없음)")
		return nil, false
	}
	return actor, true
}

func (a *ServerApp) requireRole(w http.ResponseWriter, r *http.Request, roles ...protocol.Role) (*protocol.Teacher, bool) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return nil, false
	}
	if !auth.HasRole(actor, roles...) {
		writeError(w, http.StatusForbidden, "해당 업무를 실행할 담당자 권한이 없습니다")
		return nil, false
	}
	return actor, true
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
