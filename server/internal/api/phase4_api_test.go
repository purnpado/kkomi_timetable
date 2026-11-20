package api_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/internal_exam"
)

func readZipSectionText(t *testing.T, raw []byte, target string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("ZIP 파싱 실패: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == target {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			return string(b)
		}
	}
	t.Fatalf("ZIP 내 엔트리(%s)를 찾을 수 없습니다", target)
	return ""
}

func TestPhase4MultiDayExamAndSettlementAPI(t *testing.T) {
	_, h := newTestServerApp(t)
	tokTeacher := loginForToken(t, h, "t06", "1234")
	tokInternal := loginForToken(t, h, "internal", "1234")

	// 1. 다중일(2일) 지필고사 생성 후 1일차만 부분 공개
	day1, day2 := "2026-12-01", "2026-12-02"
	code, raw := doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/generate", tokInternal, map[string]interface{}{
		"config": protocol.ExamConfig{
			ID:                 "exam-api-2day",
			Title:              "API 다중일 테스트",
			ExamDate:           day1,
			ExamDates:          []string{day1, day2},
			UnsealTime:         "08:20",
			IsSealed:           true,
			AvoidConsecutive:   true,
			MaxDailyPerTeacher: 2,
		},
		"specs": []internal_exam.ExamPeriodSpec{
			{ExamDate: day1, Period: 1, Grade: 1, Subject: "국어", StartTime: "08:50", EndTime: "09:35", DurationMinutes: 45, ClassRooms: []string{"1-1", "1-2"}},
			{ExamDate: day2, Period: 1, Grade: 2, Subject: "수학", StartTime: "08:50", EndTime: "09:35", DurationMinutes: 45, ClassRooms: []string{"2-1", "2-2"}},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("다중일 시험감독 생성 실패: status=%d, body=%s", code, string(raw))
	}

	code, raw = doRecorderReq(t, h, http.MethodPost, "/api/internal-exam/unseal", tokInternal, map[string]string{
		"examDate": day1,
	})
	if code != http.StatusOK || !strings.Contains(string(raw), `"isSealed":true`) {
		t.Fatalf("1일차 부분 공개 오류: status=%d, body=%s", code, string(raw))
	}

	// 2. 일반 교사 vs 교내감독 관리자 봉인 마스킹 (조회 + HWPX + XLSX 모두 검증)
	code, rawTeacherSlots := doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", tokTeacher, nil)
	if code != http.StatusOK || !strings.Contains(string(rawTeacherSlots), "1-1 고사장") || strings.Contains(string(rawTeacherSlots), "2-1 고사장") {
		t.Fatalf("일반 교사 부분공개 마스킹 오류: %s", string(rawTeacherSlots))
	}
	code, rawAdminSlots := doRecorderReq(t, h, http.MethodGet, "/api/internal-exam/slots", tokInternal, nil)
	if code != http.StatusOK || !strings.Contains(string(rawAdminSlots), "2-1 고사장") {
		t.Fatalf("관리자 전체 고사장 조회 실패: %s", string(rawAdminSlots))
	}

	code, hwpxTeacher := doRecorderReq(t, h, http.MethodGet, "/api/export/hwpx?kind=internal-exam", tokTeacher, nil)
	if code != http.StatusOK {
		t.Fatalf("일반 교사 시험감독 HWPX 출력 실패: %d", code)
	}
	secTeacher := readZipSectionText(t, hwpxTeacher, "Contents/section0.xml")
	if !strings.Contains(secTeacher, "1-1 고사장") || strings.Contains(secTeacher, "2-1 고사장") {
		t.Fatalf("일반 교사용 시험감독 HWPX에서 미공개 일자 고사장이 노출되었습니다: %s", secTeacher)
	}

	code, xlsxTeacher := doRecorderReq(t, h, http.MethodGet, "/api/export/xlsx?kind=internal-exam", tokTeacher, nil)
	if code != http.StatusOK || len(xlsxTeacher) < 4 || string(xlsxTeacher[:2]) != "PK" {
		t.Fatalf("일반 교사 시험감독 XLSX 출력 실패: status=%d, len=%d", code, len(xlsxTeacher))
	}

	// 3. 통합 통계 조회(/api/stats/summary) 인증 및 정산 출력 검증
	if code, _ := doRecorderReq(t, h, http.MethodGet, "/api/stats/summary?mode=ALL", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("무인증 통계 조회가 401로 차단되지 않았습니다: %d", code)
	}
	code, sumBody := doRecorderReq(t, h, http.MethodGet, "/api/stats/summary?mode=ALL", tokTeacher, nil)
	if code != http.StatusOK || !strings.Contains(string(sumBody), `"periodLabel"`) || !strings.Contains(string(sumBody), `"teacherStats"`) {
		t.Fatalf("통합 통계 조회 실패: status=%d, body=%s", code, string(sumBody))
	}

	for _, ep := range []string{
		"/api/export/xlsx?kind=settlement&mode=ALL",
		"/api/export/hwpx?kind=settlement&mode=ALL",
		"/api/export/hwpx?kind=substitution-cert&teacherId=t06&mode=ALL",
	} {
		code, out := doRecorderReq(t, h, http.MethodGet, ep, tokTeacher, nil)
		if code != http.StatusOK || len(out) < 4 || string(out[:2]) != "PK" {
			t.Fatalf("정산 파일 다운로드(%s) 실패: status=%d, len=%d", ep, code, len(out))
		}
	}
	code, htmlOut := doRecorderReq(t, h, http.MethodGet, "/api/export/print?kind=settlement&mode=ALL", tokTeacher, nil)
	if code != http.StatusOK || !strings.Contains(string(htmlOut), "교무업무 통합 정산표") {
		t.Fatalf("정산 고대비 인쇄 출력 실패: status=%d", code)
	}
}
