package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/server/internal/api"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/exporter"
	"kkomi-timetable/server/internal/ws"
)

//go:embed all:webdist
var embeddedWebFS embed.FS

// ServerConfig는 꼬미시간 통합 서버 실행 설정입니다.
type ServerConfig struct {
	Port     int
	SeedDemo bool
	Headless bool
}

// ServerStatusSnapshot은 GUI 컨트롤 센터에 전달할 실시간 서버 상태 스냅샷입니다.
type ServerStatusSnapshot struct {
	State          string   `json:"state"`
	StateLabel     string   `json:"stateLabel"`
	ProgressPct    int      `json:"progressPct"`
	ElapsedSec     int      `json:"elapsedSec"`
	Version        string   `json:"version"`
	Port           int      `json:"port"`
	LanURL         string   `json:"lanUrl"`
	LocalURL       string   `json:"localUrl"`
	DBMode         string   `json:"dbMode"`
	BaseDir        string   `json:"baseDir"`
	ConnectedCount int      `json:"connectedCount"`
	BackupCount    int      `json:"backupCount"`
	LastBackupFile string   `json:"lastBackupFile,omitempty"`
	LastError      string   `json:"lastError,omitempty"`
	ErrorHints     []string `json:"errorHints,omitempty"`
	Logs           []string `json:"logs"`
}

type logRingBuffer struct {
	mu      sync.Mutex
	lines   []string
	partial string
	max     int
}

func (b *logRingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		b.max = 200
	}
	chunk := b.partial + string(p)
	parts := strings.Split(chunk, "\n")
	b.partial = parts[len(parts)-1]
	for _, ln := range parts[:len(parts)-1] {
		trimmed := strings.TrimRight(ln, "\r")
		if trimmed == "" {
			continue
		}
		b.lines = append(b.lines, trimmed)
	}
	if len(b.lines) > b.max {
		b.lines = append([]string(nil), b.lines[len(b.lines)-b.max:]...)
	}
	return len(p), nil
}

func (b *logRingBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines), len(b.lines)+1)
	copy(out, b.lines)
	if strings.TrimSpace(b.partial) != "" {
		out = append(out, strings.TrimRight(b.partial, "\r"))
	}
	return out
}

// ServerRuntime은 포터블 MariaDB, HTTP/WS 서버 생명주기 및 컨트롤 센터 상태·제어를 관리합니다.
type ServerRuntime struct {
	cfg          ServerConfig
	startedAt    time.Time
	logBuf       *logRingBuffer
	mu           sync.RWMutex
	state        string
	stateLabel   string
	progressPct  int
	lastError    string
	errorHints   []string
	lastBackup   string
	httpServer   *http.Server
	store        *database.Store
	serverApp    *api.ServerApp
	cleaned      atomic.Bool
	shutdownDone chan struct{}
}

// NewServerRuntime은 서버 런타임 제어기를 생성하고 로그 수집기를 표준 출력에 연결합니다.
func NewServerRuntime(cfg ServerConfig) *ServerRuntime {
	if cfg.Port <= 0 {
		cfg.Port = 18090
	}
	buf := &logRingBuffer{max: 200}
	log.SetOutput(io.MultiWriter(os.Stderr, buf))
	return &ServerRuntime{
		cfg:          cfg,
		startedAt:    time.Now(),
		logBuf:       buf,
		state:        "INIT_DB",
		stateLabel:   "포터블 MariaDB 초기화 준비 중",
		progressPct:  10,
		shutdownDone: make(chan struct{}),
	}
}

func (rt *ServerRuntime) setPhase(state, label string, pct int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.state = state
	rt.stateLabel = label
	rt.progressPct = pct
}

func (rt *ServerRuntime) setError(title string, err error, hints ...string) {
	if err != nil {
		writeServerErrorLog(title, err, hints...)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.state = "ERROR"
	rt.stateLabel = title
	if err != nil {
		rt.lastError = err.Error()
	}
	rt.errorHints = hints
}

// StartAsync는 백그라운드 고루틴에서 서버를 비동기 기동합니다.
func (rt *ServerRuntime) StartAsync() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				rt.setError("예기치 못한 서버 내부 오류(Panic)가 발생했습니다", fmt.Errorf("%v", r),
					"1) server-error.log 파일 내용을 확인해 주세요.",
					"2) 백신·보안 프로그램이 실행 파일이나 DB 프로세스를 차단하지 않았는지 확인해 주세요.")
			}
		}()
		_ = rt.Start()
	}()
}

// Start는 포터블 MariaDB 구동, 초기 데이터 구성, HTTP/WebSocket 서버 실행까지 블로킹 수행합니다.
func (rt *ServerRuntime) Start() error {
	pathutil.CleanupOldBinaries()
	log.Println("============================================================")
	log.Printf("꼬미시간 통합 학사·시험감독 서버 v%s", ws.ServerVersion)
	log.Printf("실행 기준 절대경로: %s", pathutil.BaseDir())

	rt.setPhase("INIT_DB", "포터블 MariaDB 초기화 및 AES-256 TDE 구동 중", 25)
	openedStore, err := database.Open()
	if err != nil {
		database.StopActiveDaemon()
		rt.setError("포터블 데이터베이스(MariaDB) 초기화 또는 실행에 실패했습니다", err, dbErrorHints()...)
		return err
	}
	rt.mu.Lock()
	rt.store = openedStore
	rt.mu.Unlock()

	if rt.cleaned.Load() {
		_ = openedStore.Close()
		return nil
	}

	rt.setPhase("INIT_DATA", "초기 계정 및 한글(.hwpx) 기본 템플릿 준비 중", 65)
	if rt.cfg.SeedDemo {
		log.Println("--seed-demo 옵션으로 예시 데이터를 초기화합니다.")
		if sErr := database.SeedIfEmpty(openedStore); sErr != nil {
			log.Printf("[경고] 초기 예시 데이터 생성 중 오류: %v", sErr)
		}
	} else {
		if cErr := database.EnsureCleanInit(openedStore); cErr != nil {
			log.Printf("[경고] 시스템 초기 계정/설정 준비 중 오류: %v", cErr)
		}
	}
	if tplPath, tErr := exporter.EnsureDefaultHWPXTemplate(); tErr == nil {
		log.Printf("한글(.hwpx) 기본 템플릿 준비 완료: %s", tplPath)
	}

	createdApp := api.NewServerApp(openedStore, rt.Shutdown)
	rt.mu.Lock()
	rt.serverApp = createdApp
	rt.mu.Unlock()

	webSubFS, err := fs.Sub(embeddedWebFS, "webdist")
	if err != nil {
		rt.setError("내장 웹 관리 화면 에셋을 불러오지 못했습니다", err,
			"배포 파일이 손상되었을 수 있습니다. 최신 릴리즈를 다시 내려받아 실행해 주세요.")
		return err
	}

	rt.setPhase("START_HTTP", "LAN HTTP/WebSocket 포트 바인딩 중", 85)
	addr := fmt.Sprintf("0.0.0.0:%d", rt.cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		rt.setError(fmt.Sprintf("서버 포트(%d)를 열 수 없습니다", rt.cfg.Port), err, portErrorHints()...)
		return err
	}

	srv := &http.Server{
		Handler:           createdApp.Handler(webSubFS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	rt.mu.Lock()
	rt.httpServer = srv
	rt.mu.Unlock()

	lanIP := ws.LocalIPAddress()
	rt.setPhase("RUNNING", "정상 작동 중", 100)
	log.Printf("[서버 상태: 켜짐] DB 구동 모드: %s", openedStore.ModeLabel())
	log.Printf("업무망 LAN 접속 주소: http://%s:%d", lanIP, rt.cfg.Port)

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		rt.setError("HTTP/WebSocket 서버 실행 중 오류가 발생했습니다", err,
			"네트워크 어댑터 상태 및 보안 프로그램의 포트 차단 여부를 확인해 주세요.")
		return err
	}
	<-rt.shutdownDone
	return nil
}

// Shutdown은 HTTP 서버와 포터블 MariaDB 데몬을 안전하게 종료합니다.
func (rt *ServerRuntime) Shutdown() {
	if !rt.cleaned.CompareAndSwap(false, true) {
		return
	}
	defer close(rt.shutdownDone)
	rt.setPhase("STOPPING", "서버 및 데이터베이스 안전 종료 중", 100)

	rt.mu.RLock()
	appRef, srvRef, storeRef := rt.serverApp, rt.httpServer, rt.store
	rt.mu.RUnlock()

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

// Status는 현재 서버 초기화·실행 상태, 접속 교사 수, 백업 현황 및 최근 로그 스냅샷을 반환합니다.
func (rt *ServerRuntime) Status() ServerStatusSnapshot {
	rt.mu.RLock()
	state := rt.state
	label := rt.stateLabel
	pct := rt.progressPct
	lastErr := rt.lastError
	hints := append([]string(nil), rt.errorHints...)
	lastBak := rt.lastBackup
	appRef := rt.serverApp
	storeRef := rt.store
	rt.mu.RUnlock()

	lanIP := ws.LocalIPAddress()
	connCount := 0
	if appRef != nil && appRef.Hub != nil {
		connCount = appRef.Hub.ConnectedCount()
	}
	backupCount := 0
	dbMode := "포터블 MariaDB 초기화 중"
	if storeRef != nil {
		dbMode = storeRef.ModeLabel()
		if list, err := storeRef.ListBackups(); err == nil {
			backupCount = len(list)
		}
	}
	return ServerStatusSnapshot{
		State:          state,
		StateLabel:     label,
		ProgressPct:    pct,
		ElapsedSec:     int(time.Since(rt.startedAt).Seconds()),
		Version:        ws.ServerVersion,
		Port:           rt.cfg.Port,
		LanURL:         fmt.Sprintf("http://%s:%d", lanIP, rt.cfg.Port),
		LocalURL:       fmt.Sprintf("http://127.0.0.1:%d", rt.cfg.Port),
		DBMode:         dbMode,
		BaseDir:        pathutil.BaseDir(),
		ConnectedCount: connCount,
		BackupCount:    backupCount,
		LastBackupFile: lastBak,
		LastError:      lastErr,
		ErrorHints:     hints,
		Logs:           rt.logBuf.Lines(),
	}
}

// CreateBackup은 즉시 AES-256 암호화 백업을 생성하고 파일명을 반환합니다.
func (rt *ServerRuntime) CreateBackup() (string, error) {
	rt.mu.RLock()
	storeRef := rt.store
	rt.mu.RUnlock()
	if storeRef == nil {
		return "", fmt.Errorf("데이터베이스 초기화가 아직 완료되지 않았습니다")
	}
	fullPath, err := storeRef.ExportEncryptedBackup("gui-manual")
	if err != nil {
		return "", err
	}
	name := filepath.Base(fullPath)
	rt.mu.Lock()
	rt.lastBackup = name
	rt.mu.Unlock()
	log.Printf("[백업] 컨트롤 센터 즉시 암호화 백업 생성: %s", name)
	return name, nil
}

// OpenBaseFolder는 서버 실행 기준 절대 경로 폴더를 OS 파일 탐색기로 엽니다.
func (rt *ServerRuntime) OpenBaseFolder() error {
	return openOSPath(pathutil.BaseDir())
}

// OpenWebAdmin은 로컬 웹 관리 주소를 기본 브라우저로 엽니다.
func (rt *ServerRuntime) OpenWebAdmin() error {
	return openOSPath(fmt.Sprintf("http://127.0.0.1:%d", rt.cfg.Port))
}

func openOSPath(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

func writeServerErrorLog(title string, err error, hints ...string) {
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
		"1) 이미 다른 '꼬미시간서버'가 실행 중인지 확인하세요.",
		"2) 다른 프로그램이 해당 포트를 사용 중이면 '--port <번호>' 옵션으로 실행하세요.",
	}
}
