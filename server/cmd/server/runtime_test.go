package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogRingBufferLimitAndConcurrency(t *testing.T) {
	buf := &logRingBuffer{max: 200}
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				_, _ = buf.Write([]byte("line-entry\n"))
			}
		}(g)
	}
	wg.Wait()

	lines := buf.Lines()
	if len(lines) != 200 {
		t.Fatalf("로그 링 버퍼는 최대 200줄을 유지해야 합니다: got=%d", len(lines))
	}
}

func TestControlAssetHandlerStatusAndDashboardHTML(t *testing.T) {
	rt := NewServerRuntime(ServerConfig{Port: 18099, SeedDemo: false})
	quitCh := make(chan struct{}, 1)
	h := newControlAssetHandler(rt, func() {
		select {
		case quitCh <- struct{}{}:
		default:
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/_control/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/_control/status 응답 코드 오류: %d, %s", rec.Code, rec.Body.String())
	}
	var snap ServerStatusSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("상태 JSON 파싱 실패: %v", err)
	}
	if snap.State != "INIT_DB" || snap.Port != 18099 || !strings.Contains(snap.LanURL, ":18099") {
		t.Fatalf("초기 상태 스냅샷이 올바르지 않습니다: %+v", snap)
	}

	// 내장 컨트롤 센터 HTML 서빙 확인
	htmlReq := httptest.NewRequest(http.MethodGet, "/", nil)
	htmlRec := httptest.NewRecorder()
	h.ServeHTTP(htmlRec, htmlReq)
	if htmlRec.Code != http.StatusOK || !strings.Contains(htmlRec.Body.String(), "꼬미시간서버 컨트롤 센터") {
		t.Fatalf("컨트롤 센터 index.html 서빙 실패: code=%d", htmlRec.Code)
	}

	// 안전 종료 콜백 호출 확인
	quitReq := httptest.NewRequest(http.MethodPost, "/_control/quit", nil)
	quitRec := httptest.NewRecorder()
	h.ServeHTTP(quitRec, quitReq)
	if quitRec.Code != http.StatusOK {
		t.Fatalf("/_control/quit 핸들러 응답 코드 오류: %d", quitRec.Code)
	}
	select {
	case <-quitCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("/_control/quit 비동기 종료 콜백이 호출되지 않았습니다")
	}
}
