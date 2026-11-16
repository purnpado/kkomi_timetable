package exporter

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
)

// BuildTimetableChangesXLSX는 나이스(NEIS) 입력 순서에 맞춘 결보강/수업교환 대장을 엑셀(.xlsx)로 생성합니다.
func BuildTimetableChangesXLSX(reqs []protocol.TimetableChangeRequest) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "나이스_결보강대장"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"순번", "적용일자", "학급", "교시", "변경유형", "원수업(과목/교사)", "변경후(과목/교사)", "사유", "승인상태", "확정일시"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	for i, r := range reqs {
		row := i + 2
		orig := fmt.Sprintf("%s (%s)", r.SourceCell.Subject, r.RequesterName)
		after := fmt.Sprintf("%s (%s)", r.TargetCell.Subject, r.PartnerName)
		vals := []interface{}{
			i + 1, r.TargetDate, r.SourceCell.ClassName, r.SourceCell.Period,
			r.RequestType, orig, after, r.Reason, string(r.Status), r.CommittedAt,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}

	// 요약 합계 수식 행 추가
	summaryRow := len(reqs) + 3
	_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", summaryRow), "총 건수")
	_ = f.SetCellFormula(sheet, fmt.Sprintf("B%d", summaryRow), fmt.Sprintf("COUNTA(A2:A%d)", len(reqs)+1))

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildExternalExamDispatchXLSX는 교육청 제출용 외부 시험감독 파견 명단을 엑셀(.xlsx)로 생성합니다.
// 계좌번호, 은행명, 수당 정산 등 금융/정산 필드는 실무 요구에 따라 제외합니다.
func BuildExternalExamDispatchXLSX(notice protocol.ExternalExamNotice, candidates []protocol.ExternalExamCandidate) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "외부감독_파견명단"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	_ = f.SetCellValue(sheet, "A1", fmt.Sprintf("시험명: %s (%s) | 시험일: %s | 고사장: %s", notice.Title, notice.ExamCategory, notice.ExamDate, notice.Location))
	headers := []string{"순번", "직위", "성명", "담당과목", "연락처", "현임교 전입일", "구분(지원/차출)", "산정 근거"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 2)
		_ = f.SetCellValue(sheet, cell, h)
	}

	rowIdx := 3
	seq := 1
	for _, c := range candidates {
		if !c.IsSelected {
			continue
		}
		kind := "차출 확정"
		if c.IsVolunteer {
			kind = "자발적 지원"
		}
		vals := []interface{}{
			seq, c.Teacher.Position, c.Teacher.Name, c.Teacher.Subject,
			c.Teacher.Phone, c.Teacher.TransferDate, kind, c.ReasonBadge,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx)
			_ = f.SetCellValue(sheet, cell, v)
		}
		seq++
		rowIdx++
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildEventsXLSX는 주중·월중 확정 행사 일람표를 엑셀(.xlsx)로 생성합니다.
func BuildEventsXLSX(events []protocol.SchoolEvent) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "주중월중_학사일정"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"순번", "행사일자", "교시", "주관부서", "행사명", "연동속성", "확정상태", "상세내용"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	for i, ev := range events {
		row := i + 2
		periodStr := fmt.Sprintf("%d~%d교시", ev.StartPeriod, ev.EndPeriod)
		if ev.StartPeriod == 0 {
			periodStr = "종일/해당없음"
		}
		vals := []interface{}{
			i + 1, ev.EventDate, periodStr, ev.Department, ev.Title, string(ev.LinkType), string(ev.Status), ev.Description,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
