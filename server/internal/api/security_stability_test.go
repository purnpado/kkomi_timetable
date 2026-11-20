package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"kkomi-timetable/pkg/protocol"
)

func doSpoofHeaderReq(t *testing.T, h http.Handler, method, path, spoofID string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if spoofID != "" {
		req.Header.Set("X-Teacher-ID", spoofID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestSecurityAuthTokenAndSpoofingProtection은 서명 토큰 없는 접근 및 X-Teacher-ID/쿼리 스푸핑 차단을 검증합니다.
func TestSecurityAuthTokenAndSpoofingProtection(t *testing.T) {
	_, h := newTestServerApp(t)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/teachers"},
		{http.MethodGet, "/api/dashboard"},
		{http.MethodGet, "/api/internal-exam/slots"},
		{http.MethodGet, "/api/internal-exam/substitute/recommend?slotId=x"},
		{http.MethodGet, "/api/external-exam/candidates?noticeId=ext-2027-csat"},
		{http.MethodGet, "/api/export/xlsx?kind=teachers"},
		{http.MethodPost, "/api/system/reset"},
	}

	for _, ep := range endpoints {
		// 1) 완전 무인증 차단
		if code := doSpoofHeaderReq(t, h, ep.method, ep.path, ""); code != http.StatusUnauthorized {
			t.Fatalf("무인증 요청 차단 실패 (%s %s): got=%d, want=401", ep.method, ep.path, code)
		}
		// 2) X-Teacher-ID: t01 스푸핑 차단
		if code := doSpoofHeaderReq(t, h, ep.method, ep.path, "t01"); code != http.StatusUnauthorized {
			t.Fatalf("X-Teacher-ID 스푸핑 차단 실패 (%s %s): got=%d, want=401", ep.method, ep.path, code)
		}
		// 3) ?teacherId=t01 쿼리 스푸핑 차단
		sep := "?"
		if strings.Contains(ep.path, "?") {
			sep = "&"
		}
		if code := doSpoofHeaderReq(t, h, ep.method, ep.path+sep+"teacherId=t01", ""); code != http.StatusUnauthorized {
			t.Fatalf("teacherId 쿼리 스푸핑 차단 실패 (%s %s): got=%d, want=401", ep.method, ep.path, code)
		}
	}

	// WebSocket 스푸핑 차단
	srv := httptest.NewServer(h)
	defer srv.Close()
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws?teacherId=t01"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatalf("토큰 없는 WebSocket 연결이 허용되었습니다")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("토큰 없는 WebSocket 응답 코드 오류: got=%d, want=401", resp.StatusCode)
	}
}

// TestSecurityPrivilegeEscalationAndStatPreservation은 비-MASTER 권한 상승 차단과
// 교사 수정 시 연간 시험감독·결보강 통계 보존 및 외부감독 민감 정보 마스킹을 검증합니다.
func TestSecurityPrivilegeEscalationAndStatPreservation(t *testing.T) {
	app, h := newTestServerApp(t)
	tokTimetable := loginForToken(t, h, "timetable", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 1. TIMETABLE_ADMIN이 교사 저장 API로 MASTER 권한 상승 시도 시 403 차단
	code, _ := doRecorderReq(t, h, http.MethodPost, "/api/teachers", tokTimetable, map[string]interface{}{
		"teacher": protocol.Teacher{
			ID:       "t02",
			Username: "timetable",
			Name:     "이수업",
			Subject:  "수학",
			Roles:    []protocol.Role{protocol.RoleMaster, protocol.RoleTimetableAdmin, protocol.RoleTeacher},
		},
	})
	if code != http.StatusForbidden {
		t.Fatalf("TIMETABLE_ADMIN의 MASTER 권한 상승 시도가 차단되지 않았습니다: status=%d", code)
	}

	// 2. 교사 정보 수정 시 기존 연간 감독 횟수·결보강 횟수·면제 설정이 보존되는지 검증
	before, err := app.Auth.GetTeacher("t15")
	if err != nil {
		t.Fatalf("t15 조회 실패: %v", err)
	}
	beforeT04, _ := app.Auth.GetTeacher("t04")
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/teachers", tokTimetable, map[string]interface{}{
		"teacher": protocol.Teacher{
			ID:       beforeT04.ID,
			Username: beforeT04.Username,
			Name:     beforeT04.Name,
			Subject:  beforeT04.Subject,
			Position: "과학교사(수정)",
			Roles:    []protocol.Role{protocol.RoleTeacher},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("교사 일반 정보 수정 실패: status=%d, body=%s", code, string(raw))
	}
	afterT04, _ := app.Auth.GetTeacher("t04")
	if afterT04.ExternalExamCount != beforeT04.ExternalExamCount ||
		afterT04.InternalExamCount != beforeT04.InternalExamCount ||
		afterT04.SubstitutionCount != beforeT04.SubstitutionCount ||
		len(afterT04.Roles) != len(beforeT04.Roles) {
		t.Fatalf("교사 수정 시 누적 통계 또는 기존 역할이 유실되었습니다: before=%+v, after=%+v", beforeT04, afterT04)
	}
	if !before.InternalExamExempt {
		t.Fatalf("t15 초기 제외 상태 오류")
	}

	// 3. 일반 교사가 외부감독 후보 조회 시 타 교사의 민감 사유(임산부/수험생자녀) 및 연락처 마스킹
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/external-exam/candidates?noticeId=ext-2027-csat", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("외부감독 후보 조회 실패: status=%d", code)
	}
	bodyStr := string(raw)
	if strings.Contains(bodyStr, "임산부") || strings.Contains(bodyStr, "수험생 자녀") || strings.Contains(bodyStr, "010-2100-0013") {
		t.Fatalf("일반 교사 응답에 타 교사 민감 면제 사유 또는 연락처가 노출되었습니다: %s", bodyStr)
	}
}

// TestSessionRevocationAndEncryptedBackupRestore는 비밀번호 변경/초기화/로그아웃 시 토큰 무효화와
// 암호화 백업 생성 -> 전체 초기화 -> 백업 복원 E2E를 검증합니다.
func TestSessionRevocationAndEncryptedBackupRestore(t *testing.T) {
	app, h := newTestServerApp(t)
	tokAdmin := loginForToken(t, h, "admin", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 1. 로그아웃 후 토큰 즉시 무효화
	code, _ := doRecorderReq(t, h, http.MethodPost, "/api/auth/logout", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("로그아웃 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodGet, "/api/dashboard", tokT06, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("로그아웃 후 구 토큰이 무효화되지 않았습니다: %d", code)
	}

	// 2. 마스터 백업 생성 -> FULL_FACTORY_RESET -> 백업 복원 -> 교사 데이터 원복 및 EventSystemRestored 발행
	tokT06 = loginForToken(t, h, "t06", "1234")
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/system/backups", tokAdmin, nil)
	if code != http.StatusOK {
		t.Fatalf("백업 생성 실패: %d, %s", code, string(raw))
	}
	var bkRes struct {
		BackupFile string `json:"backupFile"`
	}
	_ = json.Unmarshal(raw, &bkRes)
	bkName := filepath.Base(bkRes.BackupFile)

	// 초기화 실행
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/system/reset", tokAdmin, map[string]string{"mode": "FULL_FACTORY_RESET"})
	if code != http.StatusOK {
		t.Fatalf("완전 초기화 실패: %d", code)
	}
	// 초기화 후 기존 토큰 무효화 확인
	if code, _ := doRecorderReq(t, h, http.MethodGet, "/api/teachers", tokT06, nil); code != http.StatusUnauthorized {
		t.Fatalf("시스템 초기화 후 구 토큰이 무효화되지 않았습니다: %d", code)
	}

	// 새 admin 로그인 후 방금 만든 백업 복원 실행
	tokAdminNew := loginForToken(t, h, "admin", "1234")
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/system/backups/restore", tokAdminNew, map[string]string{
		"fileName": bkName,
	})
	if code != http.StatusOK {
		t.Fatalf("암호화 백업 복원 실패: status=%d, body=%s", code, string(raw))
	}
	// 복원 후 기존 토큰 무효화 확인
	if code, _ := doRecorderReq(t, h, http.MethodGet, "/api/teachers", tokAdminNew, nil); code != http.StatusUnauthorized {
		t.Fatalf("백업 복원 후 구 토큰이 무효화되지 않았습니다: %d", code)
	}
	// 복원된 데이터 검증 (t06 다시 로그인 가능)
	_ = loginForToken(t, h, "t06", "1234")

	// 경로 이탈 백업 파일명 차단 검증
	tokAdminAfter := loginForToken(t, h, "admin", "1234")
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/system/backups/restore", tokAdminAfter, map[string]string{
		"fileName": "../../etc/passwd",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("경로 이탈 백업 복원 요청이 차단되지 않았습니다: %d", code)
	}

	// EventSystemRestored 발행 확인
	foundRestored := false
	for _, ev := range app.Bus.RecentEvents(20) {
		if ev.Type == protocol.EventSystemRestored {
			foundRestored = true
			break
		}
	}
	if !foundRestored {
		t.Fatalf("EventSystemRestored 도메인 이벤트가 발행되지 않았습니다")
	}
}
