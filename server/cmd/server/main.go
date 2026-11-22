package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/server/internal/exporter"
)

func main() {
	port := flag.Int("port", 18090, "업무망 LAN HTTP/WebSocket 서버 포트")
	initTemplate := flag.Bool("init-template", false, "기본 .hwpx 템플릿 파일만 생성하고 종료")
	seedDemo := flag.Bool("seed-demo", false, "개발·예시 확인용 초기 데이터 생성")
	headless := flag.Bool("headless", false, "GUI 창 없이 콘솔 데몬 모드로만 실행")
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

	rt := NewServerRuntime(ServerConfig{
		Port:     *port,
		SeedDemo: *seedDemo,
		Headless: *headless,
	})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("\n[시스템] 종료 신호를 수신했습니다. DB 및 백그라운드 프로세스를 안전하게 정리합니다...")
		rt.Shutdown()
		os.Exit(0)
	}()

	if *headless {
		defer func() {
			if r := recover(); r != nil {
				rt.Shutdown()
				writeServerErrorLog("예기치 못한 서버 내부 오류(Panic)가 발생했습니다", fmt.Errorf("%v", r))
				log.Printf("서버 패닉 발생: %v (로그: %s)", r, pathutil.SafeJoin(pathutil.BaseDir(), "server-error.log"))
				os.Exit(1)
			}
		}()
		if err := rt.Start(); err != nil {
			rt.Shutdown()
			log.Printf("[오류] 서버 실행 실패: %v (상세 로그: %s)", err, pathutil.SafeJoin(pathutil.BaseDir(), "server-error.log"))
			os.Exit(1)
		}
		return
	}

	runControlCenterGUI(rt)
}
