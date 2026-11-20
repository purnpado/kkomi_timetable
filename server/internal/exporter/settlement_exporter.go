package exporter

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
)

func examRoleLabel(role string) string {
	switch role {
	case "MAIN":
		return "정감독"
	case "SUB":
		return "부감독"
	case "CORRIDOR":
		return "복도감독"
	case "HONORARY":
		return "학부모명예"
	default:
		return role
	}
}

func writeXLSXRow(f *excelize.File, sheet string, rowIdx int, vals []interface{}, style int) {
	for col, v := range vals {
		cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx)
		_ = f.SetCellValue(sheet, cell, v)
		_ = f.SetCellStyle(sheet, cell, cell, style)
	}
}

// BuildSettlementReportXLSX는 교원별·월별·결보강·행사 4개 시트로 구성된 교무업무 통합 정산표(.xlsx)를 생성합니다.
func BuildSettlementReportXLSX(report protocol.PeriodStatsReport) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	hStyle := newHeaderCellStyle(f)
	bStyle := newBodyCellStyle(f)

	// 시트 1: 교원별 통합 정산표
	s1 := "교원별_통합정산표"
	idx, err := f.NewSheet(s1)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	_ = f.SetCellValue(s1, "A1", fmt.Sprintf("[%s] 교무업무 통합 정산표 — %s", report.SchoolName, report.PeriodLabel))
	h1 := []string{"순번", "성명", "직위", "교과", "담임", "수업교환", "보강수행", "결보강요청", "교내정감독", "교내부감독", "교내복도", "교내합계", "외부파견", "행사참여", "총실적합계"}
	for c, h := range h1 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 2)
		_ = f.SetCellValue(s1, cell, h)
		_ = f.SetCellStyle(s1, cell, cell, hStyle)
	}
	applyColumnWidths(f, s1, []float64{6, 12, 14, 10, 9, 9, 9, 10, 10, 10, 9, 9, 9, 9, 11})

	for i, st := range report.TeacherStats {
		writeXLSXRow(f, s1, i+3, []interface{}{
			i + 1, st.TeacherName, st.Position, st.Subject, st.HomeroomClass,
			st.SwapCount, st.SubstituteGivenCount, st.SubstituteTakenCount,
			st.InternalMainCount, st.InternalSubCount, st.InternalCorrCount, st.InternalTotal,
			st.ExternalExamCount, st.EventLeadCount, st.TotalActivity,
		}, bStyle)
	}
	sumRow := len(report.TeacherStats) + 3
	_ = f.SetCellValue(s1, fmt.Sprintf("B%d", sumRow), "합계")
	for c := 6; c <= 15; c++ {
		colName, _ := excelize.ColumnNumberToName(c)
		addr := fmt.Sprintf("%s%d", colName, sumRow)
		if len(report.TeacherStats) == 0 {
			_ = f.SetCellValue(s1, addr, 0)
		} else {
			_ = f.SetCellFormula(s1, addr, fmt.Sprintf("SUM(%s3:%s%d)", colName, colName, len(report.TeacherStats)+2))
		}
	}

	// 시트 2: 월별 추이 요약
	s2 := "월별_추이요약"
	_, _ = f.NewSheet(s2)
	h2 := []string{"대상월", "수업변경(확정)", "결보강", "교내시험감독", "외부파견", "확정행사"}
	for c, h := range h2 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(s2, cell, h)
		_ = f.SetCellStyle(s2, cell, cell, hStyle)
	}
	applyColumnWidths(f, s2, []float64{12, 15, 11, 14, 11, 11})
	for i, tr := range report.MonthlyTrend {
		writeXLSXRow(f, s2, i+2, []interface{}{
			tr.Month, tr.TimetableChanges, tr.SubstituteSessions,
			tr.InternalExamSlots, tr.ExternalDispatches, tr.ApprovedEvents,
		}, bStyle)
	}
	if len(report.MonthlyTrend) == 0 {
		_ = f.SetCellValue(s2, "A2", "기간 내 집계된 월별 데이터가 없습니다.")
	}

	// 시트 3: 결보강·수업교환 확인 내역
	s3 := "결보강_확인내역"
	_, _ = f.NewSheet(s3)
	h3 := []string{"순번", "적용일", "학급", "교시", "변경유형", "원수업(요청교사)", "대체/보강교사", "사유", "승인상태"}
	for c, h := range h3 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(s3, cell, h)
		_ = f.SetCellStyle(s3, cell, cell, hStyle)
	}
	applyColumnWidths(f, s3, []float64{7, 13, 9, 7, 15, 22, 20, 28, 12})
	for i, rq := range report.ApprovedChanges {
		writeXLSXRow(f, s3, i+2, []interface{}{
			i + 1, rq.TargetDate, rq.SourceCell.ClassName, rq.SourceCell.Period,
			RequestTypeLabel(rq.RequestType),
			fmt.Sprintf("%s (%s)", rq.SourceCell.Subject, rq.RequesterName),
			fmt.Sprintf("%s (%s)", rq.TargetCell.Subject, rq.PartnerName),
			rq.Reason, StatusLabel(rq.Status),
		}, bStyle)
	}

	// 시트 4: 학사 행사 실적
	s4 := "학사행사_실적"
	_, _ = f.NewSheet(s4)
	h4 := []string{"순번", "일자", "교시", "주관부서", "행사명", "연동속성", "상태"}
	for c, h := range h4 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(s4, cell, h)
		_ = f.SetCellStyle(s4, cell, cell, hStyle)
	}
	applyColumnWidths(f, s4, []float64{7, 13, 12, 16, 26, 15, 11})
	for i, ev := range report.ApprovedEvents {
		pStr := fmt.Sprintf("%d~%d교시", ev.StartPeriod, ev.EndPeriod)
		if ev.StartPeriod == 0 {
			pStr = "종일/해당없음"
		}
		writeXLSXRow(f, s4, i+2, []interface{}{
			i + 1, ev.EventDate, pStr, ev.Department, ev.Title, EventLinkTypeLabel(ev.LinkType), StatusLabel(ev.Status),
		}, bStyle)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildInternalExamScheduleXLSX는 다중일 교내 지필고사 감독 배정표와 교원별 시수표를 엑셀(.xlsx)로 생성합니다.
func BuildInternalExamScheduleXLSX(cfg protocol.ExamConfig, slots []protocol.ExamSlot, fairness protocol.ExamFairnessSummary) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	hStyle := newHeaderCellStyle(f)
	bStyle := newBodyCellStyle(f)

	s1 := "시험감독_배정표"
	idx, err := f.NewSheet(s1)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	_ = f.SetCellValue(s1, "A1", fmt.Sprintf("시험명: %s | 기준일: %s | 감독기간: %d일 | 연강감독: %d건", cfg.Title, cfg.ExamDate, fairness.ExamDayCount, fairness.ConsecutivePairCount))
	h1 := []string{"순번", "시험일자", "교시", "시험시간", "학년", "과목", "역할", "배정 고사장/복도", "감독교사", "비고"}
	for c, h := range h1 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 2)
		_ = f.SetCellValue(s1, cell, h)
		_ = f.SetCellStyle(s1, cell, cell, hStyle)
	}
	applyColumnWidths(f, s1, []float64{7, 13, 7, 16, 7, 14, 11, 24, 12, 26})
	for i, sl := range slots {
		tRange := "-"
		if sl.StartTime != "" {
			tRange = fmt.Sprintf("%s~%s (%d분)", sl.StartTime, sl.EndTime, sl.DurationMinutes)
		}
		writeXLSXRow(f, s1, i+3, []interface{}{
			i + 1, sl.ExamDate, sl.Period, tRange, sl.Grade, sl.Subject,
			examRoleLabel(sl.SlotRole), sl.RoomName, sl.TeacherName, sl.Note,
		}, bStyle)
	}

	s2 := "교원별_감독시수표"
	_, _ = f.NewSheet(s2)
	h2 := []string{"순번", "성명", "교과", "담임", "제외여부", "기실시합계", "이번시험(정/부/복도)", "1년총합계"}
	for c, h := range h2 {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(s2, cell, h)
		_ = f.SetCellStyle(s2, cell, cell, hStyle)
	}
	applyColumnWidths(f, s2, []float64{7, 12, 12, 10, 18, 12, 19, 12})
	for i, st := range fairness.Stats {
		exemptStr := "감독 참여"
		if st.IsExempt {
			exemptStr = "제외 (" + st.ExemptReason + ")"
		}
		writeXLSXRow(f, s2, i+2, []interface{}{
			i + 1, st.TeacherName, st.Subject, st.HomeroomClass, exemptStr,
			st.BaseTotal, fmt.Sprintf("%d / %d / %d (%d)", st.CurrentMain, st.CurrentSub, st.CurrentCorridor, st.CurrentTotal), st.ProjectedTotal,
		}, bStyle)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildSettlementHWPXData는 기간별 교원 통합 정산 보고서용 HWPX 표 데이터를 생성합니다.
func BuildSettlementHWPXData(report protocol.PeriodStatsReport) HWPXDocumentData {
	rows := make([][]string, 0, len(report.TeacherStats))
	for i, st := range report.TeacherStats {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%s (%s)", st.TeacherName, st.Subject),
			fmt.Sprintf("교환 %d / 보강 %d", st.SwapCount, st.SubstituteGivenCount),
			fmt.Sprintf("%d (%d/%d/%d)", st.InternalTotal, st.InternalMainCount, st.InternalSubCount, st.InternalCorrCount),
			fmt.Sprintf("외부 %d / 행사 %d", st.ExternalExamCount, st.EventLeadCount),
			fmt.Sprintf("%d회", st.TotalActivity),
		})
	}
	return HWPXDocumentData{
		SchoolName: report.SchoolName,
		DocTitle:   fmt.Sprintf("교무업무 기간별 통합 정산 보고서 (%s)", report.PeriodLabel),
		PeriodText: fmt.Sprintf("기준: %s | 변경 %d · 교내감독 %d · 외부 %d · 행사 %d",
			report.PeriodLabel, report.TotalChanges, report.TotalInternal, report.TotalExternal, report.TotalEvents),
		Headers: []string{"순번", "교사 (과목)", "수업교환/보강", "교내감독(정/부/복도)", "외부/행사", "합계"},
		Rows:    rows,
	}
}

func matchesChangeTeacher(rq protocol.TimetableChangeRequest, teacherID string) bool {
	if teacherID == "" {
		return true
	}
	if rq.RequesterID == teacherID || rq.PartnerID == teacherID || rq.SourceCell.TeacherID == teacherID {
		return true
	}
	for _, step := range rq.ChainSteps {
		if step.TeacherID == teacherID {
			return true
		}
	}
	return false
}

// BuildSubstitutionCertHWPXData는 전체 또는 특정 교원의 결보강·수업교환 확인서용 HWPX 표 데이터를 생성합니다.
func BuildSubstitutionCertHWPXData(report protocol.PeriodStatsReport, teacherID string) HWPXDocumentData {
	rows := make([][]string, 0, len(report.ApprovedChanges))
	targetLabel := "전 교원"
	if teacherID != "" {
		targetLabel = teacherID
		for _, st := range report.TeacherStats {
			if st.TeacherID == teacherID {
				targetLabel = fmt.Sprintf("%s 선생님", st.TeacherName)
				break
			}
		}
	}
	seq := 1
	for _, rq := range report.ApprovedChanges {
		if !matchesChangeTeacher(rq, teacherID) {
			continue
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", seq),
			fmt.Sprintf("%s %s %d교시", rq.TargetDate, rq.SourceCell.ClassName, rq.SourceCell.Period),
			RequestTypeLabel(rq.RequestType),
			fmt.Sprintf("%s(%s) → %s(%s)", rq.SourceCell.Subject, rq.RequesterName, rq.TargetCell.Subject, rq.PartnerName),
			rq.Reason,
		})
		seq++
	}
	return HWPXDocumentData{
		SchoolName: report.SchoolName,
		DocTitle:   fmt.Sprintf("결보강 및 수업교환 실적 확인서 (%s)", targetLabel),
		PeriodText: fmt.Sprintf("조회기간: %s | 확정 내역 %d건", report.PeriodLabel, len(rows)),
		Headers:    []string{"순번", "일자/학급/교시", "변경유형", "원수업 → 변경/보강", "사유"},
		Rows:       rows,
	}
}

// BuildInternalExamHWPXData는 다중일 교내 지필고사 감독 배정표 HWPX 데이터를 생성합니다.
// 비관리자 요청 시에는 사전에 마스킹 처리된 slots를 전달받아 고사장 봉인을 유지합니다.
func BuildInternalExamHWPXData(schoolName string, cfg protocol.ExamConfig, slots []protocol.ExamSlot) HWPXDocumentData {
	rows := make([][]string, 0, len(slots))
	for i, sl := range slots {
		timeInfo := fmt.Sprintf("%s %d교시", sl.ExamDate, sl.Period)
		if sl.StartTime != "" {
			timeInfo = fmt.Sprintf("%s %d교시(%s~%s)", sl.ExamDate, sl.Period, sl.StartTime, sl.EndTime)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			timeInfo,
			fmt.Sprintf("%d학년 %s", sl.Grade, sl.Subject),
			fmt.Sprintf("%s / %s", examRoleLabel(sl.SlotRole), sl.RoomName),
			sl.TeacherName,
		})
	}
	return HWPXDocumentData{
		SchoolName: schoolName,
		DocTitle:   fmt.Sprintf("%s 감독 배정표", cfg.Title),
		PeriodText: fmt.Sprintf("시험일: %s | 총 %d슬롯", cfg.ExamDate, len(slots)),
		Headers:    []string{"순번", "시험일시", "학년·과목", "역할/고사장", "감독교사"},
		Rows:       rows,
	}
}
