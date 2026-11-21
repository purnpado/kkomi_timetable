package timetable

import (
	"bytes"
	"errors"
	"fmt"
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
	if protocol.IsSystemAdminAccount(teacher) || teacher.IsParentHonorary {
		return cell, fmt.Errorf("%s 계정은 정규 시간표에 배정할 수 없습니다", teacher.Name)
	}
	if cell.CoTeacherID != "" {
		coT, coOK := tMap[cell.CoTeacherID]
		if !coOK {
			return cell, fmt.Errorf("지정한 코티칭 교사(%s)를 찾을 수 없습니다", cell.CoTeacherID)
		}
		if protocol.IsSystemAdminAccount(coT) || coT.IsParentHonorary {
			return cell, fmt.Errorf("%s 계정은 코티칭 교사로 배정할 수 없습니다", coT.Name)
		}
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

	constraints, _ := s.ListConstraints()
	if reason := hardConstraintReason(cell.TeacherID, cell.DayOfWeek, cell.Period, constraints, tMap); reason != "" {
		return cell, errors.New(reason)
	}
	if cell.CoTeacherID != "" {
		if reason := hardConstraintReason(cell.CoTeacherID, cell.DayOfWeek, cell.Period, constraints, tMap); reason != "" {
			return cell, errors.New(reason)
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

	afterCells := make([]protocol.TimetableCell, 0, len(cells)+1)
	replaced := false
	for _, c := range cells {
		if c.ID == cell.ID {
			afterCells = append(afterCells, cell)
			replaced = true
			continue
		}
		afterCells = append(afterCells, c)
	}
	if !replaced {
		afterCells = append(afterCells, cell)
	}

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if hErr := recordHistoryTx(tx, fmt.Sprintf("셀 저장 (%s %d교시)", cell.ClassName, cell.Period), "관리자", cells, afterCells); hErr != nil {
			return hErr
		}
		return tx.SaveRecord("timetable_cells", cell.ID, cell)
	}); err != nil {
		return cell, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTimetableCellsUpdated,
			Title:   "기초 시간표 셀 저장",
			Message: fmt.Sprintf("%s반 %s %d교시 (%s) 저장", cell.ClassName, dayNames[cell.DayOfWeek], cell.Period, cell.Subject),
			Payload: cell,
		})
	}
	return cell, nil
}

// DeleteCell은 기초 시간표의 특정 교시 셀을 삭제합니다.
func (s *Service) DeleteCell(cellID string) error {
	if strings.TrimSpace(cellID) == "" {
		return errors.New("삭제할 셀 ID가 필요합니다")
	}
	beforeCells, _ := s.ListCells()
	afterCells := make([]protocol.TimetableCell, 0, len(beforeCells))
	for _, c := range beforeCells {
		if c.ID != cellID {
			afterCells = append(afterCells, c)
		}
	}
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if len(afterCells) < len(beforeCells) {
			if hErr := recordHistoryTx(tx, fmt.Sprintf("셀 삭제 (%s)", cellID), "관리자", beforeCells, afterCells); hErr != nil {
				return hErr
			}
		}
		return tx.DeleteRecord("timetable_cells", cellID)
	}); err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTimetableCellsUpdated,
			Title:   "기초 시간표 셀 삭제",
			Message: fmt.Sprintf("셀(%s)이 기초 시간표에서 삭제되었습니다", cellID),
			Payload: map[string]string{"deletedId": cellID},
		})
	}
	return nil
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
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	var regular []protocol.Teacher
	for _, t := range teachersList {
		tMap[t.ID] = t
		if !t.IsParentHonorary && !protocol.IsSystemAdminAccount(t) && protocol.HasRegularClasses(t) {
			regular = append(regular, t)
		}
	}
	if len(regular) == 0 {
		return nil, errors.New("배정할 수 있는 교사가 등록되어 있지 않습니다")
	}

	constraints, _ := s.ListConstraints()
	existing, _ := s.ListCells()
	beforeCells := existing
	var deleteIDs []string
	if clearExisting {
		for _, c := range existing {
			deleteIDs = append(deleteIDs, c.ID)
		}
		existing = nil
	}

	occupiedSlot := make(map[string]bool) // "class-day-period"
	teacherBusy := make(map[string]bool)  // "teacherID-day-period"
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

					// 후보 교사 정렬 (고정 공강 제외 → 기피 교시 후순위 → 부하 적은 순 → 담임 우선)
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
						if hardConstraintReason(t.ID, day, period, constraints, tMap) != "" {
							continue
						}
						cands = append(cands, t)
					}
					if len(cands) == 0 {
						continue
					}
					sort.Slice(cands, func(i, j int) bool {
						iSoft := softConstraintReason(cands[i].ID, day, period, constraints, tMap) != ""
						jSoft := softConstraintReason(cands[j].ID, day, period, constraints, tMap) != ""
						if iSoft != jSoft {
							return !iSoft
						}
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
	afterCells := make([]protocol.TimetableCell, 0, len(existing)+len(created))
	afterCells = append(afterCells, existing...)
	afterCells = append(afterCells, created...)
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if hErr := recordHistoryTx(tx, "기초 시간표 자동 생성", "관리자", beforeCells, afterCells); hErr != nil {
			return hErr
		}
		for _, id := range deleteIDs {
			if dErr := tx.DeleteRecord("timetable_cells", id); dErr != nil {
				return dErr
			}
		}
		for _, c := range created {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTimetableCellsUpdated,
			Title:   "기초 시간표 자동 편성 완료",
			Message: fmt.Sprintf("총 %d개 수업 셀이 편성되었습니다", len(created)),
			Payload: map[string]int{"createdCount": len(created)},
		})
	}
	return created, nil
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
	return wouldBlockExceedThree(slots, day, period, 1)
}

func wouldBlockExceedThree(slots map[int]bool, day, startP, hours int) bool {
	streak := 0
	for p := 1; p <= 7; p++ {
		inNew := p >= startP && p < startP+hours
		if inNew || (slots != nil && slots[day*10+p]) {
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
