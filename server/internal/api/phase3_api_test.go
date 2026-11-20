package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
)

func TestPhase3ConstraintsScenariosHistoryAPI(t *testing.T) {
	_, h := newTestServerApp(t)
	tokTeacher := loginForToken(t, h, "t06", "1234")
	tokAdmin := loginForToken(t, h, "timetable", "1234")

	// 1. 일반 교사 조회(GET)는 200 허용, 쓰기(POST/DELETE)는 모두 403 차단
	for _, path := range []string{"/api/timetable/constraints", "/api/timetable/scenarios", "/api/timetable/history"} {
		if code, _ := doRecorderReq(t, h, http.MethodGet, path, tokTeacher, nil); code != http.StatusOK {
			t.Fatalf("일반 교사 GET(%s) 실패: status=%d", path, code)
		}
	}
	forbiddenCalls := []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPost, "/api/timetable/constraints", protocol.TeacherConstraint{TeacherID: "t06", DayOfWeek: 2, Period: 1}},
		{http.MethodDelete, "/api/timetable/constraints?id=tc-t06-2-1", nil},
		{http.MethodPost, "/api/timetable/constraints/batch", protocol.BatchConstraintRequest{TargetScope: "DEPT_HEADS", DayOfWeek: 5, Period: 6}},
		{http.MethodPost, "/api/timetable/scenarios", map[string]string{"slotCode": "A", "name": "불법저장"}},
		{http.MethodDelete, "/api/timetable/scenarios?id=scenario-A", nil},
		{http.MethodPost, "/api/timetable/scenarios/apply", map[string]string{"scenarioId": "scenario-A"}},
		{http.MethodPost, "/api/timetable/undo", nil},
		{http.MethodPost, "/api/timetable/redo", nil},
	}
	for _, fc := range forbiddenCalls {
		if code, _ := doRecorderReq(t, h, fc.method, fc.path, tokTeacher, fc.body); code != http.StatusForbidden {
			t.Fatalf("일반 교사 쓰기(%s %s)가 403으로 차단되지 않았습니다: status=%d", fc.method, fc.path, code)
		}
	}

	// 2. 시간표 담당교사: 제약 단건·일괄 저장 -> 조회 -> 삭제
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/timetable/constraints", tokAdmin, protocol.TeacherConstraint{
		TeacherID: "t06", DayOfWeek: 2, Period: 1, ConstraintType: protocol.ConstraintHardAvoid, Reason: "연구일",
	})
	if code != http.StatusOK {
		t.Fatalf("제약 단건 등록 실패: status=%d, body=%s", code, string(raw))
	}
	var savedC protocol.TeacherConstraint
	if err := json.Unmarshal(raw, &savedC); err != nil {
		t.Fatalf("제약 응답 파싱 실패: %v", err)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/constraints/batch", tokAdmin, protocol.BatchConstraintRequest{
		TargetScope: "DEPT_HEADS", GroupTag: "부장회의", DayOfWeek: 5, Period: 6, ConstraintType: protocol.ConstraintMeetingFree, Reason: "회의",
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"count":3`) {
		t.Fatalf("부장회의 일괄 등록 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/constraints", tokAdmin, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), savedC.ID) {
		t.Fatalf("제약 목록 조회 실패: status=%d", code)
	}
	if code, _ := doRecorderReq(t, h, http.MethodDelete, "/api/timetable/constraints?id="+savedC.ID, tokAdmin, nil); code != http.StatusOK {
		t.Fatalf("제약 삭제 실패: status=%d", code)
	}

	// 3. 시나리오 A 저장 -> 조회 -> 셀 변경 -> 채택 적용 -> 삭제
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/scenarios", tokAdmin, map[string]string{
		"slotCode": "A", "name": "1안 기본", "description": "초기 스냅샷",
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"id":"scenario-A"`) {
		t.Fatalf("시나리오 저장 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/scenarios", tokAdmin, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), "scenario-A") || !strings.Contains(string(raw), "currentMetrics") {
		t.Fatalf("시나리오 목록 조회 실패: status=%d, body=%s", code, string(raw))
	}

	// 4. 셀 저장 -> history(canUndo:true) -> undo -> redo 왕복 검증
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/cells", tokAdmin, protocol.TimetableCell{
		ID: "api-undo-cell", DayOfWeek: 4, Period: 5, ClassName: "3-2", Grade: 3, Subject: "역사", TeacherID: "t10",
	})
	if code != http.StatusOK {
		t.Fatalf("테스트 셀 저장 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/history", tokAdmin, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"canUndo":true`) {
		t.Fatalf("히스토리 상태 조회 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/undo", tokAdmin, nil)
	if code != http.StatusOK || strings.Contains(string(raw), "api-undo-cell") {
		t.Fatalf("API Undo 실행 후 추가 셀이 남아있습니다: status=%d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/redo", tokAdmin, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), "api-undo-cell") {
		t.Fatalf("API Redo 실행 후 셀이 복원되지 않았습니다: status=%d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/scenarios/apply", tokAdmin, map[string]string{
		"scenarioId": "scenario-A",
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"canUndo":true`) {
		t.Fatalf("시나리오 적용 실패: status=%d, body=%s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/cells", tokAdmin, nil)
	if code != http.StatusOK || strings.Contains(string(raw), "api-undo-cell") {
		t.Fatalf("시나리오 A 적용 후 정규 시간표가 A안으로 되돌아가지 않았습니다: status=%d", code)
	}
}
