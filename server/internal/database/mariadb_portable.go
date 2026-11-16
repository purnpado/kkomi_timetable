package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"kkomi-timetable/pkg/pathutil"
)

// PortableMariaDBManager는 무설치 포터블 MariaDB(mariadbd.exe) 자식 프로세스의
// 초기화, AES-256 TDE 키 설정, 로컬 루프백 기동 및 안전 종료를 제어합니다.
type PortableMariaDBManager struct {
	runtimeDir string
	dataDir    string
	binPath    string
	initPath   string
	tdeKeyFile string
	port       int
	cmd        *exec.Cmd
	running    bool
}

// NewPortableMariaDBManager는 실행 파일 기준 절대 경로를 사용하여 포터블 MariaDB 관리자를 생성합니다.
func NewPortableMariaDBManager(port int) (*PortableMariaDBManager, error) {
	rtDir, err := pathutil.RuntimeMariaDBDir()
	if err != nil {
		return nil, err
	}
	dataDir, err := pathutil.EnsureDir(pathutil.SafeJoin(rtDir, "data"))
	if err != nil {
		return nil, err
	}
	binName := "mariadbd"
	initName := "mariadb-install-db"
	if runtime.GOOS == "windows" {
		binName = "mariadbd.exe"
		initName = "mariadb-install-db.exe"
	}

	binPath := pathutil.SafeJoin(rtDir, "bin", binName)
	if _, statErr := os.Stat(binPath); statErr != nil && runtime.GOOS == "windows" {
		alt := pathutil.SafeJoin(rtDir, "bin", "mysqld.exe")
		if _, altErr := os.Stat(alt); altErr == nil {
			binPath = alt
		}
	}

	return &PortableMariaDBManager{
		runtimeDir: rtDir,
		dataDir:    dataDir,
		binPath:    binPath,
		initPath:   pathutil.SafeJoin(rtDir, "bin", initName),
		tdeKeyFile: pathutil.SafeJoin(rtDir, "tde-keys.txt"),
		port:       port,
	}, nil
}

// HasBinary는 runtime/mariadb/bin 폴더에 포터블 MariaDB 실행 파일이 배치되어 있는지 반환합니다.
func (m *PortableMariaDBManager) HasBinary() bool {
	info, err := os.Stat(m.binPath)
	return err == nil && !info.IsDir()
}

// ensureTDEKeyFile은 MariaDB file_key_management(AES-256-CBC)용 암호화 키 파일을 생성합니다.
func (m *PortableMariaDBManager) ensureTDEKeyFile() error {
	if _, err := os.Stat(m.tdeKeyFile); err == nil {
		return nil
	}
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return fmt.Errorf("TDE 키 생성 실패: %w", err)
	}
	line := fmt.Sprintf("1;%s\n", hex.EncodeToString(rawKey))
	return os.WriteFile(m.tdeKeyFile, []byte(line), 0o600)
}

// ensureDataInitialized는 최초 실행 시 MariaDB 시스템 데이터베이스를 초기화합니다.
func (m *PortableMariaDBManager) ensureDataInitialized() error {
	mysqlSysDir := pathutil.SafeJoin(m.dataDir, "mysql")
	if _, err := os.Stat(mysqlSysDir); err == nil {
		return nil
	}
	if _, err := os.Stat(m.initPath); err != nil {
		// mariadb-install-db가 없으면 --initialize-insecure 시도 (배포판에 따라 다름)
		return fmt.Errorf("초기화 바이너리(%s)가 없어 기본 데이터 디렉토리 초기화 불가", filepath.Base(m.initPath))
	}
	cmd := exec.Command(m.initPath, fmt.Sprintf("--datadir=%s", m.dataDir))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("MariaDB 데이터 디렉토리 초기화 실패: %w (%s)", err, string(out))
	}
	return nil
}

// Start는 포터블 MariaDB를 127.0.0.1 로컬 전용으로 기동하고 포트 준비를 대기합니다.
func (m *PortableMariaDBManager) Start() (string, error) {
	if !m.HasBinary() {
		return "", fmt.Errorf("포터블 MariaDB 바이너리(%s) 미배치", m.binPath)
	}
	if err := m.ensureTDEKeyFile(); err != nil {
		return "", err
	}
	if err := m.ensureDataInitialized(); err != nil {
		log.Printf("[경고] MariaDB 자동 초기화 스킵: %v", err)
	}

	args := []string{
		"--no-defaults",
		fmt.Sprintf("--basedir=%s", m.runtimeDir),
		fmt.Sprintf("--datadir=%s", m.dataDir),
		"--bind-address=127.0.0.1",
		fmt.Sprintf("--port=%d", m.port),
		"--character-set-server=utf8mb4",
		"--collation-server=utf8mb4_unicode_ci",
		"--innodb-encrypt-tables=ON",
		"--innodb-encrypt-log=ON",
		"--plugin-load-add=file_key_management",
		fmt.Sprintf("--file-key-management-filename=%s", m.tdeKeyFile),
		"--file-key-management-encryption-algorithm=AES_CTR",
		"--skip-networking=0",
	}

	m.cmd = exec.Command(m.binPath, args...)
	if err := m.cmd.Start(); err != nil {
		return "", fmt.Errorf("mariadbd 실행 실패: %w", err)
	}
	m.running = true

	// 최대 10초 동안 127.0.0.1:port 리스닝 대기
	addr := fmt.Sprintf("127.0.0.1:%d", m.port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			dsn := fmt.Sprintf("root@tcp(%s)/kkomi_timetable?charset=utf8mb4&parseTime=true", addr)
			return dsn, nil
		}
		time.Sleep(250 * time.Millisecond)
	}

	_ = m.Stop()
	return "", fmt.Errorf("포터블 MariaDB 포트(%d) 응답 타임아웃", m.port)
}

// Stop은 실행 중인 포터블 MariaDB 자식 프로세스를 안전하게 종료합니다.
func (m *PortableMariaDBManager) Stop() error {
	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		_ = m.cmd.Process.Kill()
	} else {
		_ = m.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- m.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = m.cmd.Process.Kill()
		}
	}
	m.running = false
	log.Println("[DB] 포터블 MariaDB 프로세스 안전 종료 완료")
	return nil
}
