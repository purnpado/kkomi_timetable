package timetable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// SaveCell은 마스터 또는 시간표 담당교사가 학년별·학급별 기초 시간표의 특정 교시 셀을 생성·수정합니다.
func (s *Service) SaveCell(cell protocol.TimetableCell) (protocol.TimetableCell, error) {
	if cell.DayOfWeek < 1 || cell.DayOfWeek > 5 || cell.Period < 1 || cell.Period > 7 {
		return cell, errors.New("요일(1~5)과 교시(1~7)를 올바르게 지정해 주세요")
	}
	cell.ClassName = strings.TrimSpace(cell.ClassName)
	cell.Subject = strings.TrimSpace(cell.Subject)
	cell.TeacherID = strings.TrimSpace(cell.TeacherID)
	if cell.ClassName == "" || cell.Subject == "" || cell.TeacherID == "" {
		return cell, errors.New("학급, 과목, 담당교사는 필수 입력 항목입니다")
	}
	if cell.Grade == 0 {
		if g, err := strconv.Atoi(strings.Split(cell.ClassName, "-")[0]); err == nil && g > 0 {
			cell.Grade = g
		} else {
			cell.Grade = 1
		}
	}
	if cell.ID == "" {
		cell.ID = fmt.Sprintf("c-%d-%d-%s", cell.DayOfWeek, cell.Period, cell.ClassName)
	}

	teachersList, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return cell, err
	}
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}
	teacher, ok := tMap[cell.TeacherID]
	if !ok {
		return cell, fmt.Errorf("지정한 교사(%s)를 찾을 수 없습니다", cell.TeacherID)
	}
	if len(teacher.ItinerantDays) > 0 && !slices.Contains(teacher.ItinerantDays, cell.DayOfWeek) {
		return cell, fmt.Errorf("순회교사 %s 선생님은 해당 요일에 출강하지 않습니다", teacher.Name)
	}

	if cell.Room == "" {
		if teacher.DefaultRoom != "" {
			cell.Room = teacher.DefaultRoom
		} else {
			cell.Room = cell.ClassName + "교실"
		}
	}

	cells, _ := s.ListCells()
	// 같은 학급·요일·교시에 기존 셀이 있으면 덮어쓰기 대상으로 통일
	for _, existing := range cells {
		if existing.DayOfWeek == cell.DayOfWeek && existing.Period == cell.Period && existing.ClassName == cell.ClassName {
			cell.ID = existing.ID
			continue
		}
		if existing.ID != cell.ID && existing.DayOfWeek == cell.DayOfWeek && existing.Period == cell.Period {
			if existing.TeacherID == cell.TeacherID || existing.CoTeacherID == cell.TeacherID {
				return cell, fmt.Errorf("%s 선생님은 동시간대에 %s반(%s) 수업이 이미 배정되어 있습니다", teacher.Name, existing.ClassName, existing.Subject)
			}
			if cell.CoTeacherID != "" && (existing.TeacherID == cell.CoTeacherID || existing.CoTeacherID == cell.CoTeacherID) {
				coName := tMap[cell.CoTeacherID].Name
				return cell, fmt.Errorf("코티칭 교사 %s 선생님이 동시간대 %s반 수업 중입니다", coName, existing.ClassName)
			}
			if cell.Room != cell.ClassName+"교실" && existing.Room == cell.Room {
				return cell, fmt.Errorf("교과교실(%s)이 동시간대 %s반(%s)에서 이미 사용 중입니다", cell.Room, existing.ClassName, existing.Subject)
			}
		}
	}

	if err := s.store.SaveRecord("timetable_cells", cell.ID, cell); err != nil {
		return cell, err
	}
	return cell, nil
}

// DeleteCell은 기초 시간표의 특정 교시 셀을 삭제합니다.
func (s *Service) DeleteCell(cellID string) error {
	if strings.TrimSpace(cellID) == "" {
		return errors.New("삭제할 셀 ID가 필요합니다")
	}
	return s.store.DeleteRecord("timetable_cells", cellID)
}

// AutoGenerateBaseTimetable은 학년별 학급 수에 맞춰 교사 중복, 순회교사 출강일,
// 3연강 제한을 준수하는 학년·학급별 기초 정규 시간표를 자동 생성합니다.
func (s *Service) AutoGenerateBaseTimetable(grades []int, classesPerGrade int, clearExisting bool) ([]protocol.TimetableCell, error) {
	if len(grades) == 0 {
		grades = []int{1, 2, 3}
	}
	if classesPerGrade <= 0 {
		classesPerGrade = 2
	}
	if classesPerGrade > 15 {
		classesPerGrade = 15
	}

	teachersList, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	var regular []protocol.Teacher
	for _, t := range teachersList {
		if !t.IsParentHonorary {
			regular = append(regular, t)
		}
	}
	if len(regular) == 0 {
		return nil, errors.New("배정할 수 있는 교사가 등록되어 있지 않습니다")
	}

	existing, _ := s.ListCells()
	if clearExisting {
		for _, c := range existing {
			_ = s.store.DeleteRecord("timetable_cells", c.ID)
		}
		existing = nil
	}

	occupiedSlot := make(map[string]bool)    // "class-day-period"
	teacherBusy := make(map[string]bool)     // "teacherID-day-period"
	teacherDayStreak := make(map[string]map[int]bool)
	teacherLoad := make(map[string]int)

	for _, c := range existing {
		occupiedSlot[fmt.Sprintf("%s-%d-%d", c.ClassName, c.DayOfWeek, c.Period)] = true
		teacherBusy[fmt.Sprintf("%s-%d-%d", c.TeacherID, c.DayOfWeek, c.Period)] = true
		teacherLoad[c.TeacherID]++
		if teacherDayStreak[c.TeacherID] == nil {
			teacherDayStreak[c.TeacherID] = make(map[int]bool)
		}
		teacherDayStreak[c.TeacherID][c.DayOfWeek*10+c.Period] = true
	}

	var created []protocol.TimetableCell
	for _, g := range grades {
		for clsIdx := 1; clsIdx <= classesPerGrade; clsIdx++ {
			className := fmt.Sprintf("%d-%d", g, clsIdx)
			for day := 1; day <= 5; day++ {
				for period := 1; period <= 6; period++ {
					slotKey := fmt.Sprintf("%s-%d-%d", className, day, period)
					if occupiedSlot[slotKey] {
						continue
					}

					// 후보 교사 정렬 (부하 적은 순 + 동점 시 순환 다양성)
					cands := make([]protocol.Teacher, 0, len(regular))
					for _, t := range regular {
						if len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, day) {
							continue
						}
						if teacherBusy[fmt.Sprintf("%s-%d-%d", t.ID, day, period)] {
							continue
						}
						if wouldExceedThree(teacherDayStreak[t.ID], day, period) {
							continue
						}
						cands = append(cands, t)
					}
					if len(cands) == 0 {
						continue
					}
					sort.Slice(cands, func(i, j int) bool {
						if teacherLoad[cands[i].ID] != teacherLoad[cands[j].ID] {
							return teacherLoad[cands[i].ID] < teacherLoad[cands[j].ID]
						}
						// 담임 학급 수업 우선 배정 가중치
						iHomeroom := cands[i].HomeroomClass == className
						jHomeroom := cands[j].HomeroomClass == className
						if iHomeroom != jHomeroom {
							return iHomeroom
						}
						return cands[i].ID < cands[j].ID
					})

					picked := cands[0]
					cell := protocol.TimetableCell{
						ID:          fmt.Sprintf("c-%d-%d-%s", day, period, className),
						DayOfWeek:   day,
						Period:      period,
						ClassName:   className,
						Grade:       g,
						Subject:     picked.Subject,
						SubjectCode: subjectToCode(picked.Subject, g),
						TeacherID:   picked.ID,
						Room:        className + "교실",
					}
					if err := s.store.SaveRecord("timetable_cells", cell.ID, cell); err != nil {
						return created, err
					}
					created = append(created, cell)
					occupiedSlot[slotKey] = true
					teacherBusy[fmt.Sprintf("%s-%d-%d", picked.ID, day, period)] = true
					teacherLoad[picked.ID]++
					if teacherDayStreak[picked.ID] == nil {
						teacherDayStreak[picked.ID] = make(map[int]bool)
					}
					teacherDayStreak[picked.ID][day*10+period] = true
				}
			}
		}
	}
	return created, nil
}

// ImportTimetableXLSX는 엑셀(.xlsx) 기초 시간표 파일을 읽어 학년·학급별 기초 시간표에 일괄 반영합니다.
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
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		return 0, errors.New("데이터 행(헤더 제외 최소 1행)이 필요합니다")
	}

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	byNameOrID := make(map[string]string, len(teachers)*2)
	for _, t := range teachers {
		byNameOrID[t.ID] = t.ID
		byNameOrID[t.Name] = t.ID
		byNameOrID[t.Username] = t.ID
	}

	imported := 0
	for idx, row := range rows[1:] {
		if len(row) < 6 {
			continue
		}
		grade, _ := strconv.Atoi(strings.TrimSpace(row[0]))
		className := strings.TrimSpace(row[1])
		day := parseDayOfWeek(row[2])
		period, _ := strconv.Atoi(strings.TrimSpace(row[3]))
		subject := strings.TrimSpace(row[4])
		teacherKey := strings.TrimSpace(row[5])
		room := ""
		if len(row) >= 7 {
			room = strings.TrimSpace(row[6])
		}
		subjectCode := ""
		if len(row) >= 8 {
			subjectCode = strings.TrimSpace(row[7])
		}

		tid := byNameOrID[teacherKey]
		if tid == "" || className == "" || day == 0 || period == 0 || subject == "" {
			return imported, fmt.Errorf("%d행(%s %s)의 교사 또는 시간표 값이 유효하지 않습니다", idx+2, className, subject)
		}
		if _, err := s.SaveCell(protocol.TimetableCell{
			DayOfWeek:   day,
			Period:      period,
			ClassName:   className,
			Grade:       grade,
			Subject:     subject,
			SubjectCode: subjectCode,
			TeacherID:   tid,
			Room:        room,
		}); err != nil {
			return imported, fmt.Errorf("%d행 반영 오류: %w", idx+2, err)
		}
		imported++
	}
	return imported, nil
}

// BuildBaseTemplateXLSX는 기초 시간표 일괄 업로드용 표준 엑셀 양식을 생성합니다.
func BuildBaseTemplateXLSX() ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "기초시간표_업로드양식"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{"학년", "학급", "요일(월~금)", "교시", "과목", "담당교사(성명 또는 ID)", "교실", "나이스과목코드"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	sample := []interface{}{1, "1-3", "월", 7, "국어", "강국어", "1-3교실", "KOR101"}
	for i, v := range sample {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		_ = f.SetCellValue(sheet, cell, v)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseDayOfWeek(raw string) int {
	s := strings.TrimSpace(raw)
	switch s {
	case "월", "월요일", "1":
		return 1
	case "화", "화요일", "2":
		return 2
	case "수", "수요일", "3":
		return 3
	case "목", "목요일", "4":
		return 4
	case "금", "금요일", "5":
		return 5
	}
	return 0
}

func wouldExceedThree(slots map[int]bool, day, period int) bool {
	if slots == nil {
		return false
	}
	streak := 0
	for p := 1; p <= 7; p++ {
		if p == period || slots[day*10+p] {
			streak++
			if streak > 3 {
				return true
			}
		} else {
			streak = 0
		}
	}
	return false
}

func subjectToCode(subject string, grade int) string {
	switch strings.TrimSpace(subject) {
	case "국어":
		return fmt.Sprintf("KOR%d01", grade)
	case "수학":
		return fmt.Sprintf("MAT%d01", grade)
	case "영어":
		return fmt.Sprintf("ENG%d01", grade)
	case "사회":
		return fmt.Sprintf("SOC%d01", grade)
	case "과학":
		return fmt.Sprintf("SCI%d01", grade)
	case "역사":
		return fmt.Sprintf("HIS%d01", grade)
	case "정보":
		return fmt.Sprintf("INF%d01", grade)
	default:
		return fmt.Sprintf("SUB%d01", grade)
	}
}
