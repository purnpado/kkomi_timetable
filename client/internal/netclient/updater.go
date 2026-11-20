package netclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const gitHubLatestAPI = "https://api.github.com/repos/purnpado/kkomi_timetable/releases/latest"

type patchMeta struct {
	Version  string `json:"version"`
	FileName string `json:"fileName"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// ApplyClientSelfUpdate는 서버 패치 또는 GitHub Releases에서 최신 클라이언트 실행 파일을 내려받아
// 버전 비교, 크기·SHA-256 무결성·OS 매직 헤더 검증을 통과한 뒤 안전하게 자가 교체 재실행합니다.
func (c *Client) ApplyClientSelfUpdate() error {
	httpCli := &http.Client{Timeout: 90 * time.Second}
	var (
		body           io.ReadCloser
		expectedLen    int64 = -1
		expectedSHA256 string
	)

	meta := c.fetchServerPatchMeta(httpCli)
	if meta != nil && meta.Version != "" && !isNewerVersion(meta.Version, ClientVersion) {
		meta = nil
	}

	if meta != nil {
		patchURL := c.ServerURL() + "/api/system/client-patch"
		resp, err := httpCli.Get(patchURL) // #nosec G107 -- 업무망 내부 서버 URL
		if err == nil && resp.StatusCode == http.StatusOK {
			body = resp.Body
			expectedLen = resp.ContentLength
			if meta.Size > 0 && expectedLen <= 0 {
				expectedLen = meta.Size
			}
			expectedSHA256 = meta.SHA256
		} else if resp != nil {
			_ = resp.Body.Close()
		}
	} else {
		patchURL := c.ServerURL() + "/api/system/client-patch"
		resp, err := httpCli.Get(patchURL) // #nosec G107 -- 업무망 내부 서버 URL
		if err == nil && resp.StatusCode == http.StatusOK {
			body = resp.Body
			expectedLen = resp.ContentLength
		} else if resp != nil {
			_ = resp.Body.Close()
		}
	}

	if body == nil {
		ghBody, ghLen, ghErr := openGitHubClientAsset(httpCli)
		if ghErr != nil {
			return ghErr
		}
		body = ghBody
		expectedLen = ghLen
	}
	defer body.Close()

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	newPath := exePath + ".new"
	out, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), body)
	_ = out.Close()
	if copyErr != nil {
		_ = os.Remove(newPath)
		return copyErr
	}
	if written == 0 {
		_ = os.Remove(newPath)
		return errors.New("다운로드된 클라이언트 파일이 비어 있습니다")
	}
	if expectedLen > 0 && written != expectedLen {
		_ = os.Remove(newPath)
		return fmt.Errorf("다운로드 크기 불일치 (기대 %d바이트, 실제 %d바이트)", expectedLen, written)
	}
	if expectedSHA256 != "" {
		actualSHA := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualSHA, expectedSHA256) {
			_ = os.Remove(newPath)
			return errors.New("패치 파일 무결성(SHA-256) 검증에 실패했습니다")
		}
	}

	if valErr := validateClientBinaryForOS(newPath); valErr != nil {
		return valErr
	}

	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		_ = os.Remove(newPath)
		return err
	}
	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath)
		_ = os.Remove(newPath)
		return err
	}
	if runtime.GOOS == "darwin" {
		resignDarwinAppBundle(exePath)
	}

	go func() {
		time.Sleep(600 * time.Millisecond)
		cmd := exec.Command(exePath, os.Args[1:]...)
		if startErr := cmd.Start(); startErr != nil {
			log.Printf("[클라이언트 업데이트] 재실행 실패: %v", startErr)
		}
		os.Exit(0)
	}()
	return nil
}

func (c *Client) fetchServerPatchMeta(httpCli *http.Client) *patchMeta {
	metaURL := c.ServerURL() + "/api/system/client-patch/meta"
	resp, err := httpCli.Get(metaURL) // #nosec G107 -- 업무망 내부 서버 URL
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m patchMeta
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil
	}
	return &m
}

func openGitHubClientAsset(httpCli *http.Client) (io.ReadCloser, int64, error) {
	req, err := http.NewRequest(http.MethodGet, gitHubLatestAPI, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "KkomiTimetable-Client/"+ClientVersion)

	resp, err := httpCli.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("서버 및 GitHub 릴리즈 업데이트 연결 실패: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("GitHub 최신 릴리즈 조회 실패 (상태코드 %d)", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			Label              string `json:"label"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, 0, fmt.Errorf("GitHub 릴리즈 응답 파싱 실패: %w", err)
	}

	remoteVer := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v"), "V")
	if !isNewerVersion(remoteVer, ClientVersion) {
		return nil, 0, fmt.Errorf("이미 최신 버전(v%s)을 사용 중입니다 (GitHub 최신: %s)", ClientVersion, rel.TagName)
	}

	var assetName, assetURL string
	for _, a := range rel.Assets {
		low := strings.ToLower(a.Name)
		isServer := strings.Contains(a.Name, "서버") || strings.Contains(a.Label, "서버") || strings.Contains(low, "server")
		isClient := strings.Contains(a.Name, "꼬미시간") || strings.Contains(a.Label, "꼬미시간") || strings.Contains(low, "client") || strings.Contains(low, "kkomi-timetable")
		if strings.HasSuffix(low, ".exe") && !isServer && isClient {
			assetName = a.Name
			assetURL = a.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return nil, 0, fmt.Errorf("GitHub 릴리즈(%s)에서 클라이언트 실행 파일(꼬미시간.exe)을 찾지 못했습니다", rel.TagName)
	}
	if runtime.GOOS != "windows" {
		return nil, 0, fmt.Errorf("GitHub 최신 릴리즈(%s)는 Windows용(%s)입니다. macOS에서는 소스 빌드를 이용해 주세요", rel.TagName, assetName)
	}

	dlResp, err := httpCli.Get(assetURL) // #nosec G107 -- GitHub Releases 공식 다운로드 URL
	if err != nil {
		return nil, 0, fmt.Errorf("GitHub 클라이언트 다운로드 실패: %w", err)
	}
	if dlResp.StatusCode != http.StatusOK {
		_ = dlResp.Body.Close()
		return nil, 0, fmt.Errorf("GitHub 클라이언트 다운로드 오류 (%d)", dlResp.StatusCode)
	}
	return dlResp.Body, dlResp.ContentLength, nil
}

func isNewerVersion(remoteVer, currentVer string) bool {
	parse := func(v string) []int {
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V"))
		if idx := strings.IndexAny(s, "-+"); idx >= 0 {
			s = s[:idx]
		}
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ".")
		nums := make([]int, 0, len(parts))
		for _, p := range parts {
			n, _ := strconv.Atoi(strings.TrimSpace(p))
			nums = append(nums, n)
		}
		return nums
	}
	rNums, cNums := parse(remoteVer), parse(currentVer)
	maxLen := len(rNums)
	if len(cNums) > maxLen {
		maxLen = len(cNums)
	}
	for i := 0; i < maxLen; i++ {
		rv, cv := 0, 0
		if i < len(rNums) {
			rv = rNums[i]
		}
		if i < len(cNums) {
			cv = cNums[i]
		}
		if rv != cv {
			return rv > cv
		}
	}
	return false
}

func validateClientBinaryForOS(path string) error {
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
		return errors.New("손상되었거나 올바르지 않은 클라이언트 바이너리 파일입니다")
	}
	isPE := hdr[0] == 'M' && hdr[1] == 'Z'
	isMachO := (hdr[0] == 0xCF && hdr[1] == 0xFA && hdr[2] == 0xED && hdr[3] == 0xFE) ||
		(hdr[0] == 0xFE && hdr[1] == 0xED && hdr[2] == 0xFA && hdr[3] == 0xCF) ||
		(hdr[0] == 0xCA && hdr[1] == 0xFE && hdr[2] == 0xBA && hdr[3] == 0xBE) ||
		(hdr[0] == 0xBE && hdr[1] == 0xBA && hdr[2] == 0xFE && hdr[3] == 0xCA)
	switch runtime.GOOS {
	case "windows":
		if !isPE {
			_ = os.Remove(path)
			return errors.New("현재 OS(Windows)와 호환되지 않는 클라이언트 실행 파일(.exe 아님)입니다")
		}
	case "darwin":
		if isPE {
			_ = os.Remove(path)
			return errors.New("배포된 클라이언트 패치가 Windows 전용(.exe)이므로 macOS 앱에 적용할 수 없습니다")
		}
		if !isMachO {
			_ = os.Remove(path)
			return errors.New("올바른 macOS 실행 파일(Mach-O) 형식이 아닙니다")
		}
	}
	return nil
}

func resignDarwinAppBundle(exePath string) {
	marker := filepath.Join(".app", "Contents", "MacOS")
	idx := strings.Index(exePath, marker)
	if idx == -1 {
		return
	}
	bundlePath := exePath[:idx+4]
	_ = exec.Command("xattr", "-cr", bundlePath).Run()
	if out, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", bundlePath).CombinedOutput(); err != nil {
		log.Printf("[클라이언트 업데이트] macOS 번들 재서명 경고 (%s): %v (%s)", bundlePath, err, strings.TrimSpace(string(out)))
	}
}
