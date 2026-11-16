package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"kkomi-timetable/pkg/pathutil"
)

const DefaultMariaDBPort = 13306

// PortableMariaDBManager는 무설치 포터블 MariaDB(mariadbd / mariadbd.exe) 자식 프로세스의
// 런타임 탐색, 초기화, AES-256 TDE 키 설정, 로컬 루프백 기동 및 안전 종료를 제어합니다.
type PortableMariaDBManager struct {
	runtimeDir string
	dataDir    string
	binPath    string
	initPath   string
	tdeKeyFile string
	shareDir   string
	pluginDir  string
	hasTDE     bool
	port       int
	socketPath string
	pidFile    string
	cmd        *exec.Cmd
	waitCh     chan error
	running    bool
	reused     bool
}

var (
	testDaemonMu   sync.Mutex
	testDaemonMgr  *PortableMariaDBManager
	testTempRoot   string
	activeMu       sync.Mutex
	activeMgr      *PortableMariaDBManager
	isRunningTests = func() bool { return flag.Lookup("test.v") != nil }
)

func resolveRuntimeDir() (string, error) {
	binName := "mariadbd"
	if runtime.GOOS == "windows" {
		binName = "mariadbd.exe"
	}

	candidates := []string{
		pathutil.SafeJoin(pathutil.BaseDir(), "runtime", "mariadb"),
		pathutil.SafeJoin(pathutil.ExecutableDir(), "runtime", "mariadb"),
	}
	for _, start := range []string{pathutil.ExecutableDir(), mustGetwd()} {
		cur := start
		for i := 0; i < 8 && cur != "" && cur != string(filepath.Separator); i++ {
			candidates = append(candidates,
				pathutil.SafeJoin(cur, "runtime", "mariadb"),
				pathutil.SafeJoin(cur, "bin", runtime.GOOS, "runtime", "mariadb"),
			)
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	for _, dir := range candidates {
		if info, err := os.Stat(pathutil.SafeJoin(dir, "bin", binName)); err == nil && !info.IsDir() {
			return dir, nil
		}
	}
	if extracted := tryExtractPortableRuntimeFromZip(binName); extracted != "" {
		return extracted, nil
	}
	return "", fmt.Errorf("포터블 MariaDB 런타임(%s)을 찾을 수 없습니다 (압축 파일 내 1_교무실_공용서버/runtime/mariadb 폴더가 함께 있어야 합니다)", binName)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func newManagerWithPaths(rtDir, dataRoot string, port int) (*PortableMariaDBManager, error) {
	dataDir, err := pathutil.EnsureDir(pathutil.SafeJoin(dataRoot, "data"))
	if err != nil {
		return nil, err
	}

	binName, initName, pluginName := "mariadbd", "mariadb-install-db", "file_key_management.so"
	if runtime.GOOS == "windows" {
		binName, initName, pluginName = "mariadbd.exe", "mariadb-install-db.exe", "file_key_management.dll"
	}

	shareDir := pathutil.SafeJoin(rtDir, "share", "mysql")
	if _, err := os.Stat(pathutil.SafeJoin(shareDir, "english", "errmsg.sys")); err != nil {
		shareDir = pathutil.SafeJoin(rtDir, "share")
	}
	pluginDir := pathutil.SafeJoin(rtDir, "lib", "plugin")
	_, tdeErr := os.Stat(pathutil.SafeJoin(pluginDir, pluginName))

	mgr := &PortableMariaDBManager{
		runtimeDir: rtDir,
		dataDir:    dataDir,
		binPath:    pathutil.SafeJoin(rtDir, "bin", binName),
		initPath:   pathutil.SafeJoin(rtDir, "bin", initName),
		tdeKeyFile: pathutil.SafeJoin(dataRoot, "tde-keys.txt"),
		shareDir:   shareDir,
		pluginDir:  pluginDir,
		hasTDE:     tdeErr == nil,
	}
	mgr.setPort(port)
	return mgr, nil
}

func (m *PortableMariaDBManager) setPort(port int) {
	m.port = port
	m.socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("kkomi-mariadb-%d.sock", port))
	m.pidFile = pathutil.SafeJoin(m.dataDir, fmt.Sprintf("mariadbd-%d.pid", port))
}

// NewPortableMariaDBManager는 실행 환경에 맞는 포터블 MariaDB 관리자를 생성합니다.
func NewPortableMariaDBManager(port int) (*PortableMariaDBManager, error) {
	rtDir, err := resolveRuntimeDir()
	if err != nil {
		return nil, err
	}
	if port <= 0 {
		port = DefaultMariaDBPort
		if envPort := strings.TrimSpace(os.Getenv("KKOMI_DB_PORT")); envPort != "" {
			if parsed, convErr := strconv.Atoi(envPort); convErr == nil && parsed > 0 {
				port = parsed
			}
		}
	}
	dataRoot, err := pathutil.RuntimeMariaDBDir()
	if err != nil {
		return nil, err
	}
	return newManagerWithPaths(rtDir, dataRoot, port)
}

func acquireTestDaemon() (*PortableMariaDBManager, error) {
	testDaemonMu.Lock()
	defer testDaemonMu.Unlock()

	if testDaemonMgr != nil && testDaemonMgr.isPortOpen() {
		return testDaemonMgr, nil
	}
	rtDir, err := resolveRuntimeDir()
	if err != nil {
		return nil, err
	}
	port, err := pickFreeLocalPort()
	if err != nil {
		return nil, err
	}
	tmpRoot, err := os.MkdirTemp("", "kkomi-test-mariadb-*")
	if err != nil {
		return nil, err
	}
	mgr, err := newManagerWithPaths(rtDir, tmpRoot, port)
	if err != nil {
		_ = os.RemoveAll(tmpRoot)
		return nil, err
	}
	if _, err := mgr.Start(); err != nil {
		_ = os.RemoveAll(tmpRoot)
		return nil, err
	}
	testTempRoot = tmpRoot
	testDaemonMgr = mgr
	return mgr, nil
}

// StopTestDaemons는 테스트 종료 시 기동된 임시 포터블 MariaDB 데몬과 임시 폴더를 정리합니다.
func StopTestDaemons() {
	testDaemonMu.Lock()
	defer testDaemonMu.Unlock()

	if testDaemonMgr != nil {
		_ = testDaemonMgr.Stop()
		testDaemonMgr = nil
	}
	if testTempRoot != "" {
		_ = os.RemoveAll(testTempRoot)
		testTempRoot = ""
	}
}

// StopActiveDaemon은 서버 초기화 도중 시그널 종료 시 현재 기동 중인 포터블 MariaDB를 즉시 정리합니다.
func StopActiveDaemon() {
	activeMu.Lock()
	mgr := activeMgr
	activeMu.Unlock()
	if mgr != nil {
		_ = mgr.Stop()
	}
}

func pickFreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("임시 포트 할당 실패: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func canonicalDir(p string) string {
	clean := filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		clean = resolved
	}
	return strings.TrimRight(clean, string(filepath.Separator))
}

func (m *PortableMariaDBManager) isPortOpen() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", m.port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// inspectExistingPort는 포트가 이미 점유된 경우 동일 데이터 폴더 재사용·고아 데몬 정리·대체 포트 선택을 판별합니다.
func (m *PortableMariaDBManager) inspectExistingPort() (reuse bool) {
	if !m.isPortOpen() {
		return false
	}
	db, err := sql.Open("mysql", m.DSN(""))
	if err == nil {
		var runningDir string
		qErr := db.QueryRow("SELECT @@datadir").Scan(&runningDir)
		if qErr == nil && runningDir != "" {
			if canonicalDir(runningDir) == canonicalDir(m.dataDir) {
				_ = db.Close()
				return true
			}
			// 기존 데몬이 가리키는 데이터 폴더가 이미 삭제된 고아 데몬이면 SHUTDOWN으로 정리
			if _, statErr := os.Stat(canonicalDir(runningDir)); os.IsNotExist(statErr) {
				_, _ = db.Exec("SHUTDOWN")
				_ = db.Close()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if !m.isPortOpen() {
						return false
					}
					time.Sleep(150 * time.Millisecond)
				}
			}
		}
		_ = db.Close()
	}

	// 다른 유효한 인스턴스나 서비스가 점유 중이면 인접 빈 포트 자동 선택
	for cand := m.port + 1; cand <= m.port+30; cand++ {
		if ln, lErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cand)); lErr == nil {
			_ = ln.Close()
			m.setPort(cand)
			return false
		}
	}
	if freePort, fErr := pickFreeLocalPort(); fErr == nil {
		m.setPort(freePort)
	}
	return false
}

func (m *PortableMariaDBManager) ensureTDEKeyFile() error {
	if _, err := os.Stat(m.tdeKeyFile); err == nil {
		return nil
	}
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return fmt.Errorf("TDE 키 생성 실패: %w", err)
	}
	return os.WriteFile(m.tdeKeyFile, []byte(fmt.Sprintf("1;%s\n", hex.EncodeToString(rawKey))), 0o600)
}

func (m *PortableMariaDBManager) ensureDataInitialized() error {
	if _, err := os.Stat(pathutil.SafeJoin(m.dataDir, "mysql")); err == nil {
		return nil
	}
	if _, err := os.Stat(m.initPath); err != nil {
		return fmt.Errorf("초기화 바이너리(%s)를 찾을 수 없습니다", m.initPath)
	}
	if err := prepareCleanDataDir(m.dataDir); err != nil {
		return fmt.Errorf("MariaDB 데이터 폴더 준비 실패: %w", err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(m.initPath, fmt.Sprintf("--datadir=%s", m.dataDir))
	} else {
		cmd = exec.Command(
			m.initPath,
			"--no-defaults",
			fmt.Sprintf("--basedir=%s", m.runtimeDir),
			fmt.Sprintf("--datadir=%s", m.dataDir),
			"--auth-root-authentication-method=normal",
			"--skip-test-db",
			"--force",
		)
	}
	cmd.Dir = filepath.Dir(m.binPath)
	cmd.Env = append(os.Environ(), "PATH="+pathutil.SafeJoin(m.runtimeDir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("MariaDB 데이터 디렉토리 초기화 실패: %w (%s)", err, string(out))
	}
	return nil
}

// Start는 포터블 MariaDB를 127.0.0.1 로컬 전용으로 기동하고 포트 응답을 대기합니다.
func (m *PortableMariaDBManager) Start() (string, error) {
	if m.inspectExistingPort() {
		m.reused = true
		return m.DSN("kkomi_timetable"), nil
	}
	if err := m.ensureTDEKeyFile(); err != nil {
		return "", err
	}
	if err := m.ensureDataInitialized(); err != nil {
		return "", err
	}

	_ = os.Remove(m.socketPath)
	_ = os.Remove(m.pidFile)

	args := []string{
		"--no-defaults",
		fmt.Sprintf("--basedir=%s", formatMariaDBArgPath(m.runtimeDir)),
		fmt.Sprintf("--datadir=%s", formatMariaDBArgPath(m.dataDir)),
		fmt.Sprintf("--lc-messages-dir=%s", formatMariaDBArgPath(m.shareDir)),
		fmt.Sprintf("--plugin-dir=%s", formatMariaDBArgPath(m.pluginDir)),
		"--bind-address=127.0.0.1",
		fmt.Sprintf("--port=%d", m.port),
		fmt.Sprintf("--pid-file=%s", formatMariaDBArgPath(m.pidFile)),
		"--character-set-server=utf8mb4",
		"--collation-server=utf8mb4_unicode_ci",
		"--skip-grant-tables",
		"--skip-networking=0",
	}
	if runtime.GOOS != "windows" {
		args = append(args, fmt.Sprintf("--socket=%s", m.socketPath))
	}
	if m.hasTDE {
		args = append(args,
			"--plugin-load-add=file_key_management",
			fmt.Sprintf("--file-key-management-filename=%s", formatMariaDBArgPath(m.tdeKeyFile)),
			"--file-key-management-encryption-algorithm=AES_CTR",
			"--innodb-encrypt-tables=ON",
			"--innodb-encrypt-log=ON",
		)
	}

	logPath := pathutil.SafeJoin(m.dataDir, "mariadbd.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)

	m.cmd = exec.Command(m.binPath, args...)
	if logFile != nil {
		m.cmd.Stdout = logFile
		m.cmd.Stderr = logFile
	}
	if err := m.cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return "", fmt.Errorf("mariadbd 실행 실패: %w", err)
	}
	m.running = true
	m.waitCh = make(chan error, 1)
	go func() {
		err := m.cmd.Wait()
		if logFile != nil {
			_ = logFile.Close()
		}
		m.waitCh <- err
	}()

	if !isRunningTests() {
		activeMu.Lock()
		activeMgr = m
		activeMu.Unlock()
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case exitErr := <-m.waitCh:
			m.running = false
			return "", fmt.Errorf("mariadbd가 기동 중 조기 종료됨 (%v): %s", exitErr, readLogTail(logPath, 1200))
		default:
		}
		if m.isPortOpen() {
			return m.DSN("kkomi_timetable"), nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	_ = m.Stop()
	return "", fmt.Errorf("포터블 MariaDB 포트(%d) 응답 타임아웃 (%s)", m.port, readLogTail(logPath, 800))
}

// DSN은 지정된 데이터베이스 스키마에 연결할 MySQL/MariaDB DSN 문자열을 생성합니다.
func (m *PortableMariaDBManager) DSN(dbName string) string {
	return fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local&timeout=5s", m.port, dbName)
}

// Stop은 SQL SHUTDOWN으로 InnoDB를 정상 플러시한 뒤 실행 중인 포터블 MariaDB 프로세스를 안전하게 종료합니다.
func (m *PortableMariaDBManager) Stop() error {
	if m.reused || !m.running || m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	if m.isPortOpen() {
		if db, err := sql.Open("mysql", m.DSN("")); err == nil {
			_, _ = db.Exec("SHUTDOWN")
			_ = db.Close()
		}
	}

	exited := false
	select {
	case <-m.waitCh:
		exited = true
	case <-time.After(2500 * time.Millisecond):
	}

	if !exited {
		if runtime.GOOS == "windows" {
			_ = m.cmd.Process.Kill()
			<-m.waitCh
		} else {
			_ = m.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-m.waitCh:
			case <-time.After(3 * time.Second):
				_ = m.cmd.Process.Kill()
				<-m.waitCh
			}
		}
	}

	m.running = false
	activeMu.Lock()
	if activeMgr == m {
		activeMgr = nil
	}
	activeMu.Unlock()
	_ = os.Remove(m.socketPath)
	_ = os.Remove(m.pidFile)
	if !isRunningTests() {
		log.Println("[DB] 포터블 MariaDB 프로세스 안전 종료 완료")
	}
	return nil
}

func readLogTail(path string, maxBytes int) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	if len(raw) > maxBytes {
		raw = raw[len(raw)-maxBytes:]
	}
	return strings.TrimSpace(string(raw))
}
