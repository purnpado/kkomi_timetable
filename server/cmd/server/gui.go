package main

import (
	"fmt"
	"io/fs"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"kkomi-timetable/server/internal/ws"
)

// runServerGUI는 교무실 공용 PC에서 검은 콘솔창 대신 Wails v3 GUI 창과
// 시스템 트레이 아이콘을 띄워 서버 상태 안내 및 안전 종료를 제어합니다.
func runServerGUI(webFS fs.FS, lanIP string, port int, dbMode string, onShutdown func()) {
	var quitting atomic.Bool
	notifier := notifications.New()

	wailsApp := application.New(application.Options{
		Name:        "꼬미시간서버",
		Description: "꼬미시간 교무실 공용 서버 및 통합 관리 콘솔",
		Services:    []application.Service{application.NewService(notifier)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(webFS),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		OnShutdown: func() {
			if onShutdown != nil {
				onShutdown()
			}
		},
	})

	title := fmt.Sprintf("꼬미시간 서버 v%s  |  교무실 접속 주소: http://%s:%d  |  %s", ws.ServerVersion, lanIP, port, dbMode)
	win := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            title,
		Width:            1320,
		Height:           840,
		MinWidth:         900,
		MinHeight:        600,
		BackgroundColour: application.NewRGBA(244, 246, 251, 255),
		URL:              "/",
	})

	showWindow := func() {
		win.Show()
		win.UnMinimise()
		win.Focus()
	}

	notifier.OnNotificationResponse(func(notifications.NotificationResult) {
		showWindow()
	})

	// 실수로 창 닫기(X)를 눌러도 서버가 꺼지지 않고 트레이로 숨겨지도록 보호
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !quitting.Load() {
			event.Cancel()
			win.Hide()
			_ = notifier.SendNotification(notifications.NotificationOptions{
				ID:    "kkomi-server-tray-hide",
				Title: "꼬미시간 서버가 백그라운드에서 실행 중입니다",
				Body:  fmt.Sprintf("업무망 접속 주소: http://%s:%d (트레이 아이콘에서 다시 열 수 있습니다)", lanIP, port),
			})
		}
	})

	tray := wailsApp.SystemTray.New()
	tray.SetIcon(serverTrayIcon)
	tray.SetTooltip(fmt.Sprintf("꼬미시간 서버 (%s:%d)", lanIP, port))

	menu := application.NewMenu()
	menu.Add(fmt.Sprintf("접속 주소: %s:%d", lanIP, port)).SetEnabled(false)
	menu.Add(fmt.Sprintf("DB: %s", dbMode)).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("꼬미시간 서버 관리창 열기").OnClick(func(*application.Context) {
		showWindow()
	})
	menu.Add("접속 IP 알림 다시 띄우기").OnClick(func(*application.Context) {
		_ = notifier.SendNotification(notifications.NotificationOptions{
			ID:    "kkomi-server-ip-info",
			Title: "꼬미시간 서버 업무망 접속 안내",
			Body:  fmt.Sprintf("교사용 클라이언트 서버 주소에 http://%s:%d 를 입력하세요.", lanIP, port),
		})
	})
	menu.AddSeparator()
	menu.Add("서버 및 DB 완전 종료").OnClick(func(*application.Context) {
		quitting.Store(true)
		if onShutdown != nil {
			onShutdown()
		}
		wailsApp.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(showWindow)

	if err := wailsApp.Run(); err != nil {
		log.Fatal("꼬미시간 서버 GUI 실행 오류:", err)
	}
}
