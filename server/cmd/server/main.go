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

func main() {
	port := flag.Int("port", 18090, "업무망 LAN HTTP/WebSocket 서버 포트")
	initTemplate := flag.Bool("init-template", false, "기본 .hwpx 템플릿 파일만 생성하고 종료")
	_ = flag.Bool("headless", true, "콘솔 데몬 모드 호환 플래그")
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
	log.Printf("🐾 꼬미시간 통합 학사·시험감독 서버 v%s (CMD 콘솔 서버)", ws.ServerVersion)
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
	log.Printf("🌐 업무망 고정 IP 웹 접속: http://%s:%d", lanIP, *port)
	log.Printf("🔌 교사용 클라이언트(꼬미시간.exe) 서버 주소 입력: http://%s:%d", lanIP, *port)
	log.Println("💡 교사 명렬표·학년별/학급별 기초 시간표 작성은 클라이언트(마스터/담당 권한)에서 수행하세요.")
	log.Println("🛑 종료하려면 이 창을 닫거나 Ctrl+C를 누르세요.")
	log.Println("============================================================")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("\n[시스템] 종료 신호를 수신했습니다. DB 및 백그라운드 프로세스를 안전하게 정리합니다...")
		serverApp.InternalExam.StopScheduler()
		shutdownFn()
		os.Exit(0)
	}()

	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[서버 오류] HTTP/WebSocket 리스닝 실패: %v", err)
	}
}
