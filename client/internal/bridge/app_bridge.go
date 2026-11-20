package bridge

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"kkomi-timetable/client/internal/netclient"
	"kkomi-timetable/pkg/protocol"
)

// AppBridge는 React 프론트엔드와 Wails v3 데스크톱 제어(트레이, 사이드 도킹 위젯, OS 알림, 서버 접속 설정)를 연결합니다.
type AppBridge struct {
	mu       sync.Mutex
	win      *application.WebviewWindow
	net      *netclient.Client
	notifier *notifications.NotificationService
	docked   bool
	quitting bool
}

// NewAppBridge는 데스크톱 브릿지를 생성합니다.
func NewAppBridge() *AppBridge {
	b := &AppBridge{}
	b.net = netclient.NewClient(func(evt protocol.DomainEvent) {
		app := application.Get()
		if app != nil {
			app.Event.Emit("kkomi:domain-event", evt)
		}
		b.mu.Lock()
		notifier := b.notifier
		b.mu.Unlock()
		if evt.Urgent && notifier != nil {
			_ = notifier.SendNotification(notifications.NotificationOptions{
				ID:    evt.ID,
				Title: evt.Title,
				Body:  evt.Message,
			})
		}
	})
	return b
}

// SetWindow는 메인 Wails 윈도우 참조를 연결합니다.
func (b *AppBridge) SetWindow(win *application.WebviewWindow) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.win = win
}

// SetNotifier는 OS 네이티브 알림 서비스를 연결합니다.
func (b *AppBridge) SetNotifier(n *notifications.NotificationService) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notifier = n
}

// MarkQuitting은 트레이 숨김 대신 완전 종료 상태로 표시합니다.
func (b *AppBridge) MarkQuitting() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.quitting = true
}

// IsQuitting은 완전 종료 상태인지 반환합니다.
func (b *AppBridge) IsQuitting() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.quitting
}

// GetServerURL은 현재 저장된 서버 HTTP 주소를 반환합니다.
func (b *AppBridge) GetServerURL() string {
	return b.net.ServerURL()
}

// ConnectServer는 서버 주소, 교사 ID 및 세션 토큰을 저장하고 실시간 WebSocket 수신을 시작합니다.
func (b *AppBridge) ConnectServer(serverURL, teacherID, token string) string {
	b.net.SetConnection(serverURL, teacherID, token)
	return b.net.ServerURL()
}

// Disconnect는 로그아웃 또는 앱 종료 시 실시간 WebSocket 수신을 즉시 중단합니다.
func (b *AppBridge) Disconnect() {
	b.net.Disconnect()
}

// ApplySelfUpdate는 서버에 배포된 최신 클라이언트 패치를 내려받아 자가 재실행합니다.
func (b *AppBridge) ApplySelfUpdate() error {
	return b.net.ApplyClientSelfUpdate()
}

// ToggleSideDockMode는 창을 화면 우측 가장자리 폭 260px 사이드 도킹 위젯 모드로 전환하거나 기본 크기로 복원합니다.
func (b *AppBridge) ToggleSideDockMode(enable bool) bool {
	b.mu.Lock()
	win := b.win
	b.docked = enable
	b.mu.Unlock()

	if win == nil {
		return false
	}
	app := application.Get()
	if enable {
		width, height := 1440, 900
		if app != nil {
			screens := app.Screen.GetAll()
			for _, sc := range screens {
				if sc.IsPrimary && sc.Size.Width > 0 {
					width = sc.Size.Width
					height = sc.Size.Height
					break
				}
			}
		}
		dockHeight := height - 80
		if dockHeight < 640 {
			dockHeight = 640
		}
		win.UnMaximise()
		win.SetSize(280, dockHeight)
		win.SetPosition(width-290, 40)
		win.SetAlwaysOnTop(true)
	} else {
		win.SetAlwaysOnTop(false)
		win.SetSize(1320, 840)
		win.Center()
	}

	if app != nil {
		app.Event.Emit("kkomi:dock-mode", enable)
	}
	return enable
}

// ShowMainWindow는 트레이에 숨겨진 창을 복원하여 전면 포커스합니다.
func (b *AppBridge) ShowMainWindow() {
	b.mu.Lock()
	win := b.win
	b.mu.Unlock()
	if win == nil {
		return
	}
	win.Show()
	win.UnMinimise()
	win.Focus()
}

// PrintWindow는 현재 화면의 네이티브 인쇄/PDF 대화상자를 실행합니다.
func (b *AppBridge) PrintWindow() {
	b.mu.Lock()
	win := b.win
	b.mu.Unlock()
	if win != nil {
		_ = win.Print()
	}
}
