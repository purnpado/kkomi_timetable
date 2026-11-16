package database

import (
	"archive/zip"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"kkomi-timetable/pkg/pathutil"
)

// formatMariaDBArgPath는 MariaDB 명령행 인자에서 백슬래시 이스케이프 오류를 막기 위해
// Windows 경로 구분자를 슬래시(/)로 정규화합니다.
func formatMariaDBArgPath(p string) string {
	clean := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(clean)
	}
	return clean
}

// prepareCleanDataDir는 최초 MariaDB 초기화(data/mysql 미존재) 직전에 남아 있는 임시 파일·폴더를
// 정리하여 Windows mariadb-install-db.exe의 "Data directory is not empty" 오류를 방지합니다.
func prepareCleanDataDir(dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dataDir, 0o755)
		}
		return err
	}
	for _, entry := range entries {
		if rmErr := os.RemoveAll(pathutil.SafeJoin(dataDir, entry.Name())); rmErr != nil {
			return rmErr
		}
	}
	return nil
}

// tryExtractPortableRuntimeFromZip은 실행 폴더 주변에 릴리즈 ZIP(portable/kkomi-timetable)이
// 함께 있을 때 runtime/mariadb 디렉터리를 자동으로 복원합니다.
func tryExtractPortableRuntimeFromZip(binName string) string {
	destRoot := pathutil.SafeJoin(pathutil.BaseDir(), "runtime", "mariadb")
	searchDirs := []string{pathutil.ExecutableDir(), pathutil.BaseDir()}
	if wd := mustGetwd(); wd != "" {
		searchDirs = append(searchDirs, wd, filepath.Dir(wd))
	}

	seen := make(map[string]struct{})
	for _, dir := range searchDirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !isPortableReleaseZip(entry.Name()) {
				continue
			}
			zipPath := pathutil.SafeJoin(dir, entry.Name())
			if _, ok := seen[zipPath]; ok {
				continue
			}
			seen[zipPath] = struct{}{}

			log.Printf("[DB] 포터블 런타임 폴더가 없어 릴리즈 압축 파일(%s)에서 MariaDB 런타임 자동 복원을 시도합니다...", entry.Name())
			if err := extractZipRuntimeArchive(zipPath, destRoot, binName); err != nil {
				log.Printf("[DB] 압축 파일 런타임 추출 중 건너뜀 (%s): %v", entry.Name(), err)
				continue
			}
			if info, statErr := os.Stat(pathutil.SafeJoin(destRoot, "bin", binName)); statErr == nil && !info.IsDir() {
				log.Printf("[DB] 포터블 MariaDB 런타임 자동 복원 완료: %s", destRoot)
				return destRoot
			}
		}
	}
	if prepared := tryRunDarwinRuntimePrepareScript(binName); prepared != "" {
		return prepared
	}
	return ""
}

func tryRunDarwinRuntimePrepareScript(binName string) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	starts := []string{pathutil.ExecutableDir(), pathutil.BaseDir(), mustGetwd()}
	seenScripts := make(map[string]struct{})
	for _, start := range starts {
		cur := start
		for i := 0; i < 6 && cur != "" && cur != string(filepath.Separator); i++ {
			scriptPath := pathutil.SafeJoin(cur, "scripts", "prepare_mariadb_runtime.sh")
			if _, seen := seenScripts[scriptPath]; !seen {
				seenScripts[scriptPath] = struct{}{}
				if info, err := os.Stat(scriptPath); err == nil && !info.IsDir() {
					log.Printf("[DB] macOS 포터블 MariaDB 런타임이 없어 %s 실행으로 자동 복원을 시도합니다...", scriptPath)
					cmd := exec.Command("bash", scriptPath)
					cmd.Dir = cur
					if out, runErr := cmd.CombinedOutput(); runErr != nil {
						log.Printf("[DB] macOS 런타임 자동 복원 실패: %v (%s)", runErr, strings.TrimSpace(string(out)))
					} else {
						candidates := []string{
							pathutil.SafeJoin(cur, "bin", "darwin", "runtime", "mariadb"),
							pathutil.SafeJoin(pathutil.BaseDir(), "runtime", "mariadb"),
						}
						for _, cand := range candidates {
							if st, stErr := os.Stat(pathutil.SafeJoin(cand, "bin", binName)); stErr == nil && !st.IsDir() {
								log.Printf("[DB] macOS 포터블 MariaDB 런타임 자동 복원 완료: %s", cand)
								return cand
							}
						}
					}
				}
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	return ""
}

func isPortableReleaseZip(name string) bool {
	low := strings.ToLower(name)
	if !strings.HasSuffix(low, ".zip") {
		return false
	}
	return strings.Contains(low, "portable") || strings.Contains(low, "kkomi-timetable") || strings.Contains(name, "꼬미시간")
}

func extractZipRuntimeArchive(zipPath, destRoot, binName string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	const marker = "runtime/mariadb/"
	extracted := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		norm := filepath.ToSlash(f.Name)
		idx := strings.Index(strings.ToLower(norm), marker)
		if idx < 0 {
			continue
		}
		rel := strings.TrimPrefix(norm[idx+len(marker):], "/")
		cleanRel := filepath.Clean(filepath.FromSlash(rel))
		if cleanRel == "." || cleanRel == "" || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
			continue
		}
		target := filepath.Join(destRoot, cleanRel)
		if err := writeZipEntryToFile(f, target); err != nil {
			return err
		}
		extracted++
	}
	if extracted == 0 {
		return os.ErrNotExist
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(pathutil.SafeJoin(destRoot, "bin", binName), 0o755)
	}
	return nil
}

func writeZipEntryToFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	mode := f.Mode()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}
