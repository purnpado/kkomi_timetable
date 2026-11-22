package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"kkomi-timetable/pkg/protocol"
)

// TestStage4CalendarAPIEndpointsAndPermissions는 4단계 학사일정 고도화(단축 프리셋·충돌 검사·행사 수정/삭제·RBAC) API를 검증합니다.
func TestStage4CalendarAPIEndpointsAndPermissions(t *testing.T) {
	_, h := newTestServerApp(t)
	tokEvent := loginForToken(t, h, "event", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 1. 단축수업 프리셋 조회
	code, raw := doRecorderReq(t, h, http.MethodGet, "/api/calendar/bell-presets", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("시종 프리셋 조회 실패: %d, %s", code, string(raw))
	}
	var presets []protocol.BellPresetDef
	_ = json.Unmarshal(raw, &presets)
	if len(presets) < 3 {
		t.Fatalf("프리셋 3종 이상이 반환되어야 합니다: %d", len(presets))
	}

	// 2. 일반 교사 다일·복수학년·장소 포함 행사 등록
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/calendar/events", tokT06, map[string]interface{}{
		"title":        "API 다일 수련회",
		"department":   "학생생활안전부",
		"eventDate":    "2026-12-21",
		"endDate":      "2026-12-23",
		"startPeriod":  1,
		"endPeriod":    6,
		"targetGrades": []int{1, 2},
		"leadTeachers": []string{"t06", "t07"},
		"location":     "강당",
		"linkType":     "CLASS_BLOCK",
	})
	if code != http.StatusOK {
		t.Fatalf("행사 등록 실패: %d, %s", code, string(raw))
	}
	var created protocol.SchoolEvent
	_ = json.Unmarshal(raw, &created)
	if created.EndDate != "2026-12-23" || created.Location != "강당" {
		t.Fatalf("다일/장소 필드 저장 누락: %+v", created)
	}

	// 3. 실시간 충돌 검사 API (동일 장소 + 학년 교시 겹침)
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/calendar/conflicts", tokT06, map[string]interface{}{
		"title":        "충돌 테스트 행사",
		"eventDate":    "2026-12-22",
		"startPeriod":  3,
		"endPeriod":    4,
		"targetGrades": []int{2},
		"location":     "강당",
		"linkType":     "CLASS_BLOCK",
	})
	if code != http.StatusOK {
		t.Fatalf("충돌 검사 API 실패: %d, %s", code, string(raw))
	}
	var conflicts []protocol.EventConflict
	_ = json.Unmarshal(raw, &conflicts)
	if len(conflicts) < 2 {
		t.Fatalf("장소 및 학년 교시 충돌 2건 이상이 반환되어야 합니다: %+v", conflicts)
	}

	// 4. 본인 대기 행사 수정 허용 및 확정 후 권한 분기 검증
	created.Location = "운동장"
	code, raw = doRecorderReq(t, h, http.MethodPut, "/api/calendar/events", tokT06, created)
	if code != http.StatusOK {
		t.Fatalf("본인 대기 행사 수정 실패: %d, %s", code, string(raw))
	}
	var updated protocol.SchoolEvent
	_ = json.Unmarshal(raw, &updated)
	if updated.Location != "운동장" {
		t.Fatalf("행사 장소 수정 미반영: %+v", updated)
	}

	// 행사계 확정 후 일반 교사는 수정/삭제 불가, 행사계는 수정/삭제 가능
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/calendar/commit", tokEvent, map[string]interface{}{
		"eventId": created.ID,
		"approve": true,
	})
	if code != http.StatusOK {
		t.Fatalf("행사 확정 실패: %d", code)
	}
	updated.Location = "강당"
	code, _ = doRecorderReq(t, h, http.MethodPut, "/api/calendar/events", tokT06, updated)
	if code == http.StatusOK {
		t.Fatalf("확정된 행사는 일반 교사가 수정할 수 없어야 합니다")
	}
	code, _ = doRecorderReq(t, h, http.MethodPut, "/api/calendar/events", tokEvent, updated)
	if code != http.StatusOK {
		t.Fatalf("행사계는 확정 행사도 수정할 수 있어야 합니다: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodDelete, "/api/calendar/events?id="+created.ID, tokT06, nil)
	if code != http.StatusForbidden {
		t.Fatalf("일반 교사 삭제는 403이어야 합니다: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodDelete, "/api/calendar/events?id="+created.ID, tokEvent, nil)
	if code != http.StatusOK {
		t.Fatalf("행사계 행사 삭제 실패: %d", code)
	}
}
