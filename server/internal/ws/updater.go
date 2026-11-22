package ws

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
const ServerVersion = "0.2.0"

// ClientPatchMeta는 서버에 등록된 최신 클라이언트 패치의 무결성 검증 메타데이터입니다.
type ClientPatchMeta struct {
	Version    string `json:"version"`
	FileName   string `json:"fileName"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	UploadedAt string `json:"uploadedAt"`
}

const clientPatchMetaName = "client_patch_meta.json"

// CleanupOldBinaries는 자가 업데이트 직후 남은 .old 백업 바이너리를 정리합니다.
func CleanupOldBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

// LocalIPAddress는 가상 어댑터(docker/utun/veth/vmnet 등)를 제외하고
// 교무실 업무망(LAN) 사설 대역 대표 IPv4 주소를 우선순위에 따라 감지합니다.
func LocalIPAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	var p192, p10, p172, pOther string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsLinkLocalUnicast() {
				continue
			}
			v4 := ipNet.IP.To4()
			if v4 == nil {
				continue
			}
			ipStr := v4.String()
			switch {
			case v4[0] == 192 && v4[1] == 168 && p192 == "":
				p192 = ipStr
			case v4[0] == 10 && p10 == "":
				p10 = ipStr
			case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 && p172 == "":
				p172 = ipStr
			case pOther == "":
				pOther = ipStr
			}
		}
	}
	for _, cand := range []string{p192, p10, p172, pOther} {
		if cand != "" {
			return cand
		}
	}
	return "127.0.0.1"
}

func isVirtualInterface(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	for _, kw := range []string{
		"docker", "utun", "veth", "bridge", "br-", "vmnet", "vbox",
		"virbr", "tailscale", "wg", "wsl", "hyper-v", "vethernet",
		"awdl", "llw", "anpi", "tun", "tap",
	} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// SaveClientPatch는 새 버전 클라이언트 바이너리를 무결성·매직헤더 검증 후 원자적으로 저장하고
// 이전 구버전 패치 파일을 정리한 뒤 전 교사 PC에 자동 패치 알림 이벤트를 발행합니다.
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
	tmpPath := pathutil.SafeJoin(upDir, fmt.Sprintf("latest_client_%s.%d.tmp", cleanName, time.Now().UnixNano()))
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("임시 패치 파일 생성 실패: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), src)
	_ = f.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("클라이언트 패치 파일 저장 실패: %w", copyErr)
	}
	if written == 0 {
		_ = os.Remove(tmpPath)
		return "", errors.New("빈 클라이언트 패치 파일(0바이트)은 등록할 수 없습니다")
	}

	isExe := strings.HasSuffix(strings.ToLower(cleanName), ".exe")
	if valErr := validatePatchBinaryHeader(tmpPath, isExe); valErr != nil {
		_ = os.Remove(tmpPath)
		return "", valErr
	}

	// 구버전 패치 파일 모두 정리 후 원자적 교체
	if entries, rErr := os.ReadDir(upDir); rErr == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && strings.HasPrefix(name, "latest_client_") && !strings.HasSuffix(name, ".tmp") {
				_ = os.Remove(pathutil.SafeJoin(upDir, name))
			}
		}
	}
	destPath := pathutil.SafeJoin(upDir, "latest_client_"+cleanName)
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("클라이언트 패치 교체 실패: %w", err)
	}

	shaHex := hex.EncodeToString(hasher.Sum(nil))
	meta := ClientPatchMeta{
		Version:    strings.TrimSpace(version),
		FileName:   cleanName,
		SHA256:     shaHex,
		Size:       written,
		UploadedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if rawMeta, mErr := json.MarshalIndent(meta, "", "  "); mErr == nil {
		metaTmp := pathutil.SafeJoin(upDir, clientPatchMetaName+".tmp")
		if wErr := os.WriteFile(metaTmp, rawMeta, 0o644); wErr == nil {
			_ = os.Rename(metaTmp, pathutil.SafeJoin(upDir, clientPatchMetaName))
		}
	}

	if bus != nil {
		bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventSystemUpdateAvailable,
			Title:   fmt.Sprintf("꼬미시간 새 버전(%s) 패치 준비 완료", meta.Version),
			Message: "관리자가 새 클라이언트 버전을 배포했습니다. 업데이트를 적용해 주세요.",
			Urgent:  true,
			Payload: map[string]interface{}{
				"version":     meta.Version,
				"downloadUrl": "/api/system/client-patch",
				"metaUrl":     "/api/system/client-patch/meta",
				"fileName":    cleanName,
				"sha256":      shaHex,
				"size":        written,
			},
		})
	}
	return destPath, nil
}

// LatestClientPatchPath는 가장 최근 등록된 유효 클라이언트 패치 파일 경로를 반환합니다.
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
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, "latest_client_") &&
			!strings.HasSuffix(name, ".tmp") && !strings.HasSuffix(name, ".json") {
			origName := strings.TrimPrefix(name, "latest_client_")
			return pathutil.SafeJoin(upDir, name), origName
		}
	}
	return "", ""
}

// LoadClientPatchMeta는 현재 등록된 클라이언트 패치 메타데이터를 반환합니다.
func LoadClientPatchMeta() (*ClientPatchMeta, error) {
	upDir, err := pathutil.UpdateDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(pathutil.SafeJoin(upDir, clientPatchMetaName))
	if err != nil {
		return nil, err
	}
	var meta ClientPatchMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SelfReplaceServerAndRestart는 DB 암호화 백업 후 현재 실행 중인 꼬미시간서버를 교체 재시작합니다.
func SelfReplaceServerAndRestart(src io.Reader, store *database.Store, onShutdown func()) error {
	return SelfReplaceServerAndRestartWithDelay(src, store, onShutdown, 800*time.Millisecond)
}

// SelfReplaceServerAndRestartWithDelay는 지정된 유예 시간 뒤 서버 바이너리를 교체하고 재기동합니다.
func SelfReplaceServerAndRestartWithDelay(src io.Reader, store *database.Store, onShutdown func(), delay time.Duration) error {
	if store != nil {
		backupPath, bErr := store.ExportEncryptedBackup("pre_update")
		if bErr != nil {
			return fmt.Errorf("업데이트 전 암호화 백업 생성 실패로 업데이트를 중단합니다: %w", bErr)
		}
		log.Printf("[업데이트] 업데이트 전 자동 암호화 백업 완료: %s", backupPath)
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
	written, copyErr := io.Copy(out, src)
	_ = out.Close()
	if copyErr != nil || written == 0 {
		_ = os.Remove(newPath)
		if copyErr != nil {
			return fmt.Errorf("임시 바이너리 쓰기 실패: %w", copyErr)
		}
		return errors.New("빈 서버 바이너리 파일입니다")
	}

	if valErr := validateServerBinaryForOS(newPath); valErr != nil {
		return valErr
	}

	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("기존 바이너리 교체 준비 실패: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath)
		_ = os.Remove(newPath)
		return fmt.Errorf("신규 바이너리 교체 실패: %w", err)
	}
	if runtime.GOOS == "darwin" {
		resignDarwinServerBundle(exePath)
	}

	if delay <= 0 {
		delay = 800 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		if onShutdown != nil {
			onShutdown()
		}
		cmd := exec.Command(exePath, os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if startErr := cmd.Start(); startErr != nil {
			log.Printf("[업데이트] 재시작 실패 — 기존 바이너리로 롤백 시도: %v", startErr)
			_ = os.Rename(exePath, exePath+".failed")
			_ = os.Rename(oldPath, exePath)
		}
		os.Exit(0)
	}()

	return nil
}

func validatePatchBinaryHeader(path string, expectPE bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var hdr [4]byte
	n, rErr := io.ReadFull(f, hdr[:])
	_ = f.Close()
	if rErr != nil || n < 4 {
		return errors.New("손상되었거나 너무 짧은 클라이언트 패치 파일입니다")
	}
	isPE := hdr[0] == 'M' && hdr[1] == 'Z'
	isMachO := isMachOMagic(hdr)
	isELF := hdr[0] == 0x7F && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F'
	if expectPE {
		if !isPE {
			return errors.New("Windows 실행 파일(.exe, MZ 헤더) 형식이 아닙니다")
		}
		return nil
	}
	if !isMachO && !isELF {
		return errors.New("올바른 실행 파일(Mach-O/ELF) 형식이 아닙니다")
	}
	return nil
}

func validateServerBinaryForOS(path string) error {
	f, err := os.Open(path)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	var hdr [4]byte
	n, rErr := io.ReadFull(f, hdr[:])
	_ = f.Close()
	if rErr != nil || n < 4 {
		_ = os.Remove(path)
		return errors.New("손상되었거나 올바르지 않은 서버 바이너리 파일입니다")
	}
	isPE := hdr[0] == 'M' && hdr[1] == 'Z'
	isMachO := isMachOMagic(hdr)
	switch runtime.GOOS {
	case "windows":
		if !isPE {
			_ = os.Remove(path)
			return errors.New("현재 OS(Windows)와 호환되지 않는 서버 실행 파일(.exe 아님)입니다")
		}
	case "darwin":
		if isPE {
			_ = os.Remove(path)
			return errors.New("배포된 서버 파일이 Windows 전용(.exe)이므로 macOS 서버에 적용할 수 없습니다")
		}
		if !isMachO {
			_ = os.Remove(path)
			return errors.New("올바른 macOS 실행 파일(Mach-O) 형식이 아닙니다")
		}
	}
	return nil
}

func isMachOMagic(hdr [4]byte) bool {
	return (hdr[0] == 0xCF && hdr[1] == 0xFA && hdr[2] == 0xED && hdr[3] == 0xFE) ||
		(hdr[0] == 0xFE && hdr[1] == 0xED && hdr[2] == 0xFA && hdr[3] == 0xCF) ||
		(hdr[0] == 0xCA && hdr[1] == 0xFE && hdr[2] == 0xBA && hdr[3] == 0xBE) ||
		(hdr[0] == 0xBE && hdr[1] == 0xBA && hdr[2] == 0xFE && hdr[3] == 0xCA)
}

func resignDarwinServerBundle(exePath string) {
	marker := filepath.Join(".app", "Contents", "MacOS")
	idx := strings.Index(exePath, marker)
	if idx == -1 {
		return
	}
	bundlePath := exePath[:idx+4]
	_ = exec.Command("xattr", "-cr", bundlePath).Run()
	if out, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", bundlePath).CombinedOutput(); err != nil {
		log.Printf("[서버 업데이트] macOS 번들 재서명 경고 (%s): %v (%s)", bundlePath, err, strings.TrimSpace(string(out)))
	}
}
