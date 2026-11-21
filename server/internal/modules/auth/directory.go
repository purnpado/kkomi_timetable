package auth

import (
	"kkomi-timetable/pkg/protocol"
)

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
