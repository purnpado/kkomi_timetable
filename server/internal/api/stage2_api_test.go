package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"kkomi-timetable/pkg/protocol"
)

// TestStage2APIEndpointsAndPermissions는 전수 탐색, 사전 합의 동의, 일과계 즉시 반영,
// 창체 고정 편성, 3단계 워크플로우 잠금, 교과 제출·피드백 및 학기 시수 원장 REST API와 권한을 검증합니다.
func TestStage2APIEndpointsAndPermissions(t *testing.T) {
	_, h := newTestServerApp(t)
	tokAdmin := loginForToken(t, h, "admin", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 1. 정규 셀 조회 및 전수 탐색 API
	code, raw := doRecorderReq(t, h, http.MethodGet, "/api/timetable/cells", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("시간표 셀 조회 실패: %d", code)
	}
	var cells []protocol.TimetableCell
	if err := json.Unmarshal(raw, &cells); err != nil || len(cells) < 2 {
		t.Fatalf("시드 시간표 셀 부족: len=%d, err=%v", len(cells), err)
	}
	srcCell := cells[0]

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/explore-swaps", tokT06, map[string]interface{}{
		"sourceCellId":   srcCell.ID,
		"scope":          "DATE_ONLY",
		"weekStart":      "2026-11-23",
		"includeCrossWk": true,
		"maxCrossWeeks":  2,
		"includeBlocks":  true,
	})
	if code != http.StatusOK {
		t.Fatalf("전수 탐색 API 실패: status=%d, body=%s", code, string(raw))
	}
	var expResp protocol.ExploreSwapsResponse
	if err := json.Unmarshal(raw, &expResp); err != nil {
		t.Fatalf("전수 탐색 응답 파싱 실패: %v", err)
	}
	if expResp.SourceCell.ID != srcCell.ID || expResp.Scope != "DATE_ONLY" {
		t.Fatalf("전수 탐색 메타 오류: %+v", expResp.SourceCell)
	}

	// 2. 일반 교사의 autoApprove 시도 무시(STAGING 유지) + 사전 합의 보고 + 관리자 즉시 반영 검증
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/requests", tokT06, map[string]interface{}{
		"requestType": "MOVE",
		"scope":       "DATE_ONLY",
		"sourceDate":  "2026-11-23",
		"targetDate":  "2026-11-23",
		"partnerId":   "t02",
		"partnerName": "김수학",
		"autoApprove": true,
		"sourceCell":  srcCell,
		"targetCell":  map[string]interface{}{"dayOfWeek": srcCell.DayOfWeek, "period": 7, "className": srcCell.ClassName},
		"reason":      "일회성 이동 테스트",
	})
	if code != http.StatusOK {
		t.Fatalf("교사 신청 실패: status=%d, body=%s", code, string(raw))
	}
	var reqTeacher protocol.TimetableChangeRequest
	_ = json.Unmarshal(raw, &reqTeacher)
	if reqTeacher.Status != protocol.StatusStaging {
		t.Fatalf("일반 교사 요청은 STAGING이어야 합니다: got %s", reqTeacher.Status)
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/requests/agree", tokAdmin, map[string]interface{}{
		"requestId": reqTeacher.ID,
		"agreed":    true,
		"note":      "유선 사전 합의 확인",
	})
	if code != http.StatusOK {
		t.Fatalf("합의 동의 보고 API 실패: status=%d, body=%s", code, string(raw))
	}
	var agreedReq protocol.TimetableChangeRequest
	_ = json.Unmarshal(raw, &agreedReq)
	if !agreedReq.PartnerAgreed || agreedReq.AgreementNote != "유선 사전 합의 확인" {
		t.Fatalf("사전 합의 상태 미반영: %+v", agreedReq)
	}

	// 관리자 일과계 즉시 반영 (DATE_ONLY SUBSTITUTE)
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/requests", tokAdmin, map[string]interface{}{
		"requestType":   "SUBSTITUTE",
		"scope":         "DATE_ONLY",
		"sourceDate":    "2026-11-24",
		"targetDate":    "2026-11-24",
		"requesterId":   srcCell.TeacherID,
		"requesterName": "담당교사",
		"partnerId":     "t06",
		"partnerName":   "강국어",
		"partnerAgreed": true,
		"autoApprove":   true,
		"sourceCell":    srcCell,
		"targetCell":    srcCell,
		"reason":        "일과계 즉시 보강 반영",
	})
	if code != http.StatusOK {
		t.Fatalf("관리자 즉시 반영 실패: status=%d, body=%s", code, string(raw))
	}
	var instantReq protocol.TimetableChangeRequest
	_ = json.Unmarshal(raw, &instantReq)
	if instantReq.Status != protocol.StatusApproved {
		t.Fatalf("관리자 즉시 반영 요청이 APPROVED 상태가 아닙니다: %s", instantReq.Status)
	}

	// 3. 창체 동시/고정 편성 권한(교사 403, 관리자 200)
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/creative-activities", tokT06, map[string]interface{}{
		"slots": []map[string]interface{}{{"title": "창체", "grade": 1, "dayOfWeek": 5, "period": 6, "locked": true, "simultaneous": true}},
	})
	if code != http.StatusForbidden {
		t.Fatalf("일반 교사 창체 편성은 403이어야 합니다: got %d", code)
	}
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/creative-activities", tokAdmin, map[string]interface{}{
		"slots": []map[string]interface{}{{"title": "창체(동아리)", "grade": 1, "dayOfWeek": 5, "period": 6, "locked": true, "simultaneous": true}},
	})
	if code != http.StatusOK {
		t.Fatalf("창체 고정 편성 실패: status=%d, body=%s", code, string(raw))
	}

	// 4. 교과부장 시수 제출·검토 및 가편성 피드백 API
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/dept-submissions", tokT06, map[string]interface{}{
		"department": "국어과", "gradeScope": "1~3학년", "note": "시수 검토 완료",
	})
	if code != http.StatusOK {
		t.Fatalf("교과 시수 제출 실패: %d, %s", code, string(raw))
	}
	var sub protocol.DeptLoadSubmission
	_ = json.Unmarshal(raw, &sub)
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/dept-submissions/review", tokAdmin, map[string]interface{}{
		"id": sub.ID, "status": "APPROVED", "reviewNote": "확인 완료",
	})
	if code != http.StatusOK {
		t.Fatalf("교과 시수 검토 실패: %d", code)
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/feedbacks", tokT06, map[string]interface{}{
		"targetRef": "1-1 금 6교시", "category": "기피교시", "message": "창체 이동 검토 요청",
	})
	if code != http.StatusOK {
		t.Fatalf("피드백 등록 실패: %d", code)
	}
	var fb protocol.DraftFeedback
	_ = json.Unmarshal(raw, &fb)
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/feedbacks/resolve", tokAdmin, map[string]interface{}{
		"id": fb.ID, "resolution": "반영됨",
	})
	if code != http.StatusOK {
		t.Fatalf("피드백 완료 처리 실패: %d", code)
	}

	// 5. 워크플로우 최종 확정 잠금(LOCKED) 검증
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/workflow/phase", tokAdmin, map[string]interface{}{
		"phase": "LOCKED", "note": "최종 확정",
	})
	if code != http.StatusOK {
		t.Fatalf("워크플로우 잠금 실패: %d", code)
	}
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/cells", tokAdmin, map[string]interface{}{
		"dayOfWeek": 1, "period": 7, "className": "1-1", "grade": 1, "subject": "특강", "teacherId": "t02",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("LOCKED 상태에서 정규 셀 수정이 차단되어야 합니다: got %d", code)
	}
	_, _ = doRecorderReq(t, h, http.MethodPost, "/api/timetable/workflow/phase", tokAdmin, map[string]interface{}{
		"phase": "DRAFT_REVIEW", "note": "잠금 해제",
	})

	// 6. 학기 시수 원장 및 설정·부채 API 검증
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/ledger/config", tokAdmin, map[string]interface{}{
		"semesterName": "2026학년도 2학기", "totalWeeks": 16, "minRatioPct": 95, "includeCurrent": true,
	})
	if code != http.StatusOK {
		t.Fatalf("원장 기준 저장 실패: %d, %s", code, string(raw))
	}
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/timetable/ledger", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("학기 시수 원장 조회 실패: %d, %s", code, string(raw))
	}
	var ledger protocol.SemesterLedgerReport
	if err := json.Unmarshal(raw, &ledger); err != nil || ledger.Config.TotalWeeks != 16 || len(ledger.Rows) == 0 {
		t.Fatalf("학기 시수 원장 데이터 검증 실패: err=%v, cfg=%+v, rows=%d", err, ledger.Config, len(ledger.Rows))
	}
}
