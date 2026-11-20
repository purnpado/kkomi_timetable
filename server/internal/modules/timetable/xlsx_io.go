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

// ImportTeachersXLSX는 엑셀(.xlsx) 교사 명렬표를 스마트 헤더 탐색·기존 통계 보존·원자적 트랜잭션으로 일괄 반영합니다.
func (s *Service) ImportTeachersXLSX(r io.Reader) (int, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return 0, fmt.Errorf("엑셀 파일 열기 실패: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	grid, err := readSheetExpandedGrid(f, sheet)
	if err != nil || len(grid) < 2 {
		return 0, errors.New("유효한 교사 데이터 행이 없습니다")
	}

	headerIdx := detectHeaderRow(grid, []string{"성명", "이름", "담당과목", "과목", "아이디"}, 2)
	if headerIdx < 0 {
		headerIdx = 0
	}
	colMap := buildColumnMap(grid[headerIdx], map[string][]string{
		"id":        {"ID"},
		"username":  {"아이디", "계정"},
		"name":      {"성명", "이름", "교사명"},
		"subject":   {"담당과목", "과목", "교과"},
		"short":     {"단축과목명", "단축과목"},
		"pos":       {"직위", "직책"},
		"homeroom":  {"담임학급", "담임"},
		"head":      {"부장여부", "부장"},
		"room":      {"전용교과교실", "교과교실", "전용실"},
		"phone":     {"연락처", "전화번호"},
		"transfer":  {"전입일", "현임교전입일"},
		"itinerant": {"출강요일", "순회요일"},
		"exempt":    {"면제사유"},
	})
	useColMap := len(colMap) >= 2
	colOr := func(k string, fb int) int {
		if idx, ok := colMap[k]; ok {
			return idx
		}
		if useColMap {
			return -1
		}
		return fb
	}

	existingList, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	byID := make(map[string]protocol.Teacher, len(existingList))
	byUsername := make(map[string]protocol.Teacher, len(existingList))
	for _, t := range existingList {
		byID[t.ID] = t
		if t.Username != "" {
			byUsername[strings.ToLower(strings.TrimSpace(t.Username))] = t
		}
	}
	defaultHashBytes, _ := bcrypt.GenerateFromPassword([]byte("1234"), database.BcryptCost())
	defaultHash := string(defaultHashBytes)

	var parsed []protocol.Teacher
	_, hasExemptCol := colMap["exempt"]
	for _, row := range grid[headerIdx+1:] {
		name := gridCell(row, colOr("name", 2))
		subject := gridCell(row, colOr("subject", 3))
		if name == "" || subject == "" {
			continue
		}
		tid := gridCell(row, colOr("id", 0))
		username := gridCell(row, colOr("username", 1))
		if username == "" {
			username = name
		}
		prev, exists := byID[tid]
		if !exists && username != "" {
			if p2, ok2 := byUsername[strings.ToLower(username)]; ok2 {
				prev, exists = p2, true
				if tid == "" {
					tid = p2.ID
				}
			}
		}
		if tid == "" {
			tid = "t-" + username
		}

		t := protocol.Teacher{
			ID:                 tid,
			Username:           username,
			PasswordHash:       defaultHash,
			MustChangePassword: true,
			Name:               name,
			Subject:            subject,
			ShortSubject:       gridCell(row, colOr("short", 4)),
			Position:           orDefault(gridCell(row, colOr("pos", 5)), "평교사"),
			HomeroomClass:      normalizeClassName(gridCell(row, colOr("homeroom", 6)), 0),
			IsDepartmentHead:   strings.EqualFold(gridCell(row, colOr("head", 7)), "Y") || gridCell(row, colOr("head", 7)) == "예" || gridCell(row, colOr("head", 7)) == "O",
			DefaultRoom:        gridCell(row, colOr("room", 8)),
			Phone:              gridCell(row, colOr("phone", 9)),
			TransferDate:       normalizeDateString(gridCell(row, colOr("transfer", 10))),
			ItinerantDays:      normalizeKoreanDays(gridCell(row, colOr("itinerant", 11))),
			Roles:              []protocol.Role{protocol.RoleTeacher},
		}
		if hasExemptCol {
			for _, part := range strings.Split(gridCell(row, colMap["exempt"]), ",") {
				if p := strings.TrimSpace(part); p != "" {
					t.ExemptReasons = append(t.ExemptReasons, p)
				}
			}
		}
		if exists {
			if prev.PasswordHash != "" {
				t.PasswordHash = prev.PasswordHash
			}
			t.MustChangePassword = prev.MustChangePassword
			if len(prev.Roles) > 0 {
				t.Roles = prev.Roles
			}
			t.LastExternalDispatch = prev.LastExternalDispatch
			t.ExternalExamCount = prev.ExternalExamCount
			t.InternalExamCount = prev.InternalExamCount
			t.SubstitutionCount = prev.SubstitutionCount
			t.AnnualMainCount = prev.AnnualMainCount
			t.AnnualSubCount = prev.AnnualSubCount
			t.AnnualCorridorCount = prev.AnnualCorridorCount
			t.InternalExamExempt = prev.InternalExamExempt
			t.InternalExamExemptReason = prev.InternalExamExemptReason
			t.IsParentHonorary = prev.IsParentHonorary
			t.ParentOfStudentClass = prev.ParentOfStudentClass
			if !hasExemptCol {
				t.ExemptReasons = prev.ExemptReasons
			}
			if protocol.IsSystemAdminAccount(prev) || strings.EqualFold(prev.Username, "admin") {
				t.Name, t.Subject, t.Position = prev.Name, prev.Subject, prev.Position
				t.Roles = prev.Roles
			}
		}
		parsed = append(parsed, t)
	}
	if len(parsed) == 0 {
		return 0, errors.New("반영할 유효한 교사 데이터가 없습니다")
	}

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		for _, t := range parsed {
			if sErr := tx.SaveRecord("teachers", t.ID, t); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTeacherUpdated,
			Title:   "엑셀 교사 명렬표 일괄 반영 완료",
			Message: fmt.Sprintf("총 %d명의 교원 정보가 갱신되었습니다", len(parsed)),
			Payload: map[string]int{"importedCount": len(parsed)},
		})
	}
	return len(parsed), nil
}

// ExportTeachingLoadsXLSX는 컴시간 표준 형식의 교사별·학급별 시수표 엑셀(.xlsx)을 =SUM() 수식과 함께 생성합니다.
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

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#0F172A"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#E2E8F0"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	for c, h := range allHeaders {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
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
		for cIdx, cls := range classCols {
			if hrs := ld.ClassHours[cls]; hrs > 0 {
				cell, _ := excelize.CoordinatesToCellName(len(baseHeaders)+cIdx+1, row)
				_ = f.SetCellValue(sheet, cell, hrs)
			}
		}
		sumCell, _ := excelize.CoordinatesToCellName(len(allHeaders), row)
		if len(classCols) > 0 {
			startCol, _ := excelize.CoordinatesToCellName(len(baseHeaders)+1, row)
			endCol, _ := excelize.CoordinatesToCellName(len(baseHeaders)+len(classCols), row)
			_ = f.SetCellFormula(sheet, sumCell, fmt.Sprintf("SUM(%s:%s)", startCol, endCol))
		} else {
			_ = f.SetCellValue(sheet, sumCell, 0)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportTeachingLoadsXLSX는 병합 2단 헤더(1학년/1반)·실학교 엑셀 시수표를 파싱해 전수 검증 후 원자적으로 일괄 저장합니다.
func (s *Service) ImportTeachingLoadsXLSX(r io.Reader) (int, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return 0, fmt.Errorf("엑셀 파일 열기 실패: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	grid, err := readSheetExpandedGrid(f, sheet)
	if err != nil || len(grid) < 2 {
		return 0, errors.New("시수표 데이터 행이 필요합니다")
	}

	headerIdx := detectHeaderRow(grid, []string{"정식과목명", "과목", "교사명", "담당교사", "학년"}, 2)
	if headerIdx < 0 {
		headerIdx = 0
	}
	colMap := buildColumnMap(grid[headerIdx], map[string][]string{
		"seq":     {"순", "번호"},
		"grade":   {"학년"},
		"subject": {"정식과목명", "과목", "교과"},
		"short":   {"단축과목명", "단축과목"},
		"teacher": {"교사명", "담당교사", "교사"},
		"room":    {"교과교실", "특별실", "전용실"},
		"block":   {"연속시수", "연강"},
		"sim":     {"동시그룹", "동시수업"},
	})
	useColMap := len(colMap) >= 2
	colOr := func(k string, fb int) int {
		if idx, ok := colMap[k]; ok {
			return idx
		}
		if useColMap {
			return -1
		}
		return fb
	}
	fixedIdx := map[int]bool{}
	for _, k := range []string{"seq", "grade", "subject", "short", "teacher", "room", "block", "sim"} {
		if idx, ok := colMap[k]; ok {
			fixedIdx[idx] = true
		}
	}

	type classCol struct {
		Idx  int
		Name string
	}
	var classCols []classCol
	for cIdx, raw := range grid[headerIdx] {
		if fixedIdx[cIdx] {
			continue
		}
		h := strings.TrimSpace(raw)
		if h == "" || h == "계" || h == "합계" || h == "총계" {
			continue
		}
		parentGrade := 0
		if headerIdx > 0 && cIdx < len(grid[headerIdx-1]) {
			parentGrade = extractGradePrefix(grid[headerIdx-1][cIdx])
		}
		clsName := normalizeClassName(h, parentGrade)
		if clsName != "" {
			classCols = append(classCols, classCol{Idx: cIdx, Name: clsName})
		}
	}

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachers))
	for _, t := range teachers {
		tMap[t.ID] = t
	}

	var parsed []protocol.TeachingLoad
	for rIdx := headerIdx + 1; rIdx < len(grid); rIdx++ {
		row := grid[rIdx]
		subject := gridCell(row, colOr("subject", 2))
		tName := gridCell(row, colOr("teacher", 4))
		if subject == "" || tName == "" {
			continue
		}
		tid, rErr := resolveTeacherSafe(teachers, tName, subject)
		if rErr != nil {
			return 0, fmt.Errorf("%d행 오류: %w", rIdx+1, rErr)
		}

		classHours := make(map[string]int)
		sum := 0
		for _, cc := range classCols {
			if hrs := normalizeHours(gridCell(row, cc.Idx)); hrs > 0 {
				classHours[cc.Name] = hrs
				sum += hrs
			}
		}
		if len(classHours) == 0 {
			continue
		}

		grade := extractGradePrefix(gridCell(row, colOr("grade", 1)))
		if grade < 1 || grade > 3 {
			for cls := range classHours {
				if g := parseGrade(cls, 0); g >= 1 && g <= 3 {
					grade = g
					break
				}
			}
			if grade < 1 || grade > 3 {
				grade = 1
			}
		}
		blk := normalizeHours(gridCell(row, colOr("block", 6)))
		if blk <= 0 {
			blk = 1
		}
		shortSubj := gridCell(row, colOr("short", 3))
		if shortSubj == "" {
			runes := []rune(subject)
			if len(runes) > 2 {
				shortSubj = string(runes[:2])
			} else {
				shortSubj = subject
			}
		}
		room := gridCell(row, colOr("room", 5))
		if room == "" && tMap[tid].DefaultRoom != "" {
			room = tMap[tid].DefaultRoom
		}
		simGrp := gridCell(row, colOr("sim", 7))
		id := fmt.Sprintf("ld-%d-%s-%s", grade, tid, strings.ReplaceAll(subject, " ", ""))
		if simGrp != "" || room != "" {
			clean := strings.NewReplacer(" ", "", "/", "-", "\\", "-").Replace
			id = fmt.Sprintf("%s-%s-%s", id, clean(simGrp), clean(room))
		}
		parsed = append(parsed, protocol.TeachingLoad{
			ID:                id,
			TeacherID:         tid,
			TeacherName:       tMap[tid].Name,
			Subject:           subject,
			ShortSubject:      shortSubj,
			SubjectCode:       subjectToCode(subject, grade),
			Grade:             grade,
			ClassHours:        classHours,
			TotalHours:        sum,
			BlockHours:        blk,
			SimultaneousGroup: simGrp,
			DedicatedRoom:     room,
		})
	}
	if len(parsed) == 0 {
		return 0, errors.New("반영할 유효한 시수표 행이 없습니다")
	}

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		for _, ld := range parsed {
			if sErr := tx.SaveRecord("teaching_loads", ld.ID, ld); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return len(parsed), nil
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
