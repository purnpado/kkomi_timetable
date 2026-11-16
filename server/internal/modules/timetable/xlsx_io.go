package timetable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ExportTeachersXLSX는 교사 명렬표 전체를 엑셀(.xlsx) 바이트로 내보냅니다.
func (s *Service) ExportTeachersXLSX() ([]byte, error) {
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "교사명렬표"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headers := []string{
		"ID", "아이디", "성명", "담당과목", "단축과목명", "직위",
		"담임학급", "부장여부(Y/N)", "전용교과교실", "연락처", "전입일(YYYY-MM-DD)", "출강요일(1~5)", "면제사유",
	}
	for c, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	rowIdx := 2
	for _, t := range teachers {
		if t.IsParentHonorary {
			continue
		}
		headYN := "N"
		if t.IsDepartmentHead {
			headYN = "Y"
		}
		var days []string
		for _, d := range t.ItinerantDays {
			days = append(days, strconv.Itoa(d))
		}
		vals := []interface{}{
			t.ID, t.Username, t.Name, t.Subject, t.ShortSubject, t.Position,
			t.HomeroomClass, headYN, t.DefaultRoom, t.Phone, t.TransferDate,
			strings.Join(days, ","), strings.Join(t.ExemptReasons, ","),
		}
		for c, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(c+1, rowIdx)
			_ = f.SetCellValue(sheet, cell, v)
		}
		rowIdx++
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportTeachersXLSX는 엑셀(.xlsx) 교사 명렬표를 읽어 신규 등록 및 기존 정보를 일괄 갱신합니다.
func (s *Service) ImportTeachersXLSX(r io.Reader) (int, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return 0, fmt.Errorf("엑셀 파일 열기 실패: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		return 0, errors.New("유효한 교사 데이터 행이 없습니다")
	}

	existingList, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	byID := make(map[string]protocol.Teacher, len(existingList))
	for _, t := range existingList {
		byID[t.ID] = t
	}
	defaultHashBytes, _ := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.MinCost)
	defaultHash := string(defaultHashBytes)

	imported := 0
	for _, row := range rows[1:] {
		if len(row) < 4 {
			continue
		}
		getCol := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}
		tid := getCol(0)
		username := getCol(1)
		name := getCol(2)
		subject := getCol(3)
		if name == "" || subject == "" {
			continue
		}
		if username == "" {
			username = name
		}
		if tid == "" {
			tid = "t-" + username
		}

		prev, exists := byID[tid]
		pwHash := defaultHash
		mustChange := true
		roles := []protocol.Role{protocol.RoleTeacher}
		if exists {
			if prev.PasswordHash != "" {
				pwHash = prev.PasswordHash
			}
			mustChange = prev.MustChangePassword
			if len(prev.Roles) > 0 {
				roles = prev.Roles
			}
		}

		var itinerant []int
		for _, part := range strings.Split(getCol(11), ",") {
			if d, convErr := strconv.Atoi(strings.TrimSpace(part)); convErr == nil && d >= 1 && d <= 5 {
				itinerant = append(itinerant, d)
			}
		}
		var exempt []string
		for _, part := range strings.Split(getCol(12), ",") {
			if p := strings.TrimSpace(part); p != "" {
				exempt = append(exempt, p)
			}
		}

		t := protocol.Teacher{
			ID:                 tid,
			Username:           username,
			PasswordHash:       pwHash,
			MustChangePassword: mustChange,
			Name:               name,
			Subject:            subject,
			ShortSubject:       getCol(4),
			Position:           orDefault(getCol(5), "평교사"),
			HomeroomClass:      getCol(6),
			IsDepartmentHead:   strings.EqualFold(getCol(7), "Y") || getCol(7) == "예",
			DefaultRoom:        getCol(8),
			Phone:              getCol(9),
			TransferDate:       getCol(10),
			ItinerantDays:      itinerant,
			ExemptReasons:      exempt,
			Roles:              roles,
		}
		if err := s.store.SaveRecord("teachers", t.ID, t); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

// ExportTeachingLoadsXLSX는 컴시간 표준 형식의 교사별·학급별 시수표 엑셀(.xlsx)을 생성합니다.
func (s *Service) ExportTeachingLoadsXLSX() ([]byte, error) {
	cfg, _ := s.GetSchoolConfig()
	loads, err := s.ListTeachingLoads()
	if err != nil {
		return nil, err
	}

	classCols := collectOrderedClasses(cfg, loads)
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := "교사별시수표"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	baseHeaders := []string{"순", "학년", "정식과목명", "단축과목명", "교사명", "교과교실(특별실)", "연속시수", "동시그룹"}
	allHeaders := append(append([]string{}, baseHeaders...), classCols...)
	allHeaders = append(allHeaders, "계")

	for c, h := range allHeaders {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	for rIdx, ld := range loads {
		row := rIdx + 2
		fixed := []interface{}{
			rIdx + 1, ld.Grade, ld.Subject, ld.ShortSubject, ld.TeacherName,
			ld.DedicatedRoom, ld.BlockHours, ld.SimultaneousGroup,
		}
		for c, v := range fixed {
			cell, _ := excelize.CoordinatesToCellName(c+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}
		total := 0
		for cIdx, cls := range classCols {
			hrs := ld.ClassHours[cls]
			total += hrs
			if hrs > 0 {
				cell, _ := excelize.CoordinatesToCellName(len(baseHeaders)+cIdx+1, row)
				_ = f.SetCellValue(sheet, cell, hrs)
			}
		}
		sumCell, _ := excelize.CoordinatesToCellName(len(allHeaders), row)
		_ = f.SetCellValue(sheet, sumCell, total)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportTeachingLoadsXLSX는 교사별·학급별 시수표 엑셀(.xlsx)을 읽어 일괄 저장합니다.
func (s *Service) ImportTeachingLoadsXLSX(r io.Reader) (int, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return 0, fmt.Errorf("엑셀 파일 열기 실패: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		return 0, errors.New("시수표 데이터 행이 필요합니다")
	}

	header := rows[0]
	const fixedCols = 8
	var classCols []struct {
		Idx  int
		Name string
	}
	for i := fixedCols; i < len(header); i++ {
		col := strings.TrimSpace(header[i])
		if col == "" || col == "계" || col == "합계" {
			continue
		}
		classCols = append(classCols, struct {
			Idx  int
			Name string
		}{Idx: i, Name: col})
	}

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	findTeacherID := func(tName, subj string) string {
		var nameMatches []protocol.Teacher
		for _, t := range teachers {
			if t.Name == tName || t.Username == tName || t.ID == tName {
				nameMatches = append(nameMatches, t)
			}
		}
		if len(nameMatches) == 1 {
			return nameMatches[0].ID
		}
		for _, t := range nameMatches {
			if strings.EqualFold(t.Subject, subj) {
				return t.ID
			}
		}
		if len(nameMatches) > 0 {
			return nameMatches[0].ID
		}
		return ""
	}

	imported := 0
	for idx, row := range rows[1:] {
		if len(row) < 5 {
			continue
		}
		getCol := func(i int) string {
			if i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		grade, _ := strconv.Atoi(getCol(1))
		subject := getCol(2)
		shortSubj := getCol(3)
		tName := getCol(4)
		if subject == "" || tName == "" {
			continue
		}
		tid := findTeacherID(tName, subject)
		if tid == "" {
			return imported, fmt.Errorf("%d행의 교사(%s)를 명렬표에서 찾을 수 없습니다", idx+2, tName)
		}
		room := getCol(5)
		blk, _ := strconv.Atoi(getCol(6))
		if blk <= 0 {
			blk = 1
		}
		simGrp := getCol(7)

		classHours := make(map[string]int)
		for _, cc := range classCols {
			if valStr := getCol(cc.Idx); valStr != "" {
				if hrs, convErr := strconv.Atoi(valStr); convErr == nil && hrs > 0 {
					classHours[cc.Name] = hrs
				}
			}
		}
		if len(classHours) == 0 {
			continue
		}

		if _, err := s.SaveTeachingLoad(protocol.TeachingLoad{
			TeacherID:         tid,
			TeacherName:       tName,
			Subject:           subject,
			ShortSubject:      shortSubj,
			Grade:             grade,
			ClassHours:        classHours,
			BlockHours:        blk,
			SimultaneousGroup: simGrp,
			DedicatedRoom:     room,
		}); err != nil {
			return imported, fmt.Errorf("%d행 저장 오류: %w", idx+2, err)
		}
		imported++
	}
	return imported, nil
}

func collectOrderedClasses(cfg protocol.SchoolConfig, loads []protocol.TeachingLoad) []string {
	seen := make(map[string]bool)
	var list []string
	for g := 1; g <= 3; g++ {
		for c := 1; c <= cfg.GradeClassCounts[g]; c++ {
			cls := fmt.Sprintf("%d-%d", g, c)
			seen[cls] = true
			list = append(list, cls)
		}
	}
	var extras []string
	for _, ld := range loads {
		for cls := range ld.ClassHours {
			if !seen[cls] {
				seen[cls] = true
				extras = append(extras, cls)
			}
		}
	}
	sort.Strings(extras)
	return append(list, extras...)
}

func orDefault(v, fb string) string {
	if strings.TrimSpace(v) == "" {
		return fb
	}
	return strings.TrimSpace(v)
}
