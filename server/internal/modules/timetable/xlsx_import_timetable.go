package timetable

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ImportTimetableXLSX는 실학교 엑셀(.xlsx) 기초 시간표를 스마트 헤더 탐색·전수 검증 후
// Undo 이력과 함께 원자적으로 일괄 반영합니다.
func (s *Service) ImportTimetableXLSX(r io.Reader) (int, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return 0, fmt.Errorf("엑셀 파일 열기 실패: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	if sheet == "" {
		return 0, errors.New("시트가 비어 있습니다")
	}
	grid, err := readSheetExpandedGrid(f, sheet)
	if err != nil || len(grid) < 2 {
		return 0, errors.New("데이터 행(헤더 제외 최소 1행)이 필요합니다")
	}

	headerIdx := detectHeaderRow(grid, []string{"학급", "반", "요일", "교시", "과목", "교사"}, 3)
	if headerIdx < 0 {
		headerIdx = 0
	}
	colMap := buildColumnMap(grid[headerIdx], map[string][]string{
		"grade":   {"학년"},
		"class":   {"학급", "반"},
		"day":     {"요일"},
		"period":  {"교시"},
		"subject": {"과목", "교과", "정식과목명"},
		"teacher": {"담당교사", "교사명", "교사", "성명"},
		"room":    {"교실", "특별실", "강의실"},
		"code":    {"나이스과목코드", "과목코드"},
	})
	useColMap := len(colMap) >= 3
	defCol := func(k string, fallback int) int {
		if idx, ok := colMap[k]; ok {
			return idx
		}
		if useColMap {
			return -1
		}
		return fallback
	}

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachers))
	for _, t := range teachers {
		tMap[t.ID] = t
	}
	constraints, _ := s.ListConstraints()
	existingCells, _ := s.ListCells()
	cellState := make(map[string]protocol.TimetableCell, len(existingCells))
	for _, c := range existingCells {
		cellState[c.ID] = c
	}

	var parsed []protocol.TimetableCell
	for rIdx := headerIdx + 1; rIdx < len(grid); rIdx++ {
		row := grid[rIdx]
		rawClass := gridCell(row, defCol("class", 1))
		rawDay := gridCell(row, defCol("day", 2))
		rawPeriod := gridCell(row, defCol("period", 3))
		subject := gridCell(row, defCol("subject", 4))
		teacherKey := gridCell(row, defCol("teacher", 5))
		if rawClass == "" && rawDay == "" && subject == "" && teacherKey == "" {
			continue
		}

		grade, _ := strconv.Atoi(gridCell(row, defCol("grade", 0)))
		className := normalizeClassName(rawClass, grade)
		if grade == 0 {
			grade = parseGrade(className, 1)
		}
		day := parseDayOfWeek(rawDay)
		period := normalizePeriod(rawPeriod)
		tid, rErr := resolveTeacherSafe(teachers, teacherKey, subject)
		if rErr != nil {
			return 0, fmt.Errorf("%d행 오류: %w", rIdx+1, rErr)
		}
		if className == "" || day < 1 || day > 5 || period < 1 || period > 7 || subject == "" {
			return 0, fmt.Errorf("%d행(%s %s)의 학급·요일·교시·과목 값이 유효하지 않습니다", rIdx+1, rawClass, subject)
		}
		teacher := tMap[tid]
		if len(teacher.ItinerantDays) > 0 && !slices.Contains(teacher.ItinerantDays, day) {
			return 0, fmt.Errorf("%d행 오류: 순회교사 %s 선생님은 해당 요일에 출강하지 않습니다", rIdx+1, teacher.Name)
		}
		if hr := hardConstraintReason(tid, day, period, constraints, tMap); hr != "" {
			return 0, fmt.Errorf("%d행 오류: %s", rIdx+1, hr)
		}
		room := gridCell(row, defCol("room", 6))
		if room == "" {
			if teacher.DefaultRoom != "" {
				room = teacher.DefaultRoom
			} else {
				room = className + "교실"
			}
		}
		subjectCode := gridCell(row, defCol("code", 7))
		if subjectCode == "" {
			subjectCode = subjectToCode(subject, grade)
		}
		cell := protocol.TimetableCell{
			ID: fmt.Sprintf("c-%d-%d-%s", day, period, className), DayOfWeek: day, Period: period,
			ClassName: className, Grade: grade, Subject: subject, SubjectCode: subjectCode, TeacherID: tid, Room: room,
		}
		for _, other := range cellState {
			if other.ID == cell.ID || (other.ClassName == cell.ClassName && other.DayOfWeek == cell.DayOfWeek && other.Period == cell.Period) {
				cell.ID = other.ID
				continue
			}
			if other.DayOfWeek == cell.DayOfWeek && other.Period == cell.Period {
				if other.TeacherID == cell.TeacherID || other.CoTeacherID == cell.TeacherID {
					return 0, fmt.Errorf("%d행 오류: %s 선생님이 동시간대 %s반과 중복됩니다", rIdx+1, teacher.Name, other.ClassName)
				}
				if cell.Room != cell.ClassName+"교실" && other.Room == cell.Room {
					return 0, fmt.Errorf("%d행 오류: 교과교실(%s)이 %s반과 중복됩니다", rIdx+1, cell.Room, other.ClassName)
				}
			}
		}
		cellState[cell.ID] = cell
		parsed = append(parsed, cell)
	}
	if len(parsed) == 0 {
		return 0, errors.New("반영할 유효한 시간표 행이 없습니다")
	}

	afterCells := make([]protocol.TimetableCell, 0, len(cellState))
	for _, c := range cellState {
		afterCells = append(afterCells, c)
	}
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if hErr := recordHistoryTx(tx, fmt.Sprintf("엑셀 기초시간표 일괄 업로드 (%d건)", len(parsed)), "시스템/관리자", existingCells, afterCells); hErr != nil {
			return hErr
		}
		for _, c := range parsed {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTimetableCellsUpdated,
			Title:   "엑셀 기초 시간표 일괄 반영 완료",
			Message: fmt.Sprintf("총 %d교시 수업이 반영되었습니다", len(parsed)),
			Payload: map[string]int{"importedCount": len(parsed)},
		})
	}
	return len(parsed), nil
}
