package database

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"kkomi-timetable/pkg/protocol"
)

const (
	ResetModeFullFactory  = "FULL_FACTORY_RESET"
	ResetModeAcademicTerm = "ACADEMIC_TERM_RESET"
	ResetModeDemoSeed     = "DEMO_SEED_RESET"
)

// ClearCollections는 지정된 컬렉션들의 모든 레코드를 삭제합니다.
func (s *Store) ClearCollections(collections ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, col := range collections {
		if _, err := s.db.Exec(`DELETE FROM kv_records WHERE collection = ?`, col); err != nil {
			return err
		}
	}
	return nil
}

// ResetSystem은 전체 초기화 전에 AES-256 암호화 백업을 먼저 생성한 뒤,
// 지정된 모드에 따라 시스템 데이터를 안전하게 초기화합니다.
func (s *Store) ResetSystem(mode string) (string, error) {
	backupPath, _ := s.ExportEncryptedBackup("pre_reset")

	academicCollections := []string{
		"timetable_cells",
		"timetable_requests",
		"teaching_loads",
		"exam_configs",
		"exam_slots",
		"external_exams",
		"school_events",
	}

	switch mode {
	case ResetModeAcademicTerm:
		// 학기 초기화: 교사 명렬표 및 학교 기본설정·일과시간표는 보존하고 시간표·시수·시험·행사 데이터만 삭제
		if err := s.ClearCollections(academicCollections...); err != nil {
			return backupPath, err
		}
		return backupPath, nil

	case ResetModeDemoSeed:
		// 시연용 샘플 데이터 복원
		all := append([]string{"teachers", "bell_schedule", "school_configs"}, academicCollections...)
		if err := s.ClearCollections(all...); err != nil {
			return backupPath, err
		}
		if err := SeedIfEmpty(s); err != nil {
			return backupPath, err
		}
		return backupPath, nil

	default:
		// FULL_FACTORY_RESET: 모든 데이터를 비우고 초기 admin(비번 1234) + 기본 일과시간/학교설정만 생성
		all := append([]string{"teachers", "bell_schedule", "school_configs"}, academicCollections...)
		if err := s.ClearCollections(all...); err != nil {
			return backupPath, err
		}
		hashBytes, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.MinCost)
		if err != nil {
			return backupPath, err
		}
		admin := protocol.Teacher{
			ID:                       "t01",
			Username:                 "admin",
			PasswordHash:             string(hashBytes),
			MustChangePassword:       true,
			Name:                     "최고관리자",
			Position:                 "최고관리자",
			IsDepartmentHead:         false,
			Subject:                  "시스템",
			InternalExamExempt:       true,
			InternalExamExemptReason: "시스템 최고관리자 계정",
			ExemptReasons:            []string{"시스템 최고관리자 계정"},
			Roles: []protocol.Role{
				protocol.RoleMaster,
				protocol.RoleTimetableAdmin,
				protocol.RoleInternalExamAdmin,
				protocol.RoleExternalExamAdmin,
				protocol.RoleEventAdmin,
				protocol.RoleTeacher,
			},
		}
		if err := s.SaveRecord("teachers", admin.ID, admin); err != nil {
			return backupPath, err
		}
		for _, b := range DefaultBellSchedule() {
			if err := s.SaveRecord("bell_schedule", fmt.Sprintf("period-%d", b.Period), b); err != nil {
				return backupPath, err
			}
		}
		cfg := DefaultSchoolConfig()
		if err := s.SaveRecord("school_configs", cfg.ID, cfg); err != nil {
			return backupPath, err
		}
		return backupPath, nil
	}
}
