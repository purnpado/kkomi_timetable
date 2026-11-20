package main

import (
	"bufio"
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func failAndPause(shutdownFn func(), title string, err error, hints ...string) {
	if shutdownFn != nil {
		shutdownFn()
	}
	logPath := pathutil.SafeJoin(pathutil.BaseDir(), "server-error.log")
	stamp := time.Now().Format("2006-01-02 15:04:05")
	content := fmt.Sprintf("[%s] %s\n원인: %v\n", stamp, title, err)
	for _, h := range hints {
		content += fmt.Sprintf(" - %s\n", h)
	}
	content += "------------------------------------------------------------\n"
	if f, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); openErr == nil {
		_, _ = f.WriteString(content)
		_ = f.Close()
	}

	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("❌ [서버 실행 실패] %s\n", title)
	fmt.Printf("🔎 상세 원인: %v\n", err)
	if len(hints) > 0 {
		fmt.Println("💡 확인 및 해결 방법:")
		for _, h := range hints {
			fmt.Printf("   %s\n", h)
		}
	}
	fmt.Printf("📝 오류 로그 저장 위치: %s\n", logPath)
	fmt.Println("============================================================")
	fmt.Print("창을 닫거나 종료하려면 [Enter] 키를 누르세요...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	os.Exit(1)
}

func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// relaunchInDarwinTerminal은 Finder 등에서 꼬미시간서버.app을 더블클릭했을 때
// 터미널 창 없이 백그라운드 종료되는 문제를 막기 위해 Terminal.app에서 서버를 실행합니다.
func relaunchInDarwinTerminal(port int) bool {
	if runtime.GOOS != "darwin" || os.Getenv("KKOMI_IN_TERMINAL") == "1" {
		return false
	}
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, rErr := filepath.EvalSymlinks(exePath); rErr == nil && resolved != "" {
		exePath = resolved
	}
	if !strings.Contains(exePath, filepath.Join(".app", "Contents", "MacOS")) {
		return false
	}

	targetBin := exePath
	if sibling := pathutil.SafeJoin(pathutil.ExecutableDir(), "꼬미시간서버"); sibling != "" {
		if info, sErr := os.Stat(sibling); sErr == nil && !info.IsDir() {
			targetBin = sibling
		}
	}

	scriptPath := filepath.Join(os.TempDir(), "kkomi-server-launch.command")
	script := fmt.Sprintf("#!/usr/bin/env bash\nexport KKOMI_IN_TERMINAL=1\nclear\n%s --port %d\n", shellQuoteSingle(targetBin), port)
	if wErr := os.WriteFile(scriptPath, []byte(script), 0o755); wErr != nil {
		log.Printf("[시스템] Terminal 실행 스크립트 생성 실패: %v", wErr)
		return false
	}
	if runErr := exec.Command("open", "-a", "Terminal", scriptPath).Run(); runErr != nil {
		log.Printf("[시스템] Terminal.app 자동 실행 실패(백그라운드 계속 시도): %v", runErr)
		return false
	}
	return true
}

func dbErrorHints() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"1) 배포 압축 파일(꼬미시간_v..._windows_portable.zip)의 전체 폴더를 압축 해제한 상태에서 실행했는지 확인하세요.",
			"2) 실행 폴더 내 'runtime\\mariadb\\bin\\mariadbd.exe' 및 'mysqld.exe'가 모두 존재하는지 확인하세요.",
			"3) Windows Defender·백신 프로그램이나 이전 실행 프로세스(mariadbd.exe)가 DB 파일/포트를 막고 있지 않은지 확인하세요.",
		}
	}
	return []string{
		"1) 실행 폴더 내 'runtime/mariadb/bin/mariadbd' 및 'data' 폴더가 정상 존재하는지 확인하세요.",
		"2) 이미 실행 중인 '꼬미시간서버' 또는 백그라운드 'mariadbd' 프로세스가 DB 파일/포트(13306)를 점유하고 있는지 확인하세요.",
		"3) 이전 실행이 비정상 종료되었다면 잠시 후 다시 실행해 주세요(좀비 DB 프로세스 자동 정리됨).",
	}
}

func portErrorHints() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"1) 이미 다른 '꼬미시간서버.exe' 창이 실행 중인지 작업 관리자에서 확인하세요.",
			"2) Windows 방화벽 또는 다른 프로그램이 해당 포트를 점유하고 있을 수 있습니다.",
		}
	}
	return []string{
		"1) 이미 다른 '꼬미시간서버' 창이 실행 중인지(또는 백그라운드 프로세스) 확인하세요.",
		"2) 다른 프로그램이 해당 포트를 사용 중이면 '--port <번호>' 옵션으로 실행하세요.",
	}
}

func main() {
	port := flag.Int("port", 18090, "업무망 LAN HTTP/WebSocket 서버 포트")
	initTemplate := flag.Bool("init-template", false, "기본 .hwpx 템플릿 파일만 생성하고 종료")
	seedDemo := flag.Bool("seed-demo", false, "개발·예시 확인용 초기 데이터 생성")
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

	if relaunchInDarwinTerminal(*port) {
		os.Exit(0)
	}

	// DB 기동 중 인터럽트(Ctrl+C/SIGTERM) 발생 시에도 고아 mariadbd가 남지 않도록 선행 등록
	var (
		resMu        sync.Mutex
		httpServer   *http.Server
		store        *database.Store
		serverApp    *api.ServerApp
		cleaned      atomic.Bool
		shutdownDone = make(chan struct{})
	)
	shutdownFn := func() {
		if !cleaned.CompareAndSwap(false, true) {
			return
		}
		defer close(shutdownDone)
		resMu.Lock()
		appRef, srvRef, storeRef := serverApp, httpServer, store
		resMu.Unlock()

		if appRef != nil {
			appRef.InternalExam.StopScheduler()
		}
		if srvRef != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = srvRef.Shutdown(ctx)
		}
		if storeRef != nil {
			_ = storeRef.Close()
		} else {
			database.StopActiveDaemon()
		}
	}

	defer func() {
		if r := recover(); r != nil {
			failAndPause(shutdownFn, "예기치 못한 서버 내부 오류(Panic)가 발생했습니다", fmt.Errorf("%v", r),
				"1) server-error.log 파일 내용을 확인해 주세요.",
				"2) 백신·보안 프로그램이 실행 파일이나 DB 프로세스를 차단하지 않았는지 확인해 주세요.")
		}
	}()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("\n[시스템] 종료 신호를 수신했습니다. DB 및 백그라운드 프로세스를 안전하게 정리합니다...")
		shutdownFn()
		os.Exit(0)
	}()

	pathutil.CleanupOldBinaries()

	log.Println("============================================================")
	log.Printf("🐾 꼬미시간 통합 학사·시험감독 서버 v%s (CMD 콘솔 서버)", ws.ServerVersion)
	log.Printf("📁 실행 기준 절대경로: %s", pathutil.BaseDir())

	openedStore, err := database.Open()
	if err != nil {
		database.StopActiveDaemon()
		failAndPause(shutdownFn, "포터블 데이터베이스(MariaDB) 초기화 또는 실행에 실패했습니다", err, dbErrorHints()...)
	}
	resMu.Lock()
	store = openedStore
	resMu.Unlock()

	if *seedDemo {
		log.Println("ℹ️  --seed-demo 옵션으로 예시 데이터를 초기화합니다.")
		if err := database.SeedIfEmpty(store); err != nil {
			log.Printf("[경고] 초기 예시 데이터 생성 중 오류: %v", err)
		}
	} else {
		if err := database.EnsureCleanInit(store); err != nil {
			log.Printf("[경고] 시스템 초기 계정/설정 준비 중 오류: %v", err)
		}
	}
	if tplPath, err := exporter.EnsureDefaultHWPXTemplate(); err == nil {
		log.Printf("📄 한글(.hwpx) 기본 템플릿 준비 완료: %s", tplPath)
	}

	createdApp := api.NewServerApp(store, shutdownFn)
	resMu.Lock()
	serverApp = createdApp
	resMu.Unlock()

	webSubFS, err := fs.Sub(embeddedWebFS, "webdist")
	if err != nil {
		failAndPause(shutdownFn, "내장 웹 관리 화면 에셋을 불러오지 못했습니다", err,
			"배포 파일이 손상되었을 수 있습니다. 최신 릴리즈를 다시 내려받아 실행해 주세요.")
	}

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		failAndPause(shutdownFn, fmt.Sprintf("서버 포트(%d)를 열 수 없습니다", *port), err, portErrorHints()...)
	}

	srv := &http.Server{
		Handler:           createdApp.Handler(webSubFS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	resMu.Lock()
	httpServer = srv
	resMu.Unlock()

	lanIP := ws.LocalIPAddress()
	log.Println("[🟢 서버 상태: 켜짐 (정상 작동 중)]")
	log.Printf("🗄️  DB 구동 모드: %s", store.ModeLabel())
	log.Printf("🌐 업무망 고정 IP 웹 접속: http://%s:%d", lanIP, *port)
	log.Printf("🔌 교사용 클라이언트(꼬미시간.exe) 서버 주소 입력: http://%s:%d", lanIP, *port)
	log.Println("💡 교사 명렬표·학년별/학급별 기초 시간표 작성은 클라이언트(마스터/담당 권한)에서 수행하세요.")
	log.Println("[🛑 서버 종료 방법: 이 창에서 Ctrl+C를 누르거나 창을 닫으세요]")
	log.Println("============================================================")

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		failAndPause(shutdownFn, "HTTP/WebSocket 서버 실행 중 오류가 발생했습니다", err,
			"네트워크 어댑터 상태 및 보안 프로그램의 포트 차단 여부를 확인해 주세요.")
	}
	<-shutdownDone
}
