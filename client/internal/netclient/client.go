package netclient

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/pkg/protocol"
)

// ClientVersion은 현재 교사 클라이언트의 릴리즈 버전입니다.
const ClientVersion = "0.1.01"

const gitHubLatestAPI = "https://api.github.com/repos/purnpado/kkomi_timetable/releases/latest"

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

// SetConnection은 서버 주소 또는 로그인 교사 ID를 갱신하고 WebSocket 연결을 재시작합니다.
func (c *Client) SetConnection(serverAddr, teacherID string) {
	c.mu.Lock()
	if strings.TrimSpace(serverAddr) != "" {
		c.serverURL = normalizeServerURL(serverAddr)
		cfg := LoadConfig()
		cfg.ServerAddr = c.serverURL
		_ = SaveConfig(cfg)
	}
	c.teacherID = teacherID
	if c.stopCh != nil {
		close(c.stopCh)
	}
	c.stopCh = make(chan struct{})
	stop := c.stopCh
	c.mu.Unlock()

	go c.connectLoop(stop)
}

func (c *Client) connectLoop(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		c.mu.RLock()
		base := c.serverURL
		tid := c.teacherID
		c.mu.RUnlock()

		wsURL := strings.Replace(base, "http://", "ws://", 1)
		wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
		wsURL = fmt.Sprintf("%s/ws?teacherId=%s", wsURL, tid)

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

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
			time.Sleep(2 * time.Second)
		}
	}
}

// ApplyClientSelfUpdate는 서버에 배포된 최신 클라이언트 패치 또는 GitHub Releases(v0.1.01+)에서
// 최신 꼬미시간 실행 파일을 내려받아 자가 교체 재실행합니다.
func (c *Client) ApplyClientSelfUpdate() error {
	var body io.ReadCloser
	patchURL := c.ServerURL() + "/api/system/client-patch"
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(patchURL) // #nosec G107 -- 업무망 내부 서버 URL
	if err == nil && resp.StatusCode == http.StatusOK {
		body = resp.Body
	} else {
		if resp != nil {
			_ = resp.Body.Close()
		}
		ghBody, ghErr := openGitHubClientAsset(client)
		if ghErr != nil {
			return ghErr
		}
		body = ghBody
	}
	defer body.Close()

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	newPath := exePath + ".new"
	out, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, body); err != nil {
		_ = out.Close()
		return err
	}
	_ = out.Close()

	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath)
		return err
	}

	go func() {
		time.Sleep(600 * time.Millisecond)
		cmd := exec.Command(exePath, os.Args[1:]...)
		if startErr := cmd.Start(); startErr != nil {
			log.Printf("[클라이언트 업데이트] 재실행 실패: %v", startErr)
		}
		os.Exit(0)
	}()
	return nil
}

func openGitHubClientAsset(httpCli *http.Client) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, gitHubLatestAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "KkomiTimetable-Client/"+ClientVersion)

	resp, err := httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("서버 및 GitHub 릴리즈 업데이트 연결 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 최신 릴리즈 조회 실패 (상태코드 %d)", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("GitHub 릴리즈 응답 파싱 실패: %w", err)
	}

	var assetName, assetURL string
	for _, a := range rel.Assets {
		low := strings.ToLower(a.Name)
		if strings.Contains(a.Name, "꼬미시간") && !strings.Contains(a.Name, "서버") && strings.HasSuffix(low, ".exe") {
			assetName = a.Name
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return nil, fmt.Errorf("GitHub 릴리즈(%s)에서 클라이언트 실행 파일(꼬미시간.exe)을 찾지 못했습니다", rel.TagName)
	}
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("GitHub 최신 릴리즈(%s)는 Windows용(%s)입니다. macOS에서는 소스 빌드를 이용해 주세요", rel.TagName, assetName)
	}

	dlResp, err := httpCli.Get(assetURL) // #nosec G107 -- GitHub Releases 공식 다운로드 URL
	if err != nil {
		return nil, fmt.Errorf("GitHub 클라이언트 다운로드 실패: %w", err)
	}
	if dlResp.StatusCode != http.StatusOK {
		_ = dlResp.Body.Close()
		return nil, fmt.Errorf("GitHub 클라이언트 다운로드 오류 (%d)", dlResp.StatusCode)
	}
	return dlResp.Body, nil
}
