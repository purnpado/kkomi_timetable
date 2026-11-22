package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"kkomi-timetable/pkg/protocol"
)

// TestStage3ExamAPIEndpointsAndPermissions는 3단계 교내 수동 맞교환, 개인별 감독 시간표,
// 외부감독 내규 설정, 확장 차출 규칙 및 공고 수정/삭제 API와 RBAC 권한을 검증합니다.
func TestStage3ExamAPIEndpointsAndPermissions(t *testing.T) {
	_, h := newTestServerApp(t)
	tokInternal := loginForToken(t, h, "internal", "1234")
	tokExternal := loginForToken(t, h, "external", "1234")
	tokT06 := loginForToken(t, h, "t06", "1234")

	// 1. 교내 시험 생성(동일 고사실 중복 금지 + 공강자습실 + GRADE_ONLY) 및 수동 맞교환 권한·동작 검증
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/generate", tokInternal, map[string]interface{}{
		"config": map[string]interface{}{
			"id":                    "api-exam-s3",
			"title":                 "3단계 API 검증 시험",
			"examDate":              "2026-12-07",
			"isSealed":              true,
			"unsealTime":            "08:20",
			"preventSameRoomRepeat": true,
			"subjectExcludeMode":    "GRADE_ONLY",
		},
		"specs": []map[string]interface{}{
			{
				"examDate": "2026-12-07", "period": 1, "grade": 1, "subject": "사회",
				"startTime": "08:50", "endTime": "09:35", "durationMinutes": 45,
				"classRooms":     []string{"1-1", "1-2", "별도고사실 1실(감염병)", "별도고사실 2실(감염병)"},
				"studyHallRooms": []string{"도서실(공강자습)"},
			},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("교내 시험 생성 API 실패: %d, %s", code, string(raw))
	}
	var genSlots []protocol.ExamSlot
	_ = json.Unmarshal(raw, &genSlots)

	var slotA, slotB protocol.ExamSlot
	for _, sl := range genSlots {
		if sl.RoomName == "1-1 고사장" {
			slotA = sl
		} else if sl.RoomName == "도서실(공강자습)" {
			slotB = sl
		}
	}
	if slotA.ID == "" || slotB.ID == "" {
		t.Fatalf("1-1 또는 공강자습실 슬롯이 생성되지 않았습니다: len=%d", len(genSlots))
	}

	// 일반 교사 수동 맞교환은 403 차단
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/swap", tokT06, map[string]interface{}{
		"slotId1": slotA.ID, "slotId2": slotB.ID,
	})
	if code != http.StatusForbidden {
		t.Fatalf("일반 교사의 감독 맞교환이 차단되지 않았습니다: got %d", code)
	}
	// 교내 시험감독 담당자 수동 맞교환 200 성공
	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/swap", tokInternal, map[string]interface{}{
		"slotId1": slotA.ID, "slotId2": slotB.ID,
	})
	if code != http.StatusOK {
		t.Fatalf("감독 수동 맞교환 실패: %d, %s", code, string(raw))
	}

	// 2. 개인별 감독 시간표 조회 API (일반 교사는 본인만 조회 & 봉인 시 마스킹, 관리자는 타 교사 조회 가능)
	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/personal?teacherId="+slotA.TeacherID, tokInternal, nil)
	if code != http.StatusOK {
		t.Fatalf("관리자 개인 감독 시간표 조회 실패: %d", code)
	}
	var psAdmin protocol.PersonalExamSchedule
	_ = json.Unmarshal(raw, &psAdmin)
	if psAdmin.TeacherID != slotA.TeacherID {
		t.Fatalf("관리자 지정 교사 조회 불일치: got %s, want %s", psAdmin.TeacherID, slotA.TeacherID)
	}

	code, raw = doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/personal?teacherId="+slotA.TeacherID, tokT06, nil)
	if code != http.StatusOK {
		t.Fatalf("일반 교사 개인 감독 시간표 조회 실패: %d", code)
	}
	var psTeacher protocol.PersonalExamSchedule
	_ = json.Unmarshal(raw, &psTeacher)
	if psTeacher.TeacherID != "t06" {
		t.Fatalf("일반 교사가 타인 개인 시간표를 조회할 수 없어야 합니다: got %s", psTeacher.TeacherID)
	}

	// 3. 외부 시험감독 학교 내규 설정 · 공고 수정/삭제 · 확장 규칙 API 검증
	code, _ = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/config", tokT06, map[string]interface{}{
		"defaultScoringMode": "WEIGHTED_SCORE",
	})
	if code != http.StatusForbidden {
		t.Fatalf("일반 교사의 외부 내규 설정이 차단되지 않았습니다: got %d", code)
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/config", tokExternal, map[string]interface{}{
		"categories": []map[string]interface{}{
			{"name": "검정고시", "weight": 1.0},
			{"name": "수능감독", "weight": 2.0},
			{"name": "임용고사", "weight": 1.5},
			{"name": "국가기술자격", "weight": 1.2},
		},
		"defaultScoringMode":       "WEIGHTED_SCORE",
		"defaultTransferGraceYear": true,
		"defaultTieBreaker":        "LOTTERY",
	})
	if code != http.StatusOK {
		t.Fatalf("외부 내규 설정 저장 실패: %d, %s", code, string(raw))
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/notices", tokExternal, map[string]interface{}{
		"title": "국가기술자격 시험감독", "examCategory": "국가기술자격", "examDate": "2026-12-15", "requiredCount": 2,
	})
	if code != http.StatusOK {
		t.Fatalf("외부 공고 생성 실패: %d, %s", code, string(raw))
	}
	var createdNotice protocol.ExternalExamNotice
	_ = json.Unmarshal(raw, &createdNotice)

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/external-exam/rules", tokExternal, map[string]interface{}{
		"noticeId":              createdNotice.ID,
		"presetMode":            "CUSTOM",
		"scoringMode":           "SEPARATE_TRACK",
		"priorityOrder":         []string{"VOLUNTEER", "LOW_COUNT", "EARLY_TRANSFER"},
		"excludeExempt":         true,
		"transferGraceYear":     true,
		"categoryExcludeRules":  []string{"GRADE3_HOMEROOM", "CONTRACT_TEACHER"},
		"tieBreaker":            "AGE_YOUNG",
		"volunteerOverflowMode": "FIRST_COME",
	})
	if code != http.StatusOK {
		t.Fatalf("외부 규칙 확장 업데이트 실패: %d, %s", code, string(raw))
	}

	code, _ = doRecorderReq(t, h, http.MethodPut, "/api/external-exam/notices", tokExternal, map[string]interface{}{
		"id": createdNotice.ID, "title": "국가기술자격 수정 공고", "requiredCount": 3,
	})
	if code != http.StatusOK {
		t.Fatalf("외부 공고 수정 실패: %d", code)
	}

	code, _ = doRecorderReq(t, h, http.MethodDelete, "/api/external-exam/notices?id="+createdNotice.ID, tokExternal, nil)
	if code != http.StatusOK {
		t.Fatalf("외부 공고 삭제 실패: %d", code)
	}
}
