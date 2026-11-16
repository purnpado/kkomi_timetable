package pathutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKoreanAndSpacePathSafety는 한글 폴더명 및 공백이 포함된 절대 경로에서도
// 안전하게 디렉토리 생성 및 경로 결합이 작동하는지 검증합니다.
func TestKoreanAndSpacePathSafety(t *testing.T) {
	tmpRoot := t.TempDir()
	koreanBase := filepath.Join(tmpRoot, "테스트 학교 교무실", "꼬미 시간 2026")
	t.Setenv("KKOMI_BASE_DIR", koreanBase)

	dataDir, err := DataDir()
	if err != nil {
		t.Fatalf("한글/공백 경로 DataDir 생성 실패: %v", err)
	}
	if !strings.Contains(dataDir, "꼬미 시간 2026") {
		t.Fatalf("경로 보존 실패: %s", dataDir)
	}

	testFile := SafeJoin(dataDir, "샘플 설정 파일.json")
	if err := os.WriteFile(testFile, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("한글 경로 파일 쓰기 실패: %v", err)
	}
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("한글 경로 파일 검증 실패: %v", err)
	}
}
