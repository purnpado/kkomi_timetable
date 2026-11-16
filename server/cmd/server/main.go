package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/server/internal/api"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/exporter"
	"kkomi-timetable/server/internal/ws"
)

//go:embed all:webdist
var embeddedWebFS embed.FS

//go:embed tray-icon.png
var serverTrayIcon []byte

func main() {
	port := flag.Int("port", 18090, "업무망 LAN HTTP/WebSocket 서버 포트")
	initTemplate := flag.Bool("init-template", false, "기본 .hwpx 템플릿 파일만 생성하고 종료")
	headless := flag.Bool("headless", false, "GUI 창/트레이 없이 콘솔 데몬 모드로만 실행")
	flag.Parse()

	if *initTemplate {
		if _, err := exporter.EnsureDefaultHWPXTemplate(); err != nil {
			log.Fatalf("[오류] 기본 템플릿 생성 실패: %v", err)
		}
		return
	}

	if envPort := os.Getenv("KKOMI_PORT"); envPort != "" {
		var p int
		if _, err := fmt.Sscanf(envPort, "%d", &p); err == nil && p > 0 {
			*port = p
		}
	}

	// 이전 무중단 자가 업데이트 잔여 파일(.old) 정리
	pathutil.CleanupOldBinaries()

	log.Println("============================================================")
	log.Printf("🐾 꼬미시간 통합 학사·시험감독 서버 v%s 시작", ws.ServerVersion)
	log.Printf("📁 실행 기준 절대경로: %s", pathutil.BaseDir())

	store, err := database.Open()
	if err != nil {
		log.Fatalf("[치명적 오류] 데이터베이스 초기화 실패: %v", err)
	}

	if err := database.SeedIfEmpty(store); err != nil {
		log.Printf("[경고] 초기 데이터 시딩 중 오류: %v", err)
	}
	if tplPath, err := exporter.EnsureDefaultHWPXTemplate(); err == nil {
		log.Printf("📄 한글(.hwpx) 기본 템플릿 준비 완료: %s", tplPath)
	}

	var httpServer *http.Server
	var cleaned atomic.Bool
	shutdownFn := func() {
		if !cleaned.CompareAndSwap(false, true) {
			return
		}
		if httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
		}
		_ = store.Close()
	}

	serverApp := api.NewServerApp(store, shutdownFn)
	webSubFS, err := fs.Sub(embeddedWebFS, "webdist")
	if err != nil {
		log.Fatalf("[오류] 내장 웹 에셋 마운트 실패: %v", err)
	}

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("[서버 오류] 포트(%d) 열기 실패 (이미 서버가 실행 중일 수 있습니다): %v", *port, err)
	}

	httpServer = &http.Server{
		Handler:           serverApp.Handler(webSubFS),
		ReadHeaderTimeout: 10 * time.Second,
	}

	lanIP := ws.LocalIPAddress()
	log.Printf("🗄️  DB 구동 모드: %s", store.ModeLabel())
	log.Printf("🌐 업무망 고정 IP 접속 안내: http://%s:%d", lanIP, *port)
	log.Printf("🔌 교사용 클라이언트(꼬미시간.exe) 서버 주소 입력: %s:%d", lanIP, *port)
	log.Println("============================================================")

	// OS 종료 시그널(SIGINT, SIGTERM) 수신 시 안전 종료
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("\n[시스템] 종료 신호를 수신했습니다. DB 및 백그라운드 프로세스를 안전하게 정리합니다...")
		serverApp.InternalExam.StopScheduler()
		shutdownFn()
		os.Exit(0)
	}()

	if *headless {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[서버 오류] HTTP/WebSocket 리스닝 실패: %v", err)
		}
		return
	}

	// 백그라운드 고루틴으로 업무망 HTTP/WebSocket 서버 실행 후 Wails v3 GUI + 시스템 트레이 시작
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[서버 오류] HTTP/WebSocket 서빙 중단: %v", err)
		}
	}()

	runServerGUI(webSubFS, lanIP, *port, store.ModeLabel(), func() {
		serverApp.InternalExam.StopScheduler()
		shutdownFn()
	})
}
