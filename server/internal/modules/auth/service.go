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

// Authenticate는 아이디와 비밀번호로 로그인을 검증합니다.
func (s *Service) Authenticate(username, password string) (*protocol.Teacher, error) {
	teachers, err := s.ListTeachers()
	if err != nil {
		return nil, err
	}
	uname := strings.TrimSpace(username)
	for i := range teachers {
		if teachers[i].Username == uname {
			if err := bcrypt.CompareHashAndPassword([]byte(teachers[i].PasswordHash), []byte(password)); err != nil {
				return nil, errors.New("비밀번호가 일치하지 않습니다")
			}
			t := teachers[i]
			t.PasswordHash = ""
			return &t, nil
		}
	}
	return nil, errors.New("존재하지 않는 교사 아이디입니다")
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

// UpdateRoles는 시스템 총괄(MASTER)이 특정 교사의 다중 업무분장 권한을 부여/수정합니다.
func (s *Service) UpdateRoles(teacherID string, roles []protocol.Role) (*protocol.Teacher, error) {
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(roles, protocol.RoleTeacher) {
		roles = append(roles, protocol.RoleTeacher)
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

	filtered := make([]protocol.Role, 0, len(fromT.Roles))
	for _, r := range fromT.Roles {
		if r != role {
			filtered = append(filtered, r)
		}
	}
	fromT.Roles = filtered
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
