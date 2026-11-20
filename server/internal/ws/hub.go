package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
	maxMsgSize = 64 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // 학내 폐쇄망 LAN 다중 클라이언트 접속 허용
}

type clientConn struct {
	conn      *websocket.Conn
	teacherID string
	send      chan []byte
}

// Hub는 업무망 교사 클라이언트들과의 실시간 WebSocket 연결을 관리합니다.
type Hub struct {
	mu            sync.RWMutex
	clients       map[*clientConn]struct{}
	TokenVerifier func(token string) (string, bool)
}

// NewHub는 WebSocket 허브 인스턴스를 생성합니다.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*clientConn]struct{}),
	}
}

func extractWSToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// ServeWS는 토큰 검증 후 WebSocket 업그레이드를 처리하고 Ping/Pong 하트비트를 유지합니다.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	teacherID := ""
	if h.TokenVerifier != nil {
		token := extractWSToken(r)
		tid, ok := h.TokenVerifier(token)
		if !ok || strings.TrimSpace(tid) == "" {
			http.Error(w, "유효한 WebSocket 인증 토큰이 필요합니다", http.StatusUnauthorized)
			return
		}
		teacherID = tid
	} else {
		teacherID = strings.TrimSpace(r.URL.Query().Get("teacherId"))
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] 연결 업그레이드 오류: %v", err)
		return
	}
	c := &clientConn{
		conn:      conn,
		teacherID: teacherID,
		send:      make(chan []byte, 64),
	}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	go h.readPump(c)
}

// BroadcastEvent는 도메인 이벤트를 대상 교사(또는 전체 교사)에게 채널 닫힘 경합 없이 안전하게 푸시합니다.
func (h *Hub) BroadcastEvent(evt protocol.DomainEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	// RLock 유지 중 송신하여 removeClient의 close(c.send)와 절대 경합하지 않도록 보장
	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		if len(evt.TargetIDs) > 0 {
			matched := false
			for _, id := range evt.TargetIDs {
				if c.teacherID != "" && c.teacherID == id {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		select {
		case c.send <- data:
		default:
			// 전송 큐가 가득 찬 느린 연결은 블로킹 없이 건너뜀
		}
	}
}

// ConnectedCount는 현재 접속 중인 교사 클라이언트 수를 반환합니다.
func (h *Hub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) removeClient(c *clientConn) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
	_ = c.conn.Close()
}

func (h *Hub) readPump(c *clientConn) {
	defer h.removeClient(c)
	c.conn.SetReadLimit(maxMsgSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *Hub) writePump(c *clientConn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		h.removeClient(c)
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
