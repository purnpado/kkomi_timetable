package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"kkomi-timetable/client/internal/bridge"
	"kkomi-timetable/pkg/pathutil"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/tray-icon.png
var trayIcon []byte

func main() {
	pathutil.CleanupOldBinaries()

	appBridge := bridge.NewAppBridge()
	notifier := notifications.New()
	notifier.OnNotificationResponse(func(notifications.NotificationResult) {
		appBridge.ShowMainWindow()
	})
	appBridge.SetNotifier(notifier)

	services := []application.Service{
		application.NewService(appBridge),
		application.NewService(notifier),
	}

	wailsApp := application.New(application.Options{
		Name:        "꼬미시간",
		Description: "중·고등학교 학사 시간표, 내·외부 시험감독 및 주중·월중 행사 통합 관리 솔루션",
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "꼬미시간 - 교무실 통합 대시보드",
		Width:            1320,
		Height:           840,
		MinWidth:         260,
		MinHeight:        520,
		BackgroundColour: application.NewRGBA(244, 246, 251, 255),
		URL:              "/",
	})
	appBridge.SetWindow(win)

	// 창 닫기 시 트레이 상주 유지 (완전 종료 메뉴 클릭 시에만 종료)
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !appBridge.IsQuitting() {
			event.Cancel()
			win.Hide()
		}
	})

	// 시스템 트레이 메뉴 구성
	tray := wailsApp.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("꼬미시간 (교무실 통합 대시보드)")
	menu := application.NewMenu()
	menu.Add("꼬미시간 대시보드 열기").OnClick(func(*application.Context) {
		appBridge.ShowMainWindow()
	})
	menu.Add("사이드 도킹 위젯 모드 (260px)").OnClick(func(*application.Context) {
		appBridge.ShowMainWindow()
		appBridge.ToggleSideDockMode(true)
	})
	menu.Add("기본 창 크기로 복원").OnClick(func(*application.Context) {
		appBridge.ShowMainWindow()
		appBridge.ToggleSideDockMode(false)
	})
	menu.AddSeparator()
	menu.Add("완전 종료").OnClick(func(*application.Context) {
		appBridge.MarkQuitting()
		wailsApp.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(func() {
		appBridge.ShowMainWindow()
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal("꼬미시간 클라이언트 실행 오류:", err)
	}
}
