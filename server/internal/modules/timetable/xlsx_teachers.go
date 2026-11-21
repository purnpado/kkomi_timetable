package timetable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ExportTeachersXLSX는 교사 명렬표 전체(시스템 admin·학부모 명예감독 제외)를 엑셀(.xlsx) 바이트로 내보냅니다.
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
		"교원유형", "수업유무(Y/N)", "결보강대타제외(Y/N)",
	}
	for c, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}

	rowIdx := 2
	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			continue
		}
		protocol.PopulateTeacherDefaults(&t)
		var days []string
		for _, d := range t.ItinerantDays {
			days = append(days, strconv.Itoa(d))
		}
		vals := []interface{}{
			t.ID, t.Username, t.Name, t.Subject, t.ShortSubject, t.Position,
			t.HomeroomClass, boolToYN(t.IsDepartmentHead), t.DefaultRoom, t.Phone, t.TransferDate,
			strings.Join(days, ","), strings.Join(t.ExemptReasons, ","),
			protocol.TeacherTypeLabel(t.TeacherType),
			boolToYN(protocol.HasRegularClasses(t)),
			boolToYN(protocol.IsSubstituteExcluded(t)),
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
		"id":          {"ID"},
		"username":    {"아이디", "계정"},
		"name":        {"성명", "이름", "교사명"},
		"subject":     {"담당과목", "과목", "교과"},
		"short":       {"단축과목명", "단축과목"},
		"pos":         {"직위", "직책"},
		"homeroom":    {"담임학급", "담임"},
		"head":        {"부장여부", "부장"},
		"room":        {"전용교과교실", "교과교실", "전용실"},
		"phone":       {"연락처", "전화번호"},
		"transfer":    {"전입일", "현임교전입일"},
		"itinerant":   {"출강요일", "순회요일"},
		"exempt":      {"면제사유"},
		"teacherType": {"교원유형", "교사유형", "직군", "비교과구분"},
		"hasClasses":  {"수업유무", "수업여부", "정규수업"},
		"subExempt":   {"결보강대타제외", "대타제외", "보강제외"},
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
	_, hasTypeCol := colMap["teacherType"]
	_, hasClsCol := colMap["hasClasses"]
	_, hasSubExCol := colMap["subExempt"]

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
			IsDepartmentHead:   isTruthyCell(gridCell(row, colOr("head", 7))),
			DefaultRoom:        gridCell(row, colOr("room", 8)),
			Phone:              gridCell(row, colOr("phone", 9)),
			TransferDate:       normalizeDateString(gridCell(row, colOr("transfer", 10))),
			ItinerantDays:      normalizeKoreanDays(gridCell(row, colOr("itinerant", 11))),
			Roles:              []protocol.Role{protocol.RoleTeacher},
		}
		if hasTypeCol {
			t.TeacherType = protocol.NormalizeTeacherType(protocol.TeacherType(gridCell(row, colMap["teacherType"])))
		}
		if hasClsCol {
			if v, ok := parseYNCell(gridCell(row, colMap["hasClasses"])); ok {
				t.HasClasses = protocol.BoolPtr(v)
			}
		}
		if hasSubExCol {
			if v, ok := parseYNCell(gridCell(row, colMap["subExempt"])); ok {
				t.SubstituteExempt = protocol.BoolPtr(v)
			}
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
			if !hasTypeCol || t.TeacherType == "" {
				t.TeacherType = prev.TeacherType
			}
			if !hasClsCol || t.HasClasses == nil {
				t.HasClasses = prev.HasClasses
			}
			if !hasSubExCol || t.SubstituteExempt == nil {
				t.SubstituteExempt = prev.SubstituteExempt
			}
			if protocol.IsSystemAdminAccount(prev) || strings.EqualFold(prev.Username, "admin") {
				t.Name, t.Subject, t.Position = prev.Name, prev.Subject, prev.Position
				t.Roles = prev.Roles
				t.TeacherType = protocol.TeacherTypeOther
				t.HasClasses = protocol.BoolPtr(false)
				t.SubstituteExempt = protocol.BoolPtr(true)
			}
		} else if protocol.IsSpecialEducationTeacher(t) && strings.TrimSpace(t.InternalExamExemptReason) == "" {
			t.InternalExamExempt = true
			t.InternalExamExemptReason = "특수반(개별고사장 전담)"
		}
		protocol.PopulateTeacherDefaults(&t)
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

func boolToYN(v bool) string {
	if v {
		return "Y"
	}
	return "N"
}

func isTruthyCell(raw string) bool {
	v, ok := parseYNCell(raw)
	return ok && v
}

func parseYNCell(raw string) (bool, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	switch s {
	case "Y", "YES", "TRUE", "1", "O", "예", "있음", "제외":
		return true, true
	case "N", "NO", "FALSE", "0", "X", "아니오", "아니요", "없음", "포함", "해당없음":
		return false, true
	default:
		return false, false
	}
}
