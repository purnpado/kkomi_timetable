package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
)

// TestStage1APINonSubjectAndSpecialClass는 비교과 교원 저장·조회, 대타 후보 필터링 및
// 특수학급고사실 특수교사 전담 배정/대타 추천 API를 검증합니다.
func TestStage1APINonSubjectAndSpecialClass(t *testing.T) {
	_, h := newTestServerApp(t)
	tokAdmin := loginForToken(t, h, "admin", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")
	tokInternal := loginForToken(t, h, "internal", "1234")

	// 1. 비교과 교원(전문상담교사) 등록 및 조회 검증
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/teachers", tokAdmin, map[string]interface{}{
		"teacher": map[string]interface{}{
			"id":               "t-api-coun",
			"username":         "apicoun",
			"name":             "정상담",
			"subject":          "전문상담",
			"position":         "전문상담교사",
			"teacherType":      "COUNSELOR",
			"hasClasses":       false,
			"substituteExempt": true,
			"roles":            []string{"TEACHER"},
		},
		"password": "1234",
	})
	if code != http.StatusOK {
		t.Fatalf("비교과 교원 등록 API 실패: status=%d, body=%s", code, string(raw))
	}

	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/teachers", tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("교사 목록 조회 API 실패: status=%d", code)
	}
	var list []protocol.Teacher
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("교사 목록 역직렬화 실패: %v", err)
	}
	foundCoun := false
	for _, tc := range list {
		if tc.ID == "t-api-coun" {
			foundCoun = true
			if tc.TeacherType != protocol.TeacherTypeCounselor || protocol.HasRegularClasses(tc) || !protocol.IsSubstituteExcluded(tc) {
				t.Fatalf("비교과 교원 속성 저장 오류: %+v", tc)
			}
		}
	}
	if !foundCoun {
		t.Fatalf("등록한 비교과 교원이 목록에 없습니다")
	}

	// 2. 주중 미출근 대책 API에서 admin·비교과 교원이 대타 후보에서 제외되는지 검증
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/timetable/absence-plan", tokT06, map[string]interface{}{
		"teacherId": "t06", "startDate": "2026-11-18", "endDate": "2026-11-18", "startPeriod": 1, "endPeriod": 7, "reason": "병가",
	})
	if code != http.StatusOK {
		t.Fatalf("결근 대책 API 실패: status=%d, body=%s", code, string(raw))
	}
	var plans []protocol.AbsenceSlotPlan
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		t.Fatalf("결근 대책 응답 파싱 실패: err=%v, raw=%s", err, string(raw))
	}
	for _, pl := range plans {
		for _, sub := range pl.SubstituteOptions {
			if sub.TeacherID == "t01" || sub.TeacherID == "t15" || sub.TeacherID == "t16" || sub.TeacherID == "t-api-coun" {
				t.Fatalf("API 대타 후보에 시스템 관리자 또는 비교과 교원(%s)이 포함되었습니다", sub.TeacherName)
			}
		}
	}

	// 3. 특수학급고사실 포함 교내 지필고사 배정 및 특수교사 전담 확인
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/generate", tokInternal, map[string]interface{}{
		"config": map[string]interface{}{
			"id": "exam-api-special", "title": "API 특수학급고사실 테스트", "examDate": "2026-11-25",
			"isSealed": false, "instantRevealMode": true,
		},
		"specs": []map[string]interface{}{
			{"examDate": "2026-11-25", "period": 1, "grade": 1, "subject": "국어", "classRooms": []string{"1-1", "특수학급고사실"}},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("교내 시험감독 배정 API 실패: status=%d, body=%s", code, string(raw))
	}
	var slots []protocol.ExamSlot
	if err := json.Unmarshal(raw, &slots); err != nil {
		t.Fatalf("시험감독 슬롯 파싱 실패: %v", err)
	}
	var specialSlotID string
	for _, sl := range slots {
		if strings.Contains(sl.RoomName, "특수학급고사실") {
			specialSlotID = sl.ID
			if sl.TeacherID != "t15" || sl.SlotRole != "MAIN" {
				t.Fatalf("API 특수학급고사실 배정이 박특수(t15) 정감독이 아닙니다: %+v", sl)
			}
		}
	}
	if specialSlotID == "" {
		t.Fatalf("특수학급고사실 슬롯이 생성되지 않았습니다")
	}

	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/substitute/recommend?slotId="+specialSlotID, tokInternal, nil)
	if code != http.StatusOK {
		t.Fatalf("특수학급고사실 대타 추천 API 실패: status=%d, body=%s", code, string(raw))
	}
}
