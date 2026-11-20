package timetable

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"

	"kkomi-timetable/pkg/protocol"
)

var (
	reParens     = regexp.MustCompile(`\([^)]*\)`)
	reGradeClass = regexp.MustCompile(`^0*(\d+)\s*(?:학년|-|\s)\s*0*(\d+)\s*반?$`)
	reClassOnly  = regexp.MustCompile(`^0*(\d+)\s*반$`)
	rePeriodNum  = regexp.MustCompile(`(\d+)`)
	reDateParts  = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)
)

// readSheetExpandedGrid는 시트의 모든 행과 병합 셀을 전개하여 2차원 문자열 그리드로 반환합니다.
func readSheetExpandedGrid(f *excelize.File, sheet string) ([][]string, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	merges, _ := f.GetMergeCells(sheet)

	maxRows := len(rows)
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	type mergeRect struct {
		c1, r1, c2, r2 int
		val            string
	}
	var rects []mergeRect
	for _, mc := range merges {
		c1, r1, err1 := excelize.CellNameToCoordinates(mc.GetStartAxis())
		c2, r2, err2 := excelize.CellNameToCoordinates(mc.GetEndAxis())
		if err1 != nil || err2 != nil {
			continue
		}
		if r2 > maxRows {
			maxRows = r2
		}
		if c2 > maxCols {
			maxCols = c2
		}
		rects = append(rects, mergeRect{c1: c1 - 1, r1: r1 - 1, c2: c2 - 1, r2: r2 - 1, val: strings.TrimSpace(mc.GetCellValue())})
	}

	grid := make([][]string, maxRows)
	for r := 0; r < maxRows; r++ {
		grid[r] = make([]string, maxCols)
		if r < len(rows) {
			for c := 0; c < len(rows[r]) && c < maxCols; c++ {
				grid[r][c] = strings.TrimSpace(rows[r][c])
			}
		}
	}
	for _, mr := range rects {
		val := mr.val
		if val == "" && mr.r1 < len(grid) && mr.c1 < len(grid[mr.r1]) {
			val = grid[mr.r1][mr.c1]
		}
		if val == "" {
			continue
		}
		for r := mr.r1; r <= mr.r2 && r < len(grid); r++ {
			for c := mr.c1; c <= mr.c2 && c < len(grid[r]); c++ {
				if grid[r][c] == "" {
					grid[r][c] = val
				}
			}
		}
	}
	return grid, nil
}

// detectHeaderRow는 상단 제목 배너 행들을 건너뛰고 실제 헤더 행 인덱스를 탐색합니다.
func detectHeaderRow(grid [][]string, keywords []string, minMatches int) int {
	bestIdx := -1
	bestScore := 0
	limit := len(grid)
	if limit > 15 {
		limit = 15
	}
	for r := 0; r < limit; r++ {
		score := 0
		for _, cell := range grid[r] {
			norm := normalizeHeaderToken(cell)
			if norm == "" {
				continue
			}
			for _, kw := range keywords {
				if strings.Contains(norm, normalizeHeaderToken(kw)) {
					score++
					break
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = r
		}
	}
	if bestScore < minMatches {
		return -1
	}
	return bestIdx
}

func normalizeHeaderToken(s string) string {
	s = reParens.ReplaceAllString(strings.TrimSpace(s), "")
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '_' || r == '*' {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// buildColumnMap은 별칭 사전을 바탕으로 헤더 열 인덱스를 매핑합니다.
func buildColumnMap(headerRow []string, aliases map[string][]string) map[string]int {
	result := make(map[string]int, len(aliases))
	usedCol := make(map[int]bool)

	// 1패스: 정확 일치 우선
	for field, candidates := range aliases {
		for cIdx, raw := range headerRow {
			normCell := normalizeHeaderToken(raw)
			if normCell == "" {
				continue
			}
			for _, cand := range candidates {
				if normCell == normalizeHeaderToken(cand) {
					result[field] = cIdx
					usedCol[cIdx] = true
					break
				}
			}
			if _, ok := result[field]; ok {
				break
			}
		}
	}
	// 2패스: 부분 포함 매칭
	for field, candidates := range aliases {
		if _, ok := result[field]; ok {
			continue
		}
		for cIdx, raw := range headerRow {
			if usedCol[cIdx] {
				continue
			}
			normCell := normalizeHeaderToken(raw)
			if normCell == "" {
				continue
			}
			for _, cand := range candidates {
				if strings.Contains(normCell, normalizeHeaderToken(cand)) {
					result[field] = cIdx
					usedCol[cIdx] = true
					break
				}
			}
			if _, ok := result[field]; ok {
				break
			}
		}
	}
	return result
}

func extractGradePrefix(raw string) int {
	m := rePeriodNum.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) >= 2 {
		if g, err := strconv.Atoi(m[1]); err == nil && g >= 1 && g <= 6 {
			return g
		}
	}
	return 0
}

// normalizeClassName은 "1학년 1반", "1-01", "1반"(상위 학년 조합) 등을 "1-1" 형식으로 정규화합니다.
func normalizeClassName(raw string, fallbackGrade int) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if m := reGradeClass.FindStringSubmatch(s); len(m) == 3 {
		return fmt.Sprintf("%s-%s", m[1], m[2])
	}
	if fallbackGrade > 0 {
		if m := reClassOnly.FindStringSubmatch(s); len(m) == 2 {
			return fmt.Sprintf("%d-%s", fallbackGrade, m[1])
		}
	}
	return s
}

func normalizePeriod(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		p := int(math.Round(f))
		if p >= 1 && p <= 9 {
			return p
		}
		return 0
	}
	if m := rePeriodNum.FindStringSubmatch(s); len(m) >= 2 {
		if p, err := strconv.Atoi(m[1]); err == nil && p >= 1 && p <= 9 {
			return p
		}
	}
	return 0
}

func normalizeHours(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		h := int(math.Round(f))
		if h > 0 {
			return h
		}
		return 0
	}
	if m := rePeriodNum.FindStringSubmatch(s); len(m) >= 2 {
		if h, err := strconv.Atoi(m[1]); err == nil && h > 0 {
			return h
		}
	}
	return 0
}

func normalizeKoreanDays(raw string) []int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	f := func(r rune) bool {
		return r == ',' || r == '·' || r == '/' || r == ';' || r == '|' || unicode.IsSpace(r)
	}
	seen := make(map[int]bool)
	for _, tok := range strings.FieldsFunc(s, f) {
		if d := parseDayOfWeek(tok); d > 0 {
			seen[d] = true
		}
	}
	if len(seen) == 0 {
		// 구분자 없이 "월수금"처럼 붙여 쓴 경우 처리
		for _, r := range s {
			if d := parseDayOfWeek(string(r)); d > 0 {
				seen[d] = true
			}
		}
	}
	out := make([]int, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Ints(out)
	return out
}

func normalizeDateString(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f >= 10000 && f <= 100000 {
			if tm, tErr := excelize.ExcelDateToTime(f, false); tErr == nil {
				return tm.Format("2006-01-02")
			}
		}
		if len(s) == 8 {
			if tm, tErr := time.Parse("20060102", s); tErr == nil {
				return tm.Format("2006-01-02")
			}
		}
	}
	if m := reDateParts.FindStringSubmatch(s); len(m) == 4 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if y >= 1950 && y <= 2100 && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
		}
	}
	return s
}

func resolveTeacherSafe(teachers []protocol.Teacher, key, subject string) (string, error) {
	k := strings.TrimSpace(key)
	subj := strings.TrimSpace(subject)
	if k == "" {
		return "", fmt.Errorf("교사명이 비어 있습니다")
	}
	for _, t := range teachers {
		if strings.TrimSpace(t.ID) == k || (t.Username != "" && strings.TrimSpace(t.Username) == k) {
			return t.ID, nil
		}
	}
	var nameMatches []protocol.Teacher
	for _, t := range teachers {
		if strings.TrimSpace(t.Name) == k {
			nameMatches = append(nameMatches, t)
		}
	}
	if len(nameMatches) == 0 {
		return "", fmt.Errorf("교사 '%s'을(를) 명렬표에서 찾을 수 없습니다", k)
	}
	if len(nameMatches) == 1 {
		return nameMatches[0].ID, nil
	}
	if subj != "" {
		var subjMatches []protocol.Teacher
		for _, t := range nameMatches {
			if strings.EqualFold(strings.TrimSpace(t.Subject), subj) {
				subjMatches = append(subjMatches, t)
			}
		}
		if len(subjMatches) == 1 {
			return subjMatches[0].ID, nil
		}
	}
	return "", fmt.Errorf("동명이인 교사 '%s'이(가) %d명 존재합니다. 과목을 정확히 맞추거나 교사 아이디/ID로 지정해 주세요", k, len(nameMatches))
}

func gridCell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
