package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
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
	mu      sync.RWMutex
	clients map[*clientConn]struct{}
}

// NewHub는 WebSocket 허브 인스턴스를 생성합니다.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*clientConn]struct{}),
	}
}

// ServeWS는 WebSocket 업그레이드를 처리하고 실시간 이벤트 수신 채널을 유지합니다.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] 연결 업그레이드 오류: %v", err)
		return
	}
	teacherID := r.URL.Query().Get("teacherId")
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

// BroadcastEvent는 도메인 이벤트를 대상 교사(또는 전체 교사)에게 실시간 푸시합니다.
func (h *Hub) BroadcastEvent(evt protocol.DomainEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	h.mu.RLock()
	targets := make([]*clientConn, 0, len(h.clients))
	for c := range h.clients {
		if len(evt.TargetIDs) == 0 {
			targets = append(targets, c)
			continue
		}
		for _, id := range evt.TargetIDs {
			if c.teacherID == id || c.teacherID == "" {
				targets = append(targets, c)
				break
			}
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// 전송 큐가 가득 찬 느린 연결은 안전하게 건너뜀
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
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *Hub) writePump(c *clientConn) {
	defer h.removeClient(c)
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}
