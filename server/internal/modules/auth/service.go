package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 로그인 인증, 서명된 세션 토큰 관리, 6종 업무분장(RBAC) 권한 관리 및 학년도 교사 승계를 담당합니다.
type Service struct {
	store    *database.Store
	bus      *eventbus.Bus
	sessions *SessionManager
}

// NewService는 인증/권한 모듈 서비스를 생성합니다.
func NewService(store *database.Store, buses ...*eventbus.Bus) *Service {
	var bus *eventbus.Bus
	if len(buses) > 0 {
		bus = buses[0]
	}
	return &Service{
		store:    store,
		bus:      bus,
		sessions: NewSessionManager(store.MasterKey()),
	}
}

// ListTeachers는 전체 교사 및 학부모 명예감독 명렬표를 반환합니다.
func (s *Service) ListTeachers() ([]protocol.Teacher, error) {
	return database.LoadCollection[protocol.Teacher](s.store, "teachers")
}

// GetTeacher는 ID로 교사 1명을 기본 키 단건 조회합니다.
func (s *Service) GetTeacher(id string) (*protocol.Teacher, error) {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return nil, errors.New("교사 ID가 필요합니다")
	}
	t, err := database.LoadRecord[protocol.Teacher](s.store, "teachers", cleanID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("교사(%s)를 찾을 수 없습니다", cleanID)
		}
		return nil, err
	}
	return t, nil
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

// IsDemoMode는 초기 시연용 체험 계정이 활성화되어 있는지 반환합니다.
func (s *Service) IsDemoMode() bool {
	teachers, err := s.ListTeachers()
	if err != nil {
		return false
	}
	for _, t := range teachers {
		if t.Username == "timetable" || t.Username == "internal" || t.Username == "external" {
			return true
		}
	}
	return false
}

// Authenticate는 아이디와 비밀번호로 로그인을 검증합니다.
func (s *Service) Authenticate(username, password string) (*protocol.Teacher, error) {
	return s.AuthenticateLogin(username, "", "", password)
}

// LoginWithToken은 로그인을 검증한 뒤 교사 정보와 서명된 세션 토큰을 함께 반환합니다.
func (s *Service) LoginWithToken(username, name, subject, password string) (*protocol.Teacher, string, error) {
	t, err := s.AuthenticateLogin(username, name, subject, password)
	if err != nil {
		return nil, "", err
	}
	tok, err := s.sessions.IssueToken(t.ID, 7*24*time.Hour)
	if err != nil {
		return nil, "", err
	}
	return t, tok, nil
}

// VerifySessionToken은 Bearer 세션 토큰을 검증하고 최신 권한이 반영된 교사 정보를 반환합니다.
func (s *Service) VerifySessionToken(token string) (*protocol.Teacher, error) {
	tid, err := s.sessions.VerifyToken(token)
	if err != nil {
		return nil, err
	}
	t, err := s.GetTeacher(tid)
	if err != nil {
		return nil, errors.New("해당 교사 계정이 존재하지 않습니다")
	}
	t.PasswordHash = ""
	return t, nil
}

// LogoutToken은 단일 로그인 세션을 즉시 폐기합니다.
func (s *Service) LogoutToken(token string) {
	s.sessions.RevokeToken(token)
}

// RevokeAllSessions는 시스템 초기화·복원 후 모든 기존 세션을 즉시 무효화합니다.
func (s *Service) RevokeAllSessions() {
	s.sessions.RevokeAll()
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

// SaveTeacher는 내부·마스터 권한으로 교사 기초 인사 정보를 등록 또는 수정합니다.
func (s *Service) SaveTeacher(t protocol.Teacher, rawPassword string) (protocol.Teacher, error) {
	return s.SaveTeacherByActor(nil, t, rawPassword)
}

// SaveTeacherByActor는 요청자 권한에 따라 권한 상승을 차단하고 기존 교사의 누적 통계·면제 이력을 보존하며 저장합니다.
func (s *Service) SaveTeacherByActor(actor *protocol.Teacher, t protocol.Teacher, rawPassword string) (protocol.Teacher, error) {
	t.ID = strings.TrimSpace(t.ID)
	t.Username = strings.TrimSpace(t.Username)
	t.Name = strings.TrimSpace(t.Name)
	t.Subject = strings.TrimSpace(t.Subject)
	if t.ID == "" {
		t.ID = fmt.Sprintf("t-%s", t.Username)
	}
	if t.Username == "" {
		t.Username = t.ID
	}

	allTeachers, err := s.ListTeachers()
	if err != nil {
		return t, err
	}
	var existing *protocol.Teacher
	for i := range allTeachers {
		if allTeachers[i].ID == t.ID {
			cp := allTeachers[i]
			existing = &cp
		} else if strings.EqualFold(strings.TrimSpace(allTeachers[i].Username), t.Username) {
			return t, fmt.Errorf("아이디(%s)는 이미 다른 교사(%s)가 사용 중입니다", t.Username, allTeachers[i].Name)
		}
	}

	// 비-MASTER(시간표 담당자 등)의 권한 상승(MASTER/타 관리자 부여) 원천 차단
	isMasterActor := actor == nil || HasRole(actor, protocol.RoleMaster)
	if !isMasterActor {
		for _, r := range t.Roles {
			if r != "" && r != protocol.RoleTeacher && (existing == nil || !slices.Contains(existing.Roles, r)) {
				return t, errors.New("최고관리자(MASTER)만 교사 권한을 변경하거나 부여할 수 있습니다")
			}
		}
	}

	if existing != nil {
		// 기존 비밀번호, 연간 시험감독·결보강 통계, 교내감독 제외 설정 보존
		t.PasswordHash = existing.PasswordHash
		t.MustChangePassword = existing.MustChangePassword
		t.AnnualMainCount = existing.AnnualMainCount
		t.AnnualSubCount = existing.AnnualSubCount
		t.AnnualCorridorCount = existing.AnnualCorridorCount
		t.InternalExamCount = existing.InternalExamCount
		t.ExternalExamCount = existing.ExternalExamCount
		t.LastExternalDispatch = existing.LastExternalDispatch
		t.SubstitutionCount = existing.SubstitutionCount
		t.InternalExamExempt = existing.InternalExamExempt
		t.InternalExamExemptReason = existing.InternalExamExemptReason
		if len(t.ExemptReasons) == 0 && len(existing.ExemptReasons) > 0 {
			t.ExemptReasons = existing.ExemptReasons
		}
		if !isMasterActor || len(t.Roles) == 0 {
			t.Roles = existing.Roles
		}
		// 시스템 기본 admin 계정 보호
		if strings.EqualFold(strings.TrimSpace(existing.Username), "admin") || existing.ID == "t01" {
			t.Username = existing.Username
			t.Roles = existing.Roles
			t.InternalExamExempt = true
			t.InternalExamExemptReason = existing.InternalExamExemptReason
		}
	} else {
		if !isMasterActor || len(t.Roles) == 0 {
			t.Roles = []protocol.Role{protocol.RoleTeacher}
		}
		if strings.EqualFold(t.Username, "admin") {
			return t, errors.New("시스템 기본 아이디 'admin'은 새로 등록할 수 없습니다")
		}
	}

	if !slices.Contains(t.Roles, protocol.RoleTeacher) {
		t.Roles = append(t.Roles, protocol.RoleTeacher)
	}

	if strings.TrimSpace(rawPassword) != "" || t.PasswordHash == "" {
		pw := strings.TrimSpace(rawPassword)
		if pw == "" {
			pw = "1234"
		}
		if pw == "1234" {
			t.MustChangePassword = true
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(pw), database.BcryptCost())
		if err != nil {
			return t, err
		}
		t.PasswordHash = string(hashed)
	}

	if err := s.store.SaveRecord("teachers", t.ID, t); err != nil {
		return t, err
	}
	t.PasswordHash = ""
	s.publishTeacherEvent(actor, fmt.Sprintf("교사 정보 저장 (%s)", t.Name), t)
	return t, nil
}

// ChangePassword는 교사 본인의 현재 비밀번호를 검증한 뒤 새 비밀번호로 변경하고 기존 세션을 폐기합니다.
func (s *Service) ChangePassword(teacherID, currentPassword, newPassword string) (*protocol.Teacher, error) {
	t, _, err := s.ChangePasswordWithToken(teacherID, currentPassword, newPassword)
	return t, err
}

// ChangePasswordWithToken은 비밀번호 변경 후 기존 토큰을 폐기하고 새 세션 토큰을 재발급합니다.
func (s *Service) ChangePasswordWithToken(teacherID, currentPassword, newPassword string) (*protocol.Teacher, string, error) {
	newPW := strings.TrimSpace(newPassword)
	if len(newPW) < 4 {
		return nil, "", errors.New("새 비밀번호는 최소 4자 이상이어야 합니다")
	}
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return nil, "", err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(currentPassword)); err != nil {
		return nil, "", errors.New("현재 비밀번호가 일치하지 않습니다")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPW), database.BcryptCost())
	if err != nil {
		return nil, "", err
	}
	t.PasswordHash = string(hashed)
	t.MustChangePassword = false
	if err := s.store.SaveRecord("teachers", t.ID, *t); err != nil {
		return nil, "", err
	}
	s.sessions.RevokeTeacher(t.ID)
	newTok, err := s.sessions.IssueToken(t.ID, 7*24*time.Hour)
	if err != nil {
		return nil, "", err
	}
	t.PasswordHash = ""
	s.publishTeacherEvent(t, fmt.Sprintf("%s 비밀번호 변경", t.Name), *t)
	return t, newTok, nil
}

// ResetTeacherPassword는 마스터가 특정 교사의 비밀번호를 초기값("1234")으로 되돌리고 기존 세션을 모두 폐기합니다.
func (s *Service) ResetTeacherPassword(teacherID string) error {
	t, err := s.GetTeacher(teacherID)
	if err != nil {
		return err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte("1234"), database.BcryptCost())
	if err != nil {
		return err
	}
	t.PasswordHash = string(hashed)
	t.MustChangePassword = true
	if err := s.store.SaveRecord("teachers", t.ID, *t); err != nil {
		return err
	}
	s.sessions.RevokeTeacher(t.ID)
	s.publishTeacherEvent(nil, fmt.Sprintf("%s 비밀번호 초기화", t.Name), t.ID)
	return nil
}

// DeleteTeacher는 교사 레코드를 삭제하고 기존 세션을 폐기합니다. (최고관리자 전용, 기본 admin 삭제 불가)
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
	if err := s.store.DeleteRecord("teachers", cleanID); err != nil {
		return err
	}
	s.sessions.RevokeTeacher(cleanID)
	s.publishTeacherEvent(nil, "교사 계정 삭제", cleanID)
	return nil
}

// UpdateRoles는 최고관리자(MASTER)가 특정 교사의 다중 업무분장 권한을 부여/수정합니다.
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
	s.publishTeacherEvent(nil, fmt.Sprintf("%s 업무분장 권한 변경", t.Name), *t)
	return t, nil
}

// TransferRoleHandover는 업무분장 변경 시 기존 교사의 특정 권한을 단일 트랜잭션으로 후임 교사에게 인계합니다.
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

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if err := tx.SaveRecord("teachers", fromT.ID, *fromT); err != nil {
			return err
		}
		return tx.SaveRecord("teachers", toT.ID, *toT)
	}); err != nil {
		return err
	}
	s.publishTeacherEvent(nil, fmt.Sprintf("업무분장 인계 (%s → %s)", fromT.Name, toT.Name), map[string]string{
		"from": fromT.ID, "to": toT.ID, "role": string(role),
	})
	return nil
}

func (s *Service) publishTeacherEvent(actor *protocol.Teacher, title string, payload interface{}) {
	if s.bus == nil {
		return
	}
	actorID, actorName := "", "시스템"
	if actor != nil {
		actorID, actorName = actor.ID, actor.Name
	}
	s.bus.Publish(protocol.DomainEvent{
		Type:      protocol.EventTeacherUpdated,
		Title:     title,
		Message:   "교사 명렬표 및 권한 정보가 갱신되었습니다.",
		ActorID:   actorID,
		ActorName: actorName,
		Payload:   payload,
	})
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
