package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:gui_assets
var guiAssetsFS embed.FS

//go:embed assets/tray-icon.png
var serverTrayIcon []byte

func writeControlJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func newControlAssetHandler(rt *ServerRuntime, onQuit func()) http.Handler {
	subFS, err := fs.Sub(guiAssetsFS, "gui_assets")
	var staticHandler http.Handler
	if err == nil {
		staticHandler = http.FileServer(http.FS(subFS))
	} else {
		staticHandler = http.NotFoundHandler()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/_control/status", func(w http.ResponseWriter, r *http.Request) {
		writeControlJSON(w, http.StatusOK, rt.Status())
	})
	mux.HandleFunc("/_control/backup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeControlJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST 요청만 허용됩니다"})
			return
		}
		fileName, bErr := rt.CreateBackup()
		if bErr != nil {
			writeControlJSON(w, http.StatusBadRequest, map[string]string{"error": bErr.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]string{"backupFile": fileName})
	})
	mux.HandleFunc("/_control/open-folder", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeControlJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST 요청만 허용됩니다"})
			return
		}
		if err := rt.OpenBaseFolder(); err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/_control/open-web", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeControlJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST 요청만 허용됩니다"})
			return
		}
		if err := rt.OpenWebAdmin(); err != nil {
			writeControlJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/_control/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeControlJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST 요청만 허용됩니다"})
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
		if onQuit != nil {
			go onQuit()
		}
	})
	mux.Handle("/", staticHandler)
	return mux
}

func runControlCenterGUI(rt *ServerRuntime) {
	var quitting atomic.Bool
	var wailsApp *application.App
	var win *application.WebviewWindow

	triggerQuit := func() {
		if !quitting.CompareAndSwap(false, true) {
			return
		}
		rt.Shutdown()
		if wailsApp != nil {
			wailsApp.Quit()
		}
	}

	handler := newControlAssetHandler(rt, triggerQuit)
	wailsApp = application.New(application.Options{
		Name:        "꼬미시간서버",
		Description: "꼬미시간 학사·시험감독 교무실 공용 서버 컨트롤 센터",
		Assets: application.AssetOptions{
			Handler: handler,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	win = wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "꼬미시간서버 - 컨트롤 센터",
		Width:            520,
		Height:           640,
		MinWidth:         460,
		MinHeight:        540,
		BackgroundColour: application.NewRGBA(244, 246, 251, 255),
		URL:              "/",
	})

	showWindow := func() {
		if win == nil {
			return
		}
		win.Show()
		win.UnMinimise()
		win.Focus()
	}

	// 창 닫기(X) 시 실수 종료를 방지하고 시스템 트레이로 최소화
	win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !quitting.Load() {
			event.Cancel()
			win.Hide()
		}
	})

	tray := wailsApp.SystemTray.New()
	tray.SetIcon(serverTrayIcon)
	tray.SetTooltip("꼬미시간서버 (교무실 공용 서버)")
	menu := application.NewMenu()
	menu.Add("컨트롤 센터 열기").OnClick(func(*application.Context) {
		showWindow()
	})
	menu.Add("웹 관리 화면 열기").OnClick(func(*application.Context) {
		_ = rt.OpenWebAdmin()
	})
	menu.Add("즉시 암호화 백업").OnClick(func(*application.Context) {
		if _, err := rt.CreateBackup(); err != nil {
			log.Printf("[백업] 트레이 백업 실패: %v", err)
		}
	})
	menu.Add("서버 폴더 열기").OnClick(func(*application.Context) {
		_ = rt.OpenBaseFolder()
	})
	menu.AddSeparator()
	menu.Add("서버 안전 종료").OnClick(func(*application.Context) {
		triggerQuit()
	})
	tray.SetMenu(menu)
	tray.OnClick(func() {
		showWindow()
	})

	rt.StartAsync()
	defer rt.Shutdown()

	if err := wailsApp.Run(); err != nil {
		writeServerErrorLog("꼬미시간서버 GUI 컨트롤 센터 실행 실패", err)
		log.Fatal("꼬미시간서버 GUI 실행 오류:", err)
	}
}
