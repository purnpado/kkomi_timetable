package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/api"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/ws"
)

func TestMain(m *testing.M) {
	code := m.Run()
	database.StopTestDaemons()
	os.Exit(code)
}

func newTestServerApp(t *testing.T) (*api.ServerApp, http.Handler) {
	t.Helper()
	t.Setenv("KKOMI_BASE_DIR", t.TempDir())
	store, err := database.Open()
	if err != nil {
		t.Fatalf("테스트 DB 열기 실패: %v", err)
	}
	if err := database.SeedIfEmpty(store); err != nil {
		t.Fatalf("테스트 시드 실패: %v", err)
	}
	app := api.NewServerApp(store, nil)
	t.Cleanup(func() {
		app.InternalExam.StopScheduler()
		_ = store.Close()
	})
	return app, app.Handler(nil)
}

func doRecorderReq(t *testing.T, h http.Handler, method, path, token string, body interface{}) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("요청 JSON 직렬화 실패: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func loginForToken(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if code != http.StatusOK {
		t.Fatalf("로그인 실패(%s): status=%d, body=%s", username, code, string(raw))
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.Token == "" {
		t.Fatalf("세션 토큰 추출 실패(%s): err=%v, body=%s", username, err, string(raw))
	}
	return res.Token
}

// TestFullAPIEndpointsE2E는 프론트엔드가 호출하는 전체 REST API와 도메인 이벤트 발행을 검증합니다.
func TestFullAPIEndpointsE2E(t *testing.T) {
	app, h := newTestServerApp(t)

	var mu sync.Mutex
	var capturedEvents []protocol.DomainEvent
	app.Bus.SubscribeAll(func(evt protocol.DomainEvent) {
		mu.Lock()
		capturedEvents = append(capturedEvents, evt)
		mu.Unlock()
	})

	// 1. 헬스체크 (현재 서버 버전 및 시연 모드 확인)
	code, raw := doRecorderReq(t, h, http.MethodGet, "/api/health", "", nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"version":"`+ws.ServerVersion+`"`) {
		t.Fatalf("헬스체크 실패 (status=%d, body=%s)", code, string(raw))
	}

	// 2. 로그인 및 잘못된 비밀번호 거부 확인
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "t06", "password": "wrong",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("잘못된 비밀번호가 차단되지 않음 (status=%d)", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "admin", "password": "1234",
	})
	if code != http.StatusOK || strings.Contains(string(raw), "passwordHash") || !strings.Contains(string(raw), `"mustChangePassword":true`) {
		t.Fatalf("admin 로그인 실패 또는 해시/초기변경 플래그 오류 (status=%d, body=%s)", code, string(raw))
	}

	tokAdmin := loginForToken(t, h, "admin", "1234")
	tokTimetable := loginForToken(t, h, "timetable", "1234")
	tokInternal := loginForToken(t, h, "internal", "1234")
	tokExternal := loginForToken(t, h, "external", "1234")
	tokEvent := loginForToken(t, h, "event", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 비밀번호 변경 및 새 비밀번호 로그인·새 토큰 발급 확인 (t06)
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/auth/change-password", tokT06, map[string]string{
		"currentPassword": "1234", "newPassword": "newpass99",
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"mustChangePassword":false`) {
		t.Fatalf("비밀번호 변경 실패 (status=%d, body=%s)", code, string(raw))
	}
	tokT06New := loginForToken(t, h, "t06", "newpass99")

	// 3. 교내 지필고사 봉인 마스킹 -> 권한 없는 교사 차단 -> 관리자 개봉 및 이벤트 발행
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", tokT06New, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), "공개") {
		t.Fatalf("일반 교사 조회 시 고사장 봉인이 풀렸습니다: %s", string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", tokT06New, nil)
	if code != http.StatusForbidden {
		t.Fatalf("권한 없는 교사의 개봉 요청이 차단되지 않음: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", tokInternal, nil)
	if code != http.StatusOK {
		t.Fatalf("교내감독 관리자 고사장 발표 실패: %d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", tokT06New, nil)
	if code != http.StatusOK || strings.Contains(string(raw), `"actualRoomHidden":true`) {
		t.Fatalf("개봉 이후에도 고사장이 숨겨져 있습니다: %s", string(raw))
	}

	// 4. 시간표 조회, 신호등 판정, 대기열 확정 및 나이스 입력 도우미
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/dashboard", tokT06New, nil)
	if code != http.StatusOK {
		t.Fatalf("대시보드 조회 실패: %d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/evaluate", tokTimetable, protocol.EvaluateMoveRequest{
		SourceCellID: "c-1-3-11",
		TargetDay:    2,
		TargetPeriod: 3,
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"signal":"RED"`) {
		t.Fatalf("순회교사 출강일 충돌 RED 판정 실패: %s", string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/commit", tokTimetable, map[string]interface{}{
		"requestId": "req-seed-1", "approve": true,
	})
	if code != http.StatusOK {
		t.Fatalf("시간표 담당교사 최종 선택 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/timetable/neis", tokTimetable, nil)
	if code != http.StatusOK {
		t.Fatalf("나이스 입력 도우미 조회 실패: %d", code)
	}

	// 5. 외부감독 지원 및 후보 산정 조회
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/volunteer", tokT06New, map[string]string{"noticeId": "ext-2027-csat"})
	if code != http.StatusOK {
		t.Fatalf("외부감독 지원 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/external-exam/candidates?noticeId=ext-2027-csat", tokExternal, nil)
	if code != http.StatusOK {
		t.Fatalf("외부감독 후보 산정 조회 실패: %d", code)
	}

	// 6. 행사 취합 확정, 백업 및 출력(.hwpx, .xlsx, 고대비 인쇄)
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/calendar/commit", tokEvent, map[string]interface{}{
		"eventId": "evt-seed-2", "approve": true,
	})
	if code != http.StatusOK {
		t.Fatalf("행사 담당교사 확정 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/system/backups", tokAdmin, nil)
	if code != http.StatusOK {
		t.Fatalf("시스템 마스터 백업 생성 실패: %d", code)
	}
	for _, ep := range []string{
		"/api/export/hwpx?kind=calendar",
		"/api/export/xlsx?kind=timetable",
		"/api/export/xlsx?kind=teachers",
		"/api/export/xlsx?kind=loads",
		"/api/export/xlsx?kind=base-template",
		"/api/export/print",
	} {
		c, body := doRecorderReq(t, h, http.MethodGet, ep, tokAdmin, nil)
		if c != http.StatusOK || len(body) == 0 {
			t.Fatalf("출력 엔드포인트 실패 (%s): status=%d", ep, c)
		}
	}

	// 6-1. 교사 공개 디렉토리(동명이인 구분) · 학교 기본설정 · 시수표 조회 및 시수기반 자동편성
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/auth/directory", "", nil)
	if code != http.StatusOK || strings.Contains(string(raw), "passwordHash") {
		t.Fatalf("공개 디렉토리 조회 실패 또는 해시 노출: status=%d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/loads", tokTimetable, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), "classStatuses") {
		t.Fatalf("시수표 조회 실패: status=%d, body=%s", code, string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/auto-generate", tokTimetable, map[string]interface{}{
		"useCurriculum": true, "clearExisting": true,
	})
	if code != http.StatusOK {
		t.Fatalf("시수표 기반 자동 편성 실패: status=%d", code)
	}

	// 6-2. 다주간 시간표 조회, 주차 간 맞교환 후보 및 주중 미출근 대책 수립/적용 API 검증
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/cells?weekStart=2026-11-16", tokT06New, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"lessonDate"`) {
		t.Fatalf("주차별 시간표 조회 실패: status=%d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/absence-plan", tokT06New, map[string]interface{}{
		"teacherId": "t06", "startDate": "2026-11-16", "endDate": "2026-11-20", "startPeriod": 1, "endPeriod": 7, "reason": "병가 테스트",
	})
	if code != http.StatusOK || (!strings.Contains(string(raw), "substituteOptions") && string(raw) != "null\n") {
		t.Fatalf("주중 미출근 대책 API 실패: status=%d, body=%s", code, string(raw))
	}

	// 7. 클라이언트에서 마스터/시간표 담당 권한으로 기초 시간표 셀 저장·삭제·일괄 자동 편성
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/cells", tokAdmin, protocol.TimetableCell{
		DayOfWeek: 4, Period: 6, ClassName: "1-3", Grade: 1, Subject: "국어", TeacherID: "t06",
	})
	if code != http.StatusOK {
		t.Fatalf("기초 시간표 셀 저장 실패: status=%d, body=%s", code, string(raw))
	}
	var savedCell protocol.TimetableCell
	_ = json.Unmarshal(raw, &savedCell)
	code, _ = doRecorderReq(t, h, http.MethodDelete, "/api/timetable/cells?id="+savedCell.ID, tokAdmin, nil)
	if code != http.StatusOK {
		t.Fatalf("기초 시간표 셀 삭제 실패: status=%d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/auto-generate", tokTimetable, map[string]interface{}{
		"grades": []int{1, 2, 3}, "classesPerGrade": 2, "clearExisting": false,
	})
	if code != http.StatusOK {
		t.Fatalf("학년·학급별 기초 시간표 자동 편성 실패: status=%d", code)
	}

	// 8. 정규 일과시간 설정, 과목별 상이 시험시간 배정 및 시스템 전체 초기화 검증
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/calendar/bells", tokTimetable, []protocol.BellPeriod{
		{Period: 1, Label: "1교시", StartTime: "08:50", EndTime: "09:35"},
		{Period: 2, Label: "2교시", StartTime: "09:45", EndTime: "10:30"},
	})
	if code != http.StatusOK || !strings.Contains(string(raw), "08:50") {
		t.Fatalf("정규 일과시간 설정 실패: status=%d, body=%s", code, string(raw))
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/generate", tokInternal, map[string]interface{}{
		"config": map[string]interface{}{"id": "exam-custom-time", "title": "맞춤 시험", "examDate": "2026-11-25", "isSealed": false, "instantRevealMode": true, "subjectTeacherPolicy": "CORRIDOR_FIRST"},
		"specs":  []map[string]interface{}{{"period": 1, "grade": 1, "subject": "국어", "startTime": "09:00", "endTime": "09:40", "durationMinutes": 40, "classRooms": []string{"1-1", "1-2"}}},
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"durationMinutes":40`) {
		t.Fatalf("과목별 시험시간 배정 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/teacher-policy", tokInternal, map[string]interface{}{
		"teacherId": "t15", "internalExamExempt": true, "internalExamExemptReason": "특수반(개별고사장 전담)",
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"maxGap"`) {
		t.Fatalf("교내 감독 제외 정책 API 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", tokInternal, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"roomCatalog"`) {
		t.Fatalf("교내 감독 고사실 카탈로그 조회 실패: status=%d", code)
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/system/reset", tokAdmin, map[string]string{"mode": "FULL_FACTORY_RESET"})
	if code != http.StatusOK {
		t.Fatalf("전체 완전 초기화 실패: status=%d, body=%s", code, string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": "1234"})
	if code != http.StatusOK {
		t.Fatalf("완전 초기화 후 기본 admin/1234 로그인 실패: status=%d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{"username": "t06", "password": "newpass99"})
	if code != http.StatusUnauthorized {
		t.Fatalf("완전 초기화 후 기존 교사 계정이 삭제되지 않았습니다: status=%d", code)
	}

	// 비동기 이벤트 버스 발행 검증
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	foundUnseal := false
	for _, ev := range capturedEvents {
		if ev.Type == protocol.EventExamUnsealed {
			foundUnseal = true
			break
		}
	}
	if !foundUnseal {
		t.Fatalf("EventExamUnsealed 도메인 이벤트가 발행되지 않았습니다")
	}
}

// TestWebSocketRealServerE2E는 실제 TCP 소켓 서버에서 토큰 검증 후 WebSocket 실시간 브로드캐스트 수신을 검증합니다.
func TestWebSocketRealServerE2E(t *testing.T) {
	_, h := newTestServerApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	tokT06 := loginForToken(t, h, "t06", "1234")
	tokInternal := loginForToken(t, h, "internal", "1234")

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws?token=" + url.QueryEscape(tokT06)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("제한된 샌드박스 환경에서 로컬 TCP 연결이 차단되어 WebSocket 테스트를 건너뜁니다")
		}
		t.Fatalf("WebSocket 연결 실패: %v", err)
	}
	defer wsConn.Close()

	code, _ := doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", tokInternal, nil)
	if code != http.StatusOK {
		t.Fatalf("개봉 요청 실패: %d", code)
	}
	_ = wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, readErr := wsConn.ReadMessage()
	if readErr != nil || !strings.Contains(string(msg), string(protocol.EventExamUnsealed)) {
		t.Fatalf("실시간 WebSocket 메시지 수신 실패: err=%v, msg=%s", readErr, string(msg))
	}
}
