package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// SafeJoin은 한글 폴더명과 공백이 포함된 경로에서도 안전하게 경로를 결합합니다.
// UTF-8 NFC 정규화를 적용하여 macOS(NFD)와 Windows(NFC) 간 경로 불일치를 방지합니다.
func SafeJoin(base string, elems ...string) string {
	normalized := make([]string, 0, len(elems)+1)
	normalized = append(normalized, norm.NFC.String(filepath.Clean(base)))
	for _, elem := range elems {
		cleaned := norm.NFC.String(strings.TrimSpace(elem))
		if cleaned != "" {
			normalized = append(normalized, cleaned)
		}
	}
	return filepath.Join(normalized...)
}

// ExecutableDir은 현재 실행 파일(꼬미시간서버.exe / 꼬미시간.exe 등)이 위치한 절대 디렉토리를 반환합니다.
// 상대경로(cwd)나 바로가기 실행 시에도 항상 실행 파일 기준의 경로를 보장합니다.
func ExecutableDir() string {
	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return "."
		}
		return norm.NFC.String(wd)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
		exePath = resolved
	}
	dir := filepath.Dir(exePath)

	// macOS .app 번들 내부(Contents/MacOS)에서 실행될 경우 .app 번들이 놓인 상위 폴더를 기준으로 사용
	if runtime.GOOS == "darwin" {
		suffix := filepath.Join(".app", "Contents", "MacOS")
		if idx := strings.Index(dir, suffix); idx != -1 {
			bundlePath := dir[:idx+4]
			return norm.NFC.String(filepath.Dir(bundlePath))
		}
	}
	return norm.NFC.String(dir)
}

// BaseDir은 프로그램 데이터 루트 디렉토리를 반환합니다.
func BaseDir() string {
	if override := strings.TrimSpace(os.Getenv("KKOMI_BASE_DIR")); override != "" {
		return norm.NFC.String(filepath.Clean(override))
	}
	return ExecutableDir()
}

// EnsureDir은 지정된 절대 경로 디렉토리가 존재하지 않으면 생성합니다.
func EnsureDir(dir string) (string, error) {
	clean := norm.NFC.String(filepath.Clean(dir))
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", err
	}
	return clean, nil
}

// DataDir은 DB 및 암호화 키가 저장되는 data 디렉토리의 절대 경로를 반환합니다.
func DataDir() (string, error) {
	return EnsureDir(SafeJoin(BaseDir(), "data"))
}

// BackupDir은 자동 백업본이 저장되는 backups 디렉토리의 절대 경로를 반환합니다.
func BackupDir() (string, error) {
	return EnsureDir(SafeJoin(BaseDir(), "backups"))
}

// RuntimeMariaDBDir은 포터블 MariaDB 바이너리와 데이터가 위치하는 runtime/mariadb 경로를 반환합니다.
func RuntimeMariaDBDir() (string, error) {
	return EnsureDir(SafeJoin(BaseDir(), "runtime", "mariadb"))
}

// TemplateDir은 사용자 정의 .hwpx 템플릿 파일이 위치하는 templates 디렉토리를 반환합니다.
func TemplateDir() (string, error) {
	return EnsureDir(SafeJoin(BaseDir(), "templates"))
}

// UpdateDir은 원격 무중단 자가 업데이트용 임시 파일 디렉토리를 반환합니다.
func UpdateDir() (string, error) {
	return EnsureDir(SafeJoin(BaseDir(), "updates"))
}

// CleanupOldBinaries는 자가 업데이트 직후 남은 .old 백업 바이너리를 정리합니다.
func CleanupOldBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}
