package exporter_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/pathutil"
	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/exporter"
)

func TestHWPXStandardOWPMLAndTemplateSafety(t *testing.T) {
	t.Setenv("KKOMI_BASE_DIR", t.TempDir())

	raw, err := exporter.BuildHWPXBytes(exporter.HWPXDocumentData{
		SchoolName: "새빛고등학교",
		DocTitle:   "2학기 학사일정 보고서",
		PeriodText: "2026년 11월",
		Headers:    []string{"순번", "일자", "부서", "행사명"},
		Rows:       [][]string{{"1", "2026-11-18", "교무부", "수능 <예비> & 소집"}},
	})
	if err != nil {
		t.Fatalf("HWPX 생성 실패: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("ZIP 파싱 실패: %v", err)
	}
	if len(zr.File) < 8 || zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Fatalf("첫 번째 ZIP 엔트리가 무압축(Store) mimetype이 아닙니다: %+v", zr.File[0])
	}

	expected := map[string]bool{
		"mimetype": true, "version.xml": true, "settings.xml": true,
		"META-INF/container.xml": true, "META-INF/manifest.xml": true,
		"Contents/content.hpf": true, "Contents/header.xml": true, "Contents/section0.xml": true,
	}
	found := map[string]string{}
	for _, f := range zr.File {
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatalf("ZIP 엔트리 열기 실패(%s): %v", f.Name, openErr)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		found[f.Name] = string(b)

		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".hpf") {
			dec := xml.NewDecoder(bytes.NewReader(b))
			for {
				if _, tokErr := dec.Token(); tokErr == io.EOF {
					break
				} else if tokErr != nil {
					t.Fatalf("XML 문법 오류(%s): %v", f.Name, tokErr)
				}
			}
		}
	}
	for req := range expected {
		if _, ok := found[req]; !ok {
			t.Fatalf("필수 HWPX 파일 누락: %s", req)
		}
	}
	if strings.TrimSpace(found["mimetype"]) != "application/hwp+zip" {
		t.Fatalf("mimetype 본문 오류: %q", found["mimetype"])
	}
	if !strings.Contains(found["Contents/header.xml"], "<hh:borderFills") || !strings.Contains(found["Contents/header.xml"], "<hh:charProperties") {
		t.Fatalf("header.xml에 필수 borderFills/charProperties가 누락되었습니다")
	}
	sec := found["Contents/section0.xml"]
	for _, marker := range []string{"<hp:secPr", "<hp:cellAddr", "<hp:cellSz", `height="1000"`, "새빛고등학교"} {
		if !strings.Contains(sec, marker) {
			t.Fatalf("section0.xml에 필수 OWPML 요소(%s)가 누락되었습니다", marker)
		}
	}

	// 사용자 커스텀 템플릿(<hp:linesegarray> 포함 + {{DOC_TITLE}}만 있고 {{ROW_CONTENT}} 없는 경우) 레이아웃 보존 및 linesegarray 제거 검증
	customSec := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><hs:sec xmlns:hs="http://www.hancom.co.kr/hwpml/2011/section" xmlns:hp="http://www.hancom.co.kr/hwpml/2011/paragraph"><hp:p><hp:run><hp:t>CUSTOM-LAYOUT {{SCHOOL_NAME}} {{DOC_TITLE}}</hp:t></hp:run><hp:linesegarray><hp:lineseg textpos="0"/></hp:linesegarray></hp:p></hs:sec>`
	var tplBuf bytes.Buffer
	tzw := zip.NewWriter(&tplBuf)
	mh := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	mw, _ := tzw.CreateHeader(mh)
	_, _ = mw.Write([]byte("application/hwp+zip"))
	for name, content := range map[string]string{
		"Contents/content.hpf": found["Contents/content.hpf"],
		"Contents/header.xml":  found["Contents/header.xml"],
		"Contents/section0.xml": customSec,
	} {
		w, _ := tzw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = tzw.Close()

	tplDir, _ := pathutil.TemplateDir()
	_ = os.WriteFile(pathutil.SafeJoin(tplDir, "학교표준_학사보고서_템플릿.hwpx"), tplBuf.Bytes(), 0o644)

	customOut, err := exporter.BuildHWPXBytes(exporter.HWPXDocumentData{
		SchoolName: "새빛고등학교",
		DocTitle:   "사용자양식제목",
	})
	if err != nil {
		t.Fatalf("커스텀 템플릿 치환 실패: %v", err)
	}
	czr, _ := zip.NewReader(bytes.NewReader(customOut), int64(len(customOut)))
	var outSec string
	for _, f := range czr.File {
		if f.Name == "Contents/section0.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			outSec = string(b)
		}
	}
	if !strings.Contains(outSec, "CUSTOM-LAYOUT 새빛고등학교 사용자양식제목") {
		t.Fatalf("커스텀 템플릿의 레이아웃과 치환 텍스트가 보존되지 않았습니다: %s", outSec)
	}
	if strings.Contains(outSec, "linesegarray") {
		t.Fatalf("커스텀 템플릿에서 <hp:linesegarray>가 제거되지 않았습니다: %s", outSec)
	}
}

func TestXLSXZeroRowsFormulaAndKoreanLabels(t *testing.T) {
	// 0건일 때 COUNTA(A2:A1) 깨진 수식 대신 0 출력 확인
	emptyBytes, err := exporter.BuildTimetableChangesXLSX(nil)
	if err != nil {
		t.Fatalf("빈 결보강 대장 생성 실패: %v", err)
	}
	if bytes.Contains(emptyBytes, []byte("COUNTA(A2:A1)")) {
		t.Fatalf("0건 엑셀 출력에 깨진 수식 COUNTA(A2:A1)가 포함되어 있습니다")
	}
	f0, _ := excelize.OpenReader(bytes.NewReader(emptyBytes))
	b3, _ := f0.GetCellValue("나이스_결보강대장", "B3")
	_ = f0.Close()
	if b3 != "0" {
		t.Fatalf("0건 요약 값이 0이 아닙니다: %q", b3)
	}

	// 데이터 존재 시 한글 라벨 및 COUNTA(A2:A2) 수식 확인
	dataBytes, err := exporter.BuildTimetableChangesXLSX([]protocol.TimetableChangeRequest{
		{
			RequestType: "SWAP", TargetDate: "2026-11-18",
			RequesterName: "강국어", PartnerName: "이수업",
			SourceCell: protocol.TimetableCell{ClassName: "1-1", Period: 1, Subject: "국어"},
			TargetCell: protocol.TimetableCell{Subject: "수학"},
			Reason:     "협의", Status: protocol.StatusApproved,
		},
	})
	if err != nil {
		t.Fatalf("데이터 결보강 대장 생성 실패: %v", err)
	}
	f1, _ := excelize.OpenReader(bytes.NewReader(dataBytes))
	typVal, _ := f1.GetCellValue("나이스_결보강대장", "E2")
	stVal, _ := f1.GetCellValue("나이스_결보강대장", "I2")
	sumFml, _ := f1.GetCellFormula("나이스_결보강대장", "B4")
	_ = f1.Close()
	if typVal != "수업 맞교환" || stVal != "확정 반영" {
		t.Fatalf("한글 라벨 변환 오류: type=%q, status=%q", typVal, stVal)
	}
	if sumFml != "COUNTA(A2:A2)" {
		t.Fatalf("요약 수식 오류: %q", sumFml)
	}
}

func TestSettlementAndInternalExamExporters(t *testing.T) {
	t.Setenv("KKOMI_BASE_DIR", t.TempDir())

	// 1. 0건 통합 정산표 XLSX 수식 방어 검증
	emptyReport := protocol.PeriodStatsReport{SchoolName: "새빛고", PeriodLabel: "2026년 10월"}
	emptyXLSX, err := exporter.BuildSettlementReportXLSX(emptyReport)
	if err != nil {
		t.Fatalf("빈 정산표 XLSX 생성 실패: %v", err)
	}
	f0, _ := excelize.OpenReader(bytes.NewReader(emptyXLSX))
	vF3, _ := f0.GetCellValue("교원별_통합정산표", "F3")
	_ = f0.Close()
	if vF3 != "0" {
		t.Fatalf("0건 정산표 요약 셀이 0이 아닙니다: %q", vF3)
	}

	// 2. 실데이터 통합 정산 XLSX/HWPX, 결보강 확인서 HWPX 및 다중일 시험감독 출력 검증
	sampleReport := protocol.PeriodStatsReport{
		SchoolName: "새빛고", PeriodLabel: "2026학년도 2학기", TotalChanges: 1, TotalSubstitutes: 1, TotalInternal: 1,
		TeacherStats: []protocol.TeacherPeriodStat{
			{TeacherID: "t06", TeacherName: "강국어", Subject: "국어", SwapCount: 1, SubstituteGivenCount: 1, InternalMainCount: 1, InternalTotal: 1, TotalActivity: 3},
		},
		MonthlyTrend: []protocol.MonthlyTrendRow{
			{Month: "2026-10", TimetableChanges: 1, SubstituteSessions: 1, InternalExamSlots: 1},
		},
		ApprovedChanges: []protocol.TimetableChangeRequest{
			{RequestType: "SUBSTITUTE", TargetDate: "2026-10-12", RequesterID: "t06", RequesterName: "강국어", PartnerID: "t07", PartnerName: "조수학", SourceCell: protocol.TimetableCell{ClassName: "1-1", Period: 1, Subject: "국어"}, TargetCell: protocol.TimetableCell{Subject: "수학"}, Reason: "연수", Status: protocol.StatusApproved},
		},
	}
	xlsxBytes, err := exporter.BuildSettlementReportXLSX(sampleReport)
	if err != nil {
		t.Fatalf("통합 정산표 XLSX 생성 실패: %v", err)
	}
	fx, _ := excelize.OpenReader(bytes.NewReader(xlsxBytes))
	sheets := fx.GetSheetList()
	_ = fx.Close()
	if len(sheets) != 4 {
		t.Fatalf("통합 정산표 시트 개수 불일치: %v", sheets)
	}
	if bytes.Contains(xlsxBytes, []byte("계좌")) || bytes.Contains(xlsxBytes, []byte("수당")) {
		t.Fatalf("정산표에 제외 대상 금융 키워드가 포함되었습니다")
	}

	for _, doc := range []exporter.HWPXDocumentData{
		exporter.BuildSettlementHWPXData(sampleReport),
		exporter.BuildSubstitutionCertHWPXData(sampleReport, "t06"),
		exporter.BuildInternalExamHWPXData("새빛고", protocol.ExamConfig{Title: "중간고사", ExamDate: "2026-10-20"}, []protocol.ExamSlot{
			{ExamDate: "2026-10-20", Period: 1, StartTime: "08:50", EndTime: "09:35", Grade: 1, Subject: "국어", SlotRole: "MAIN", RoomName: "🔒 당일 08:20 공개", TeacherName: "강국어"},
		}),
	} {
		raw, hErr := exporter.BuildHWPXBytes(doc)
		if hErr != nil {
			t.Fatalf("HWPX 생성 실패(%s): %v", doc.DocTitle, hErr)
		}
		zr, zErr := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if zErr != nil || len(zr.File) < 8 || zr.File[0].Name != "mimetype" {
			t.Fatalf("HWPX ZIP 구조 오류(%s): err=%v", doc.DocTitle, zErr)
		}
	}

	examXLSX, err := exporter.BuildInternalExamScheduleXLSX(
		protocol.ExamConfig{Title: "기말고사", ExamDate: "2026-11-23"},
		[]protocol.ExamSlot{{ExamDate: "2026-11-23", Period: 1, Grade: 1, Subject: "국어", SlotRole: "MAIN", RoomName: "1-1 고사장", TeacherName: "강국어"}},
		protocol.ExamFairnessSummary{ExamDayCount: 1, MaxDailyAssigned: 1},
	)
	if err != nil {
		t.Fatalf("시험감독 배정표 XLSX 생성 실패: %v", err)
	}
	fe, _ := excelize.OpenReader(bytes.NewReader(examXLSX))
	eSheets := fe.GetSheetList()
	_ = fe.Close()
	if len(eSheets) != 2 {
		t.Fatalf("시험감독 XLSX 시트 개수 불일치: %v", eSheets)
	}
}
