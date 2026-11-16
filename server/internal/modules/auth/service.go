package auth

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// Service는 로그인 인증, 6종 업무분장(RBAC) 권한 관리 및 학년도 교사 승계를 담당합니다.
type Service struct {
	store *database.Store
}

// NewService는 인증/권한 모듈 서비스를 생성합니다.
func NewService(store *database.Store) *Service {
	return &Service{store: store}
}

// ListTeachers는 전체 교사 및 학부모 명예감독 명렬표를 반환합니다.
func (s *Service) ListTeachers() ([]protocol.Teacher, error) {
	return database.LoadCollection[protocol.Teacher](s.store, "teachers")
}

// GetTeacher는 ID로 교사 1명을 조회합니다.
func (s *Service) GetTeacher(id string) (*protocol.Teacher, error) {
	teachers, err := s.ListTeachers()
	if err != nil {
		return nil, err
	}
	for i := range teachers {
		if teachers[i].ID == id {
			return &teachers[i], nil
		}
	}
	return nil, fmt.Errorf("교사(%s)를 찾을 수 없습니다", id)
}

// PublicTeacherDirectoryEntry는 로그인 전 과목·성명 선택 및 동명이인 구분용 공개 목록 항목입니다.
type PublicTeacherDirectoryEntry struct {
	ID            string          `json:"id"`
	Username      string          `json:"username"`
	Name          string          `json:"name"`
	Subject       string          `json:"subject"`
	HomeroomClass string          `json:"homeroomClass"`
	Position      string          `json:"position"`
	Roles         []protocol.Role `json:"roles"`
}

// ListPublicDirectory는 로그인 화면에서 과목별 교사 선택 및 동명이인 확인을 위한 공개 목록을 반환합니다.
func (s *Service) ListPublicDirectory() ([]PublicTeacherDirectoryEntry, error) {
	teachers, err := s.ListTeachers()
	if err != nil {
		return nil, err
	}
	var out []PublicTeacherDirectoryEntry
	for _, t := range teachers {
		if t.IsParentHonorary {
			continue
		}
		out = append(out, PublicTeacherDirectoryEntry{
			ID:            t.ID,
			Username:      t.Username,
			Name:          t.Name,
			Subject:       t.Subject,
			HomeroomClass: t.HomeroomClass,
			Position:      t.Position,
			Roles:         t.Roles,
		})
	}
	return out, nil
}

// Authenticate는 아이디와 비밀번호로 로그인을 검증합니다.
func (s *Service) Authenticate(username, password string) (*protocol.Teacher, error) {
	return s.AuthenticateLogin(username, "", "", password)
}

// AuthenticateLogin은 아이디 또는 [성명 + 담당과목(동명이인 구분)] + 비밀번호로 로그인을 검증합니다.
func (s *Service) AuthenticateLogin(username, name, subject, password string) (*protocol.Teacher, error) {
	teachers, err := s.ListTeachers()
	if err != nil {
		return nil, err
	}
	uname := strings.TrimSpace(username)
	tname := strings.TrimSpace(name)
	tsubj := strings.TrimSpace(subject)

	if uname != "" {
		for i := range teachers {
			if teachers[i].Username == uname {
				if tsubj != "" && teachers[i].Subject != "" && !strings.EqualFold(teachers[i].Subject, tsubj) {
					continue
				}
				if err := bcrypt.CompareHashAndPassword([]byte(teachers[i].PasswordHash), []byte(password)); err != nil {
					return nil, errors.New("비밀번호가 일치하지 않습니다")
				}
				t := teachers[i]
				t.PasswordHash = ""
				return &t, nil
			}
		}
		if tname == "" {
			tname = uname
		}
	}

	if tname != "" {
		var matches []protocol.Teacher
		for _, tc := range teachers {
			if tc.Name == tname && (tsubj == "" || strings.EqualFold(tc.Subject, tsubj)) {
				matches = append(matches, tc)
			}
		}
		if len(matches) > 1 {
			return nil, errors.New("동명이인 교사가 존재합니다. 담당과목을 지정해 주세요")
		}
		if len(matches) == 1 {
			if err := bcrypt.CompareHashAndPassword([]byte(matches[0].PasswordHash), []byte(password)); err != nil {
				return nil, errors.New("비밀번호가 일치하지 않습니다")
			}
			t := matches[0]
			t.PasswordHash = ""
			return &t, nil
		}
	}

	return nil, errors.New("일치하는 교사 계정을 찾을 수 없습니다 (아이디/성명 및 담당과목 확인)")
}

// SaveTeacher는 교사 기초 인사 정보를 등록 또는 수정합니다. (비밀번호 미지정 시 기존 해시 유지 또는 "1234" 초기화)
func (s *Service) SaveTeacher(t protocol.Teacher, rawPassword string) (protocol.Teacher, error) {
	if strings.TrimSpace(t.ID) == "" {
		t.ID = fmt.Sprintf("t-%s", strings.TrimSpace(t.Username))
	}
	if strings.TrimSpace(t.Username) == "" {
		t.Username = t.ID
	}
	if len(t.Roles) == 0 {
		t.Roles = []protocol.Role{protocol.RoleTeacher}
	}

	if existing, err := s.GetTeacher(t.ID); err == nil {
		t.PasswordHash = existing.PasswordHash
	}
	if strings.TrimSpace(rawPassword) != "" || t.PasswordHash == "" {
		pw := strings.TrimSpace(rawPassword)
		if pw == "" {
			pw = "1234"
		}
		if pw == "1234" {
			t.MustChangePassword = true
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
		if err != nil {
			return t, err
		}
		t.PasswordHash = string(hashed)
	}

	if err := s.store.SaveRecord("teachers", t.ID, t); err != nil {
		return t, err
	}
	t.PasswordHash = ""
	return t, nil
}

// ChangePassword는 교사 본인의 현재 비밀번호를 검증한 뒤 새 비밀번호로 변경하고 최초 변경 플래그를 해제합니다.
func (s *Service) ChangePassword(teacherID, currentPassword, newPassword string) (*protocol.Teacher, error) {
	newPW := strings.TrimSpace(newPassword)
	if len(newPW) < 4 {
		return nil, errors.New("새 비밀번호는 최소 4자 이상이어야 합니다")
	}
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(currentPassword)); err != nil {
		return nil, errors.New("현재 비밀번호가 일치하지 않습니다")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPW), bcrypt.MinCost)
	if err != nil {
		return nil, err
	}
	t.PasswordHash = string(hashed)
	t.MustChangePassword = false
	if err := s.store.SaveRecord("teachers", t.ID, *t); err != nil {
		return nil, err
	}
	t.PasswordHash = ""
	return t, nil
}

// ResetTeacherPassword는 마스터가 특정 교사의 비밀번호를 초기값("1234")으로 되돌리고 다음 로그인 시 변경을 강제합니다.
func (s *Service) ResetTeacherPassword(teacherID string) error {
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.MinCost)
	if err != nil {
		return err
	}
	t.PasswordHash = string(hashed)
	t.MustChangePassword = true
	return s.store.SaveRecord("teachers", t.ID, *t)
}

// DeleteTeacher는 교사 레코드를 삭제합니다. (최고관리자 전용, 시스템 기본 admin 계정은 삭제 불가)
func (s *Service) DeleteTeacher(id string) error {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return errors.New("삭제할 교사 ID가 필요합니다")
	}
	if cleanID == "t01" {
		return errors.New("시스템 기본 최고관리자(admin) 계정은 삭제할 수 없습니다")
	}
	if t, err := s.GetTeacher(cleanID); err == nil && (strings.EqualFold(strings.TrimSpace(t.Username), "admin") || protocol.IsSystemAdminAccount(*t)) {
		return errors.New("시스템 기본 최고관리자(admin) 계정은 삭제할 수 없습니다")
	}
	return s.store.DeleteRecord("teachers", cleanID)
}

// UpdateRoles는 최고관리자(MASTER)가 특정 교사의 다중 업무분장 권한을 부여/수정합니다.
// 일반 교사에게도 RoleMaster(최고관리자) 권한 부여/해제가 가능하며, 기본 admin 계정의 RoleMaster는 항상 유지됩니다.
func (s *Service) UpdateRoles(teacherID string, roles []protocol.Role) (*protocol.Teacher, error) {
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(roles, protocol.RoleTeacher) {
		roles = append(roles, protocol.RoleTeacher)
	}
	if (strings.EqualFold(strings.TrimSpace(t.Username), "admin") || t.ID == "t01") && !slices.Contains(roles, protocol.RoleMaster) {
		roles = append(roles, protocol.RoleMaster)
	}
	t.Roles = roles
	if err := s.store.SaveRecord("teachers", t.ID, *t); err != nil {
		return nil, err
	}
	t.PasswordHash = ""
	return t, nil
}

// TransferRoleHandover는 업무분장 변경 시 기존 교사의 특정 권한을 후임 교사에게 인계(승계)합니다.
func (s *Service) TransferRoleHandover(fromTeacherID, toTeacherID string, role protocol.Role) error {
	if role == protocol.RoleTeacher {
		return errors.New("일반 교사 기본 권한은 인계 대상이 아닙니다")
	}
	fromT, err := s.GetTeacher(fromTeacherID)
	if err != nil {
		return err
	}
	toT, err := s.GetTeacher(toTeacherID)
	if err != nil {
		return err
	}

	isBaseAdminMaster := (strings.EqualFold(strings.TrimSpace(fromT.Username), "admin") || fromT.ID == "t01") && role == protocol.RoleMaster
	if !isBaseAdminMaster {
		filtered := make([]protocol.Role, 0, len(fromT.Roles))
		for _, r := range fromT.Roles {
			if r != role {
				filtered = append(filtered, r)
			}
		}
		fromT.Roles = filtered
	}
	if !slices.Contains(toT.Roles, role) {
		toT.Roles = append(toT.Roles, role)
	}

	if err := s.store.SaveRecord("teachers", fromT.ID, *fromT); err != nil {
		return err
	}
	return s.store.SaveRecord("teachers", toT.ID, *toT)
}

// HasRole은 교사가 지정된 권한 중 하나라도 보유하고 있는지(또는 MASTER인지) 확인합니다.
func HasRole(t *protocol.Teacher, required ...protocol.Role) bool {
	if t == nil {
		return false
	}
	if slices.Contains(t.Roles, protocol.RoleMaster) {
		return true
	}
	for _, req := range required {
		if slices.Contains(t.Roles, req) {
			return true
		}
	}
	return false
}
