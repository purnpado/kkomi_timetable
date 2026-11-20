package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

const (
	// GitHubRepoOwner는 꼬미시간 공식 GitHub 릴리즈 저장소 소유자입니다.
	GitHubRepoOwner = "purnpado"
	// GitHubRepoName은 꼬미시간 공식 GitHub 릴리즈 저장소 이름입니다.
	GitHubRepoName = "kkomi_timetable"
)

var gitHubLatestReleaseAPI = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", GitHubRepoOwner, GitHubRepoName)

// GitHubReleaseInfo는 GitHub Releases 최신 릴리즈 조회 결과 및 다운로드 URL 정보입니다.
type GitHubReleaseInfo struct {
	Repository      string `json:"repository"`
	CurrentVersion  string `json:"currentVersion"`
	TagName         string `json:"tagName"`
	Version         string `json:"version"`
	HasUpdate       bool   `json:"hasUpdate"`
	HTMLURL         string `json:"htmlUrl"`
	PublishedAt     string `json:"publishedAt"`
	Body            string `json:"body"`
	ServerAssetName string `json:"serverAssetName"`
	ServerAssetURL  string `json:"serverAssetUrl"`
	ClientAssetName string `json:"clientAssetName"`
	ClientAssetURL  string `json:"clientAssetUrl"`
	PortableZipName string `json:"portableZipName"`
	PortableZipURL  string `json:"portableZipUrl"`
}

type ghReleasePayload struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		Label              string `json:"label"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// ParseVersionTuple은 "v0.1.01" 같은 버전 문자열을 비교 가능한 정수 슬라이스로 변환합니다.
func ParseVersionTuple(ver string) []int {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(ver, "v"), "V"))
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			nums = append(nums, 0)
			continue
		}
		nums = append(nums, n)
	}
	return nums
}

// IsNewerVersion은 remoteVer가 currentVer보다 높은 버전인지 판정합니다.
func IsNewerVersion(remoteVer, currentVer string) bool {
	rNums := ParseVersionTuple(remoteVer)
	cNums := ParseVersionTuple(currentVer)
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

// CheckGitHubLatestRelease는 공식 GitHub 저장소에서 최신 릴리즈 메타데이터를 조회합니다.
func CheckGitHubLatestRelease() (*GitHubReleaseInfo, error) {
	return FetchLatestGitHubReleaseFrom(gitHubLatestReleaseAPI, ServerVersion)
}

// FetchLatestGitHubReleaseFrom은 지정된 GitHub Releases API 엔드포인트에서 릴리즈 정보를 파싱합니다.
func FetchLatestGitHubReleaseFrom(apiURL, currentVer string) (*GitHubReleaseInfo, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "KkomiTimetable-Updater/"+currentVer)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub 릴리즈 조회 연결 실패 (인터넷 연결 확인): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("GitHub 저장소에 아직 게시된 정식 릴리즈가 없습니다")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 응답 오류 (상태코드 %d)", resp.StatusCode)
	}

	var raw ghReleasePayload
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("GitHub 릴리즈 정보 파싱 실패: %w", err)
	}

	remoteVer := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw.TagName), "v"), "V")
	info := &GitHubReleaseInfo{
		Repository:     GitHubRepoOwner + "/" + GitHubRepoName,
		CurrentVersion: currentVer,
		TagName:        raw.TagName,
		Version:        remoteVer,
		HasUpdate:      IsNewerVersion(remoteVer, currentVer),
		HTMLURL:        raw.HTMLURL,
		PublishedAt:    raw.PublishedAt,
		Body:           raw.Body,
	}
	for _, a := range raw.Assets {
		low := strings.ToLower(a.Name)
		isServer := strings.Contains(a.Name, "서버") || strings.Contains(a.Label, "서버") || strings.Contains(low, "server")
		isClient := strings.Contains(a.Name, "꼬미시간") || strings.Contains(a.Label, "꼬미시간") || strings.Contains(low, "client") || strings.Contains(low, "kkomi-timetable")
		switch {
		case strings.HasSuffix(low, ".zip") && (strings.Contains(low, "portable") || strings.Contains(low, "kkomi-timetable")):
			info.PortableZipName = a.Name
			info.PortableZipURL = a.BrowserDownloadURL
		case strings.HasSuffix(low, ".exe") && isServer:
			info.ServerAssetName = a.Name
			info.ServerAssetURL = a.BrowserDownloadURL
		case strings.HasSuffix(low, ".exe") && !isServer && isClient:
			info.ClientAssetName = a.Name
			info.ClientAssetURL = a.BrowserDownloadURL
		}
	}
	return info, nil
}

// ApplyGitHubReleaseUpdate는 GitHub 최신 릴리즈에서 클라이언트 패치(꼬미시간.exe)를 배포하거나
// 실행 중인 서버 바이너리(꼬미시간서버.exe)를 무중단 자동 업데이트합니다.
func ApplyGitHubReleaseUpdate(target string, store *database.Store, bus *eventbus.Bus, onShutdown func()) (*GitHubReleaseInfo, error) {
	info, err := CheckGitHubLatestRelease()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 180 * time.Second}

	if target == "client" || target == "all" {
		if info.ClientAssetURL == "" {
			return info, errors.New("GitHub 최신 릴리즈에 클라이언트 실행 파일(꼬미시간.exe) 에셋이 없습니다")
		}
		resp, err := client.Get(info.ClientAssetURL) // #nosec G107 -- 검증된 GitHub 릴리즈 에셋 URL
		if err != nil {
			return info, fmt.Errorf("GitHub 클라이언트 바이너리 다운로드 실패: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return info, fmt.Errorf("GitHub 클라이언트 에셋 다운로드 오류 (%d)", resp.StatusCode)
		}
		if _, err := SaveClientPatch(info.Version, info.ClientAssetName, resp.Body, bus); err != nil {
			return info, err
		}
	}

	if target == "server" || target == "all" {
		if info.ServerAssetURL == "" {
			return info, errors.New("GitHub 최신 릴리즈에 서버 실행 파일(꼬미시간서버.exe) 에셋이 없습니다")
		}
		isWinAsset := strings.HasSuffix(strings.ToLower(info.ServerAssetName), ".exe")
		if isWinAsset != (runtime.GOOS == "windows") {
			return info, fmt.Errorf("현재 서버 OS(%s)와 릴리즈 에셋(%s)의 운영체제가 달라 서버 바이너리를 직접 교체할 수 없습니다", runtime.GOOS, info.ServerAssetName)
		}
		resp, err := client.Get(info.ServerAssetURL) // #nosec G107 -- 검증된 GitHub 릴리즈 에셋 URL
		if err != nil {
			return info, fmt.Errorf("GitHub 서버 바이너리 다운로드 실패: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return info, fmt.Errorf("GitHub 서버 에셋 다운로드 오류 (%d)", resp.StatusCode)
		}
		delay := 800 * time.Millisecond
		if target == "all" {
			// 클라이언트들이 WebSocket 알림을 받고 패치 파일을 내려받을 여유 시간 확보
			delay = 3500 * time.Millisecond
		}
		if err := SelfReplaceServerAndRestartWithDelay(resp.Body, store, onShutdown, delay); err != nil {
			return info, err
		}
	}

	if target != "client" && target != "server" && target != "all" {
		return info, fmt.Errorf("지원하지 않는 업데이트 대상(%s)입니다", target)
	}
	return info, nil
}
