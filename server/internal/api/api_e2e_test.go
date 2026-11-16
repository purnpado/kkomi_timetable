package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/api"
	"kkomi-timetable/server/internal/database"
)

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

func doRecorderReq(t *testing.T, h http.Handler, method, path, teacherID string, body interface{}) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("요청 JSON 직렬화 실패: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if teacherID != "" {
		req.Header.Set("X-Teacher-ID", teacherID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
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

	// 1. 헬스체크 (v0.1.0 확인)
	code, raw := doRecorderReq(t, h, http.MethodGet, "/api/health", "", nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"version":"0.1.0"`) {
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
		"username": "master", "password": "1234",
	})
	if code != http.StatusOK || strings.Contains(string(raw), "passwordHash") {
		t.Fatalf("로그인 실패 또는 해시 노출 (status=%d)", code)
	}

	// 3. 교내 지필고사 봉인 마스킹 -> 권한 없는 교사 차단 -> 관리자 개봉 및 이벤트 발행
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", "t06", nil)
	if code != http.StatusOK || !strings.Contains(string(raw), "공개") {
		t.Fatalf("일반 교사 조회 시 고사장 봉인이 풀렸습니다: %s", string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", "t06", nil)
	if code != http.StatusForbidden {
		t.Fatalf("권한 없는 교사의 개봉 요청이 차단되지 않음: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", "t03", nil)
	if code != http.StatusOK {
		t.Fatalf("교내감독 관리자 고사장 발표 실패: %d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", "t06", nil)
	if code != http.StatusOK || strings.Contains(string(raw), `"actualRoomHidden":true`) {
		t.Fatalf("개봉 이후에도 고사장이 숨겨져 있습니다: %s", string(raw))
	}

	// 4. 시간표 조회, 신호등 판정, 대기열 확정 및 나이스 입력 도우미
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/dashboard", "t06", nil)
	if code != http.StatusOK {
		t.Fatalf("대시보드 조회 실패: %d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/evaluate", "t02", protocol.EvaluateMoveRequest{
		SourceCellID: "c-1-3-11",
		TargetDay:    2,
		TargetPeriod: 3,
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"signal":"RED"`) {
		t.Fatalf("순회교사 출강일 충돌 RED 판정 실패: %s", string(raw))
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/commit", "t02", map[string]interface{}{
		"requestId": "req-seed-1", "approve": true,
	})
	if code != http.StatusOK {
		t.Fatalf("시간표 담당교사 최종 선택 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/timetable/neis", "t02", nil)
	if code != http.StatusOK {
		t.Fatalf("나이스 입력 도우미 조회 실패: %d", code)
	}

	// 5. 외부감독 지원 및 후보 산정 조회
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/volunteer", "t06", map[string]string{"noticeId": "ext-2027-csat"})
	if code != http.StatusOK {
		t.Fatalf("외부감독 지원 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/external-exam/candidates?noticeId=ext-2027-csat", "t04", nil)
	if code != http.StatusOK {
		t.Fatalf("외부감독 후보 산정 조회 실패: %d", code)
	}

	// 6. 행사 취합 확정, 백업 및 출력(.hwpx, .xlsx, 고대비 인쇄)
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/calendar/commit", "t05", map[string]interface{}{
		"eventId": "evt-seed-2", "approve": true,
	})
	if code != http.StatusOK {
		t.Fatalf("행사 담당교사 확정 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/system/backups", "t01", nil)
	if code != http.StatusOK {
		t.Fatalf("시스템 마스터 백업 생성 실패: %d", code)
	}
	for _, ep := range []string{"/api/export/hwpx?kind=calendar", "/api/export/xlsx?kind=timetable", "/api/export/xlsx?kind=base-template", "/api/export/print"} {
		c, body := doRecorderReq(t, h, http.MethodGet, ep, "t01", nil)
		if c != http.StatusOK || len(body) == 0 {
			t.Fatalf("출력 엔드포인트 실패 (%s): status=%d", ep, c)
		}
	}

	// 7. 클라이언트에서 마스터/시간표 담당 권한으로 기초 시간표 셀 저장·삭제·일괄 자동 편성
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/cells", "t01", protocol.TimetableCell{
		DayOfWeek: 4, Period: 6, ClassName: "1-3", Grade: 1, Subject: "국어", TeacherID: "t06",
	})
	if code != http.StatusOK {
		t.Fatalf("기초 시간표 셀 저장 실패: status=%d, body=%s", code, string(raw))
	}
	var savedCell protocol.TimetableCell
	_ = json.Unmarshal(raw, &savedCell)
	code, _ = doRecorderReq(t, h, http.MethodDelete, "/api/timetable/cells?id="+savedCell.ID, "t01", nil)
	if code != http.StatusOK {
		t.Fatalf("기초 시간표 셀 삭제 실패: status=%d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/auto-generate", "t02", map[string]interface{}{
		"grades": []int{1, 2, 3}, "classesPerGrade": 2, "clearExisting": false,
	})
	if code != http.StatusOK {
		t.Fatalf("학년·학급별 기초 시간표 자동 편성 실패: status=%d", code)
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

// TestWebSocketRealServerE2E는 실제 TCP 소켓 서버에서 WebSocket 실시간 브로드캐스트 수신을 검증합니다.
func TestWebSocketRealServerE2E(t *testing.T) {
	_, h := newTestServerApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws?teacherId=t06"
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("제한된 샌드박스 환경에서 로컬 TCP 연결이 차단되어 WebSocket 테스트를 건너뜁니다")
		}
		t.Fatalf("WebSocket 연결 실패: %v", err)
	}
	defer wsConn.Close()

	code, _ := doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", "t03", nil)
	if code != http.StatusOK {
		t.Fatalf("개봉 요청 실패: %d", code)
	}
	_ = wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, readErr := wsConn.ReadMessage()
	if readErr != nil || !strings.Contains(string(msg), string(protocol.EventExamUnsealed)) {
		t.Fatalf("실시간 WebSocket 메시지 수신 실패: err=%v, msg=%s", readErr, string(msg))
	}
}
