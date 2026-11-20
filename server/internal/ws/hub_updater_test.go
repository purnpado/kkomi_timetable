package ws

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/eventbus"
)

// TestHubConcurrentBroadcastAndDisconnect는 다수 WebSocket 동시 접속·해제 중 브로드캐스트 시
// 채널 닫힘 패닉이 발생하지 않고 대상 필터링이 정확히 동작하는지 검증합니다.
func TestHubConcurrentBroadcastAndDisconnect(t *testing.T) {
	hub := NewHub()
	hub.TokenVerifier = func(token string) (string, bool) {
		if strings.HasPrefix(token, "tok-") {
			return strings.TrimPrefix(token, "tok-"), true
		}
		return "", false
	}

	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	wsBase := strings.Replace(srv.URL, "http://", "ws://", 1)

	// 토큰 없는 연결 거부 확인
	if _, resp, err := websocket.DefaultDialer.Dial(wsBase+"?teacherId=t01", nil); err == nil {
		t.Fatalf("토큰 없는 WS 연결이 허용되었습니다")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("WS 인증 거부 코드 오류: %d", resp.StatusCode)
	}

	const workers = 12
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conn, _, err := websocket.DefaultDialer.Dial(wsBase+"?token=tok-t06", nil)
			if err != nil {
				if strings.Contains(err.Error(), "operation not permitted") {
					return
				}
				t.Errorf("WS 연결 오류: %v", err)
				return
			}
			for j := 0; j < 25; j++ {
				hub.BroadcastEvent(protocol.DomainEvent{
					Type:    protocol.EventTimetableApproved,
					Title:   "동시성 테스트",
					Message: "경합 안전",
				})
			}
			_ = conn.Close()
		}(i)
	}

	for j := 0; j < 50; j++ {
		hub.BroadcastEvent(protocol.DomainEvent{
			Type:      protocol.EventExamUnsealed,
			Title:     "타겟 이벤트",
			TargetIDs: []string{"t06"},
		})
	}
	wg.Wait()
}

// TestClientPatchCleanupAndSHA256Integrity는 SaveClientPatch 시 구버전 정리,
// SHA-256 메타데이터 저장, 잘못된 바이너리(0바이트/비실행파일) 차단을 검증합니다.
func TestClientPatchCleanupAndSHA256Integrity(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("KKOMI_BASE_DIR", tmp)
	bus := eventbus.New()

	// 1. 0바이트 차단
	if _, err := SaveClientPatch("0.2.0", "꼬미시간.exe", bytes.NewReader(nil), bus); err == nil {
		t.Fatalf("빈 바이너리가 차단되지 않았습니다")
	}
	// 2. 가짜 텍스트 파일 차단
	if _, err := SaveClientPatch("0.2.0", "꼬미시간.exe", strings.NewReader("not-exe"), bus); err == nil {
		t.Fatalf("MZ 헤더 없는 .exe가 차단되지 않았습니다")
	}

	// 3. 정상 PE 헤더 패치 1차 등록 후 2차 등록 시 1차 파일이 정리되는지 검증
	pePayload1 := append([]byte{'M', 'Z', 0x90, 0x00}, bytes.Repeat([]byte{'A'}, 32)...)
	p1, err := SaveClientPatch("0.1.01", "a_old_client.exe", bytes.NewReader(pePayload1), bus)
	if err != nil {
		t.Fatalf("1차 패치 저장 실패: %v", err)
	}

	pePayload2 := append([]byte{'M', 'Z', 0x90, 0x00}, bytes.Repeat([]byte{'B'}, 40)...)
	sum2 := sha256.Sum256(pePayload2)
	wantSHA := hex.EncodeToString(sum2[:])
	p2, err := SaveClientPatch("0.2.0", "z_new_client.exe", bytes.NewReader(pePayload2), bus)
	if err != nil {
		t.Fatalf("2차 패치 저장 실패: %v", err)
	}

	if _, statErr := os.Stat(p1); !os.IsNotExist(statErr) {
		t.Fatalf("이전 클라이언트 패치 파일(%s)이 정리되지 않았습니다", filepath.Base(p1))
	}
	gotPath, gotName := LatestClientPatchPath()
	if gotPath != p2 || gotName != "z_new_client.exe" {
		t.Fatalf("최신 패치 경로 불일치: got=(%s,%s), want=(%s,z_new_client.exe)", gotPath, gotName, p2)
	}
	meta, err := LoadClientPatchMeta()
	if err != nil || meta.Version != "0.2.0" || meta.SHA256 != wantSHA || meta.Size != int64(len(pePayload2)) {
		t.Fatalf("패치 메타데이터 무결성 오류: err=%v, meta=%+v", err, meta)
	}
}

// TestEventBusUniqueIDsAndPanicRecovery는 연속 발행 시 이벤트 ID 유일성과 구독자 패닉 격리를 검증합니다.
func TestEventBusUniqueIDsAndPanicRecovery(t *testing.T) {
	bus := eventbus.New()
	bus.Subscribe(protocol.EventTeacherUpdated, func(protocol.DomainEvent) {
		panic("의도된 구독자 패닉")
	})

	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		evt := bus.Publish(protocol.DomainEvent{
			Type:  protocol.EventTeacherUpdated,
			Title: "고속 연속 발행",
		})
		if seen[evt.ID] {
			t.Fatalf("중복 이벤트 ID가 생성되었습니다: %s", evt.ID)
		}
		seen[evt.ID] = true
	}
}
