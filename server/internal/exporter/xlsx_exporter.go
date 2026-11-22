package exporter

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
)

// RequestTypeLabel은 시간표 변경 요청 유형 코드를 한국어 명칭으로 변환합니다.
func RequestTypeLabel(reqType string) string {
	switch reqType {
	case "SWAP":
		return "수업 맞교환"
	case "TRIANGLE":
		return "삼각 연쇄교환"
	case "QUAD":
		return "사각 연쇄교환"
	case "MOVE":
		return "빈 교시 이동"
	case "CROSS_WEEK_SWAP":
		return "주차간 맞교환"
	case "SUBSTITUTE":
		return "수업 대타/보강"
	case "ABSENCE_SUBSTITUTE":
		return "결근 일일보강"
	case "EXTERNAL_TRIP":
		return "외부감독 출장"
	case "EVENT_TRIP":
		return "행사 인솔출장"
	default:
		return reqType
	}
}

// StatusLabel은 결재 승인 상태 코드를 한국어 명칭으로 변환합니다.
func StatusLabel(st protocol.ApprovalStatus) string {
	switch st {
	case protocol.StatusStaging:
		return "승인 대기"
	case protocol.StatusApproved:
		return "확정 반영"
	case protocol.StatusRejected:
		return "반려"
	default:
		return string(st)
	}
}

// EventLinkTypeLabel은 학사 행사 연동 속성을 한국어 명칭으로 변환합니다.
func EventLinkTypeLabel(lt protocol.EventLinkType) string {
	switch lt {
	case protocol.EventLinkSimpleNotice:
		return "단순 안내"
	case protocol.EventLinkClassBlock:
		return "수업 블록"
	case protocol.EventLinkTeacherTrip:
		return "인솔 교사 출장"
	case protocol.EventLinkBellChange:
		return "시종시간 변경"
	case protocol.EventLinkHoliday:
		return "공휴일/휴업일"
	default:
		return string(lt)
	}
}

func newHeaderCellStyle(f *excelize.File) int {
	id, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#0F172A"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#E8EFF8"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "94A3B8", Style: 1},
			{Type: "right", Color: "94A3B8", Style: 1},
			{Type: "top", Color: "94A3B8", Style: 1},
			{Type: "bottom", Color: "64748B", Style: 2},
		},
	})
	return id
}

func newBodyCellStyle(f *excelize.File) int {
	id, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "CBD5E1", Style: 1},
			{Type: "right", Color: "CBD5E1", Style: 1},
			{Type: "top", Color: "CBD5E1", Style: 1},
			{Type: "bottom", Color: "CBD5E1", Style: 1},
		},
	})
	return id
}

func applyColumnWidths(f *excelize.File, sheet string, widths []float64) {
	for i, w := range widths {
		colName, err := excelize.ColumnNumberToName(i + 1)
		if err == nil {
			_ = f.SetColWidth(sheet, colName, colName, w)
		}
	}
}

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

	hStyle := newHeaderCellStyle(f)
	bStyle := newBodyCellStyle(f)
	headers := []string{"순번", "적용일자", "학급", "교시", "변경유형", "원수업(과목/교사)", "변경후(과목/교사)", "사유", "승인상태", "확정일시"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, hStyle)
	}
	applyColumnWidths(f, sheet, []float64{7, 13, 10, 7, 15, 22, 22, 28, 12, 19})

	for i, r := range reqs {
		row := i + 2
		orig := fmt.Sprintf("%s (%s)", r.SourceCell.Subject, r.RequesterName)
		after := fmt.Sprintf("%s (%s)", r.TargetCell.Subject, r.PartnerName)
		vals := []interface{}{
			i + 1, r.TargetDate, r.SourceCell.ClassName, r.SourceCell.Period,
			RequestTypeLabel(r.RequestType), orig, after, r.Reason, StatusLabel(r.Status), r.CommittedAt,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = f.SetCellValue(sheet, cell, v)
			_ = f.SetCellStyle(sheet, cell, cell, bStyle)
		}
	}

	// 요약 합계 행: 0건일 때 잘못된 COUNTA(A2:A1) 수식 방지
	summaryRow := len(reqs) + 3
	_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", summaryRow), "총 건수")
	if len(reqs) == 0 {
		_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", summaryRow), 0)
	} else {
		_ = f.SetCellFormula(sheet, fmt.Sprintf("B%d", summaryRow), fmt.Sprintf("COUNTA(A2:A%d)", len(reqs)+1))
	}

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

	hStyle := newHeaderCellStyle(f)
	bStyle := newBodyCellStyle(f)
	_ = f.SetCellValue(sheet, "A1", fmt.Sprintf("시험명: %s (%s) | 시험일: %s | 고사장: %s", notice.Title, notice.ExamCategory, notice.ExamDate, notice.Location))
	headers := []string{"순번", "직위", "성명", "담당과목", "연락처", "현임교 전입일", "구분(지원/차출)", "산정 근거"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 2)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, hStyle)
	}
	applyColumnWidths(f, sheet, []float64{7, 14, 12, 12, 16, 15, 14, 34})

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
			_ = f.SetCellStyle(sheet, cell, cell, bStyle)
		}
		seq++
		rowIdx++
	}
	if seq == 1 {
		_ = f.SetCellValue(sheet, "A3", "-")
		_ = f.SetCellValue(sheet, "B3", "아직 파견 명단이 확정되지 않았습니다.")
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

	hStyle := newHeaderCellStyle(f)
	bStyle := newBodyCellStyle(f)
	headers := []string{"순번", "행사일자", "교시", "주관부서", "행사명", "연동속성", "확정상태", "상세내용"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, hStyle)
	}
	applyColumnWidths(f, sheet, []float64{7, 13, 13, 15, 24, 15, 11, 34})

	for i, ev := range events {
		row := i + 2
		periodStr := fmt.Sprintf("%d~%d교시", ev.StartPeriod, ev.EndPeriod)
		if ev.StartPeriod == 0 {
			periodStr = "종일/해당없음"
		}
		vals := []interface{}{
			i + 1, ev.EventDate, periodStr, ev.Department, ev.Title,
			EventLinkTypeLabel(ev.LinkType), StatusLabel(ev.Status), ev.Description,
		}
		for col, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = f.SetCellValue(sheet, cell, v)
			_ = f.SetCellStyle(sheet, cell, cell, bStyle)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
