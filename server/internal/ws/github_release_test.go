package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubReleaseVersionCompareAndParse(t *testing.T) {
	if !IsNewerVersion("v0.1.01", "0.1.0") {
		t.Fatalf("v0.1.01은 0.1.0보다 최신 버전이어야 합니다")
	}
	if !IsNewerVersion("v0.1.02", "0.1.01") {
		t.Fatalf("v0.1.02는 0.1.01보다 최신 버전이어야 합니다")
	}
	if IsNewerVersion("v0.1.01", "0.1.01") {
		t.Fatalf("동일 버전(v0.1.01 == 0.1.01)은 업데이트 대상으로 판정하면 안 됩니다")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name":     "v0.1.02",
			"html_url":     "https://github.com/purnpado/kkomi_timetable/releases/tag/v0.1.02",
			"published_at": "2026-11-16T06:00:00Z",
			"body":         "안정화 업데이트",
			"assets": []map[string]string{
				{"name": "kkomi-timetable-server.exe", "label": "꼬미시간서버.exe", "browser_download_url": "https://example.com/server.exe"},
				{"name": "kkomi-timetable-client.exe", "label": "꼬미시간.exe", "browser_download_url": "https://example.com/client.exe"},
				{"name": "kkomi-timetable_v0.1.02.zip", "label": "꼬미시간_v0.1.02_windows_portable.zip", "browser_download_url": "https://example.com/portable.zip"},
			},
		})
	}))
	defer srv.Close()

	info, err := FetchLatestGitHubReleaseFrom(srv.URL, "0.1.01")
	if err != nil {
		t.Fatalf("GitHub 릴리즈 파싱 실패: %v", err)
	}
	if !info.HasUpdate || info.Version != "0.1.02" {
		t.Fatalf("버전 비교 오류: %+v", info)
	}
	if info.ServerAssetURL != "https://example.com/server.exe" || info.ClientAssetURL != "https://example.com/client.exe" || info.PortableZipURL != "https://example.com/portable.zip" {
		t.Fatalf("영문/라벨 에셋 매칭 오류: %+v", info)
	}

	// 비표준 이름 + 한글 label 조합에서도 정상 매칭되는지 검증
	srvLabel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v0.1.03",
			"assets": []map[string]string{
				{"name": "release-win-a.exe", "label": "꼬미시간서버.exe", "browser_download_url": "https://example.com/lbl-server.exe"},
				{"name": "release-win-b.exe", "label": "꼬미시간.exe", "browser_download_url": "https://example.com/lbl-client.exe"},
			},
		})
	}))
	defer srvLabel.Close()

	labelInfo, err := FetchLatestGitHubReleaseFrom(srvLabel.URL, "0.1.01")
	if err != nil {
		t.Fatalf("라벨 기반 릴리즈 파싱 실패: %v", err)
	}
	if labelInfo.ServerAssetURL != "https://example.com/lbl-server.exe" || labelInfo.ClientAssetURL != "https://example.com/lbl-client.exe" {
		t.Fatalf("라벨 기반 에셋 매칭 오류: %+v", labelInfo)
	}
}
