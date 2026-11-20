package netclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/pkg/protocol"
)

// ClientVersion은 현재 교사 클라이언트의 릴리즈 버전입니다.
const ClientVersion = "0.1.01"

// ClientConfig는 교사용 클라이언트의 서버 접속 정보(os.Executable() 기준 절대 경로 저장)입니다.
type ClientConfig struct {
	ServerAddr string `json:"serverAddr"` // 예: "127.0.0.1:18090" 또는 "192.168.0.10:18090"
	LastUser   string `json:"lastUser"`
}

// Client는 꼬미시간서버와의 WebSocket 실시간 수신 및 자가 패치를 담당합니다.
type Client struct {
	mu        sync.RWMutex
	serverURL string
	teacherID string
	token     string
	stopCh    chan struct{}
	onEvent   func(protocol.DomainEvent)
}

// NewClient는 로컬 설정 파일에서 서버 주소를 읽어 netclient를 생성합니다.
func NewClient(onEvent func(protocol.DomainEvent)) *Client {
	cfg := LoadConfig()
	return &Client{
		serverURL: normalizeServerURL(cfg.ServerAddr),
		onEvent:   onEvent,
	}
}

func configPath() string {
	dir, err := pathutil.EnsureDir(pathutil.SafeJoin(pathutil.BaseDir(), "client-data"))
	if err != nil {
		return "client_config.json"
	}
	return pathutil.SafeJoin(dir, "client_config.json")
}

// LoadConfig는 저장된 서버 주소 및 최근 로그인 계정을 로드합니다.
func LoadConfig() ClientConfig {
	cfg := ClientConfig{ServerAddr: "http://127.0.0.1:18090"}
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	if strings.TrimSpace(cfg.ServerAddr) == "" {
		cfg.ServerAddr = "http://127.0.0.1:18090"
	}
	return cfg
}

// SaveConfig는 서버 주소 및 사용자 설정을 실행 파일 기준 경로에 저장합니다.
func SaveConfig(cfg ClientConfig) error {
	cfg.ServerAddr = normalizeServerURL(cfg.ServerAddr)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), raw, 0o600)
}

func normalizeServerURL(addr string) string {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "http://127.0.0.1:18090"
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "http://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

// ServerURL은 현재 연결 대상 HTTP 기본 주소를 반환합니다.
func (c *Client) ServerURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverURL
}

// SetConnection은 서버 주소, 교사 ID 및 세션 토큰을 갱신하고 WebSocket 연결을 재시작합니다.
func (c *Client) SetConnection(serverAddr, teacherID, token string) {
	c.mu.Lock()
	if strings.TrimSpace(serverAddr) != "" {
		c.serverURL = normalizeServerURL(serverAddr)
		cfg := LoadConfig()
		cfg.ServerAddr = c.serverURL
		cfg.LastUser = teacherID
		_ = SaveConfig(cfg)
	}
	c.teacherID = teacherID
	c.token = strings.TrimSpace(token)
	if c.stopCh != nil {
		close(c.stopCh)
	}
	c.stopCh = make(chan struct{})
	stop := c.stopCh
	c.mu.Unlock()

	go c.connectLoop(stop)
}

// Disconnect는 현재 활성화된 WebSocket 수신 루프를 즉시 중단합니다.
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.teacherID = ""
	if c.stopCh != nil {
		close(c.stopCh)
		c.stopCh = nil
	}
}

func (c *Client) connectLoop(stop <-chan struct{}) {
	backoff := 2 * time.Second
	const maxBackoff = 20 * time.Second

	for {
		select {
		case <-stop:
			return
		default:
		}

		c.mu.RLock()
		base := c.serverURL
		tid := c.teacherID
		tok := c.token
		c.mu.RUnlock()

		wsURL := strings.Replace(base, "http://", "ws://", 1)
		wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
		if tok != "" {
			wsURL = fmt.Sprintf("%s/ws?token=%s", wsURL, url.QueryEscape(tok))
		} else {
			wsURL = fmt.Sprintf("%s/ws?teacherId=%s", wsURL, url.QueryEscape(tid))
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			select {
			case <-stop:
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		backoff = 2 * time.Second
		_ = conn.SetReadDeadline(time.Now().Add(65 * time.Second))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(65 * time.Second))
		})

		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, data, readErr := conn.ReadMessage()
				if readErr != nil {
					return
				}
				var evt protocol.DomainEvent
				if jsonErr := json.Unmarshal(data, &evt); jsonErr == nil && c.onEvent != nil {
					c.onEvent(evt)
				}
			}
		}()

		select {
		case <-stop:
			_ = conn.Close()
			return
		case <-done:
			_ = conn.Close()
			select {
			case <-stop:
				return
			case <-time.After(backoff):
			}
		}
	}
}
