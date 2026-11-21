package timetable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

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
