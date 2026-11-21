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

func buildDefaultAdmin(pwHash string) protocol.Teacher {
	return protocol.Teacher{
		ID:                       "t01",
		Username:                 "admin",
		PasswordHash:             pwHash,
		MustChangePassword:       true,
		Name:                     "최고관리자",
		Position:                 "최고관리자",
		IsDepartmentHead:         false,
		Subject:                  "시스템",
		TeacherType:              protocol.TeacherTypeOther,
		HasClasses:               protocol.BoolPtr(false),
		SubstituteExempt:         protocol.BoolPtr(true),
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
}

// EnsureCleanInit은 최초 실행 시 DB가 비어 있으면 최고관리자(admin) 계정,
// 기본 일과시간표 및 학교 기본설정만 원자적으로 생성합니다.
func EnsureCleanInit(s *Store) error {
	cnt, err := s.CountCollection("teachers")
	if err != nil {
		return err
	}
	if cnt > 0 {
		return upgradeLegacyAdminIfNeeded(s)
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte("1234"), BcryptCost())
	if err != nil {
		return err
	}
	admin := buildDefaultAdmin(string(hashBytes))
	return s.WithTx(func(tx *TxStore) error {
		if err := tx.SaveRecord("teachers", admin.ID, admin); err != nil {
			return err
		}
		for _, b := range DefaultBellSchedule() {
			if err := tx.SaveRecord("bell_schedule", fmt.Sprintf("period-%d", b.Period), b); err != nil {
				return err
			}
		}
		cfg := DefaultSchoolConfig()
		return tx.SaveRecord("school_configs", cfg.ID, cfg)
	})
}

// ResetSystem은 전체 초기화 전에 AES-256 암호화 백업을 반드시 먼저 생성한 뒤,
// 지정된 모드에 따라 단일 트랜잭션으로 시스템 데이터를 안전하게 초기화합니다.
func (s *Store) ResetSystem(mode string) (string, error) {
	backupPath, err := s.ExportEncryptedBackup("pre_reset")
	if err != nil {
		return "", fmt.Errorf("초기화 전 안전 백업 생성 실패로 초기화를 중단합니다: %w", err)
	}

	academicCollections := []string{
		"timetable_cells",
		"timetable_requests",
		"teaching_loads",
		"teacher_constraints",
		"timetable_scenarios",
		"timetable_history",
		"exam_configs",
		"exam_specs",
		"exam_slots",
		"external_exams",
		"school_events",
	}

	switch mode {
	case ResetModeAcademicTerm:
		// 학기 초기화: 교사 명렬표 및 학교 기본설정·일과시간표는 보존하고 시간표·시수·시험·행사 데이터만 원자적으로 삭제
		if err := s.WithTx(func(tx *TxStore) error {
			return tx.ClearCollections(academicCollections...)
		}); err != nil {
			return backupPath, err
		}
		return backupPath, nil

	case ResetModeDemoSeed:
		// 시연/테스트용 예시 데이터 복원
		all := append([]string{"teachers", "bell_schedule", "school_configs"}, academicCollections...)
		if err := s.WithTx(func(tx *TxStore) error {
			return tx.ClearCollections(all...)
		}); err != nil {
			return backupPath, err
		}
		if err := SeedIfEmpty(s); err != nil {
			return backupPath, err
		}
		return backupPath, nil

	default:
		// FULL_FACTORY_RESET: 모든 데이터를 비우고 초기 admin(비번 1234) + 기본 일과시간/학교설정만 원자적으로 생성
		all := append([]string{"teachers", "bell_schedule", "school_configs"}, academicCollections...)
		hashBytes, err := bcrypt.GenerateFromPassword([]byte("1234"), BcryptCost())
		if err != nil {
			return backupPath, err
		}
		admin := buildDefaultAdmin(string(hashBytes))
		if err := s.WithTx(func(tx *TxStore) error {
			if err := tx.ClearCollections(all...); err != nil {
				return err
			}
			if err := tx.SaveRecord("teachers", admin.ID, admin); err != nil {
				return err
			}
			for _, b := range DefaultBellSchedule() {
				if err := tx.SaveRecord("bell_schedule", fmt.Sprintf("period-%d", b.Period), b); err != nil {
					return err
				}
			}
			cfg := DefaultSchoolConfig()
			return tx.SaveRecord("school_configs", cfg.ID, cfg)
		}); err != nil {
			return backupPath, err
		}
		return backupPath, nil
	}
}
