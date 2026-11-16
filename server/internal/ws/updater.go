package ws

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// ServerVersion은 현재 서버 바이너리의 릴리즈 버전입니다.
const ServerVersion = "0.1.01"

// CleanupOldBinaries는 자가 업데이트 직후 남은 .old 백업 바이너리를 정리합니다.
func CleanupOldBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	oldPath := exe + ".old"
	_ = os.Remove(oldPath)
}

// LocalIPAddress는 교무실 공용 PC가 업무망(LAN)에서 사용하는 대표 IPv4 주소를 반환합니다.
func LocalIPAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if v4 := ipNet.IP.To4(); v4 != nil {
					return v4.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// SaveClientPatch는 관리자가 업로드한 새 버전 클라이언트 바이너리(꼬미시간.exe 등)를 updates/ 폴더에 저장하고
// 전 교사 PC에 자동 패치 알림 이벤트를 발행합니다.
func SaveClientPatch(version string, fileName string, src io.Reader, bus *eventbus.Bus) (string, error) {
	upDir, err := pathutil.UpdateDir()
	if err != nil {
		return "", err
	}
	cleanName := filepath.Base(strings.TrimSpace(fileName))
	if cleanName == "" || cleanName == "." {
		if runtime.GOOS == "windows" {
			cleanName = "꼬미시간.exe"
		} else {
			cleanName = "꼬미시간"
		}
	}
	destPath := pathutil.SafeJoin(upDir, "latest_client_"+cleanName)
	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("클라이언트 패치 파일 생성 실패: %w", err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("클라이언트 패치 파일 저장 실패: %w", err)
	}
	_ = f.Close()

	if bus != nil {
		bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventSystemUpdateAvailable,
			Title:   fmt.Sprintf("꼬미시간 새 버전(%s) 패치 준비 완료", version),
			Message: "관리자가 새 클라이언트 버전을 배포했습니다. 잠시 후 자동 업데이트가 적용됩니다.",
			Urgent:  true,
			Payload: map[string]string{
				"version":     version,
				"downloadUrl": "/api/system/client-patch",
				"fileName":    cleanName,
			},
		})
	}
	return destPath, nil
}

// LatestClientPatchPath는 가장 최근 업로드된 클라이언트 패치 파일 경로를 찾습니다.
func LatestClientPatchPath() (string, string) {
	upDir, err := pathutil.UpdateDir()
	if err != nil {
		return "", ""
	}
	entries, err := os.ReadDir(upDir)
	if err != nil {
		return "", ""
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "latest_client_") {
			origName := strings.TrimPrefix(entry.Name(), "latest_client_")
			return pathutil.SafeJoin(upDir, entry.Name()), origName
		}
	}
	return "", ""
}

// SelfReplaceServerAndRestart는 DB를 암호화 백업한 뒤 현재 실행 중인 꼬미시간서버(.exe)를
// 파일명 교체(Self-Replace) 방식으로 바꾸고 2초 내 자가 재시작합니다.
func SelfReplaceServerAndRestart(src io.Reader, store *database.Store, onShutdown func()) error {
	if store != nil {
		if backupPath, bErr := store.ExportEncryptedBackup("pre_update"); bErr == nil {
			log.Printf("[업데이트] 업데이트 전 자동 암호화 백업 완료: %s", backupPath)
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("실행 파일 경로 확인 실패: %w", err)
	}
	newPath := exePath + ".new"
	out, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("임시 바이너리 생성 실패: %w", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return fmt.Errorf("임시 바이너리 쓰기 실패: %w", err)
	}
	_ = out.Close()

	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("기존 바이너리 교체 준비 실패: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath)
		return fmt.Errorf("신규 바이너리 교체 실패: %w", err)
	}

	// 비동기로 안전 종료 후 신규 바이너리 재기동
	go func() {
		time.Sleep(800 * time.Millisecond)
		if onShutdown != nil {
			onShutdown()
		}
		cmd := exec.Command(exePath, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if startErr := cmd.Start(); startErr != nil {
			log.Printf("[업데이트] 재시작 실패: %v", startErr)
		}
		os.Exit(0)
	}()

	return nil
}
