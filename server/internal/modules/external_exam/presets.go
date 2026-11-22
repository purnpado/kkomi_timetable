package external_exam

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// GetSchoolConfig는 학교 외부 시험감독 기본 내규 프리셋(종목 정의·가중치·유예·동점기준)을 반환합니다.
func (s *Service) GetSchoolConfig() (protocol.ExternalExamSchoolConfig, error) {
	loaded, err := database.LoadRecord[protocol.ExternalExamSchoolConfig](s.store, "external_exam_config", "default")
	if err == nil && loaded != nil && len(loaded.Categories) > 0 {
		return *loaded, nil
	}
	return protocol.DefaultExternalExamSchoolConfig(), nil
}

// SaveSchoolConfig는 학교 외부 시험감독 기본 내규 프리셋을 저장합니다.
func (s *Service) SaveSchoolConfig(cfg protocol.ExternalExamSchoolConfig) (protocol.ExternalExamSchoolConfig, error) {
	cfg.ID = "default"
	def := protocol.DefaultExternalExamSchoolConfig()
	if len(cfg.Categories) == 0 {
		cfg.Categories = def.Categories
	}
	for i := range cfg.Categories {
		cfg.Categories[i].Name = strings.TrimSpace(cfg.Categories[i].Name)
		if cfg.Categories[i].Weight <= 0 {
			cfg.Categories[i].Weight = 1.0
		}
	}
	if cfg.DefaultPresetMode == "" {
		cfg.DefaultPresetMode = def.DefaultPresetMode
	}
	if cfg.DefaultScoringMode == "" {
		cfg.DefaultScoringMode = def.DefaultScoringMode
	}
	if len(cfg.DefaultPriorityOrder) == 0 {
		cfg.DefaultPriorityOrder = def.DefaultPriorityOrder
	}
	if cfg.DefaultTieBreaker == "" {
		cfg.DefaultTieBreaker = def.DefaultTieBreaker
	}
	if cfg.DefaultVolunteerOverflow == "" {
		cfg.DefaultVolunteerOverflow = def.DefaultVolunteerOverflow
	}
	if err := s.store.SaveRecord("external_exam_config", cfg.ID, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func categoryWeight(cfg protocol.ExternalExamSchoolConfig, category string) float64 {
	trimmed := strings.TrimSpace(category)
	for _, cat := range cfg.Categories {
		if strings.EqualFold(strings.TrimSpace(cat.Name), trimmed) && cat.Weight > 0 {
			return cat.Weight
		}
	}
	switch {
	case strings.Contains(trimmed, "수능"):
		return 2.0
	case strings.Contains(trimmed, "임용"):
		return 1.5
	default:
		return 1.0
	}
}

func defaultExcludeRulesForCategory(cfg protocol.ExternalExamSchoolConfig, category string) []string {
	trimmed := strings.TrimSpace(category)
	for _, cat := range cfg.Categories {
		if strings.EqualFold(strings.TrimSpace(cat.Name), trimmed) {
			return append([]string(nil), cat.DefaultExcludeRules...)
		}
	}
	return nil
}

func matchesCategoryExcludeRule(t protocol.Teacher, rule string, examYear int) (bool, string) {
	reasonJoined := strings.Join(append([]string{t.Position, t.InternalExamExemptReason}, t.ExemptReasons...), " ")
	switch strings.ToUpper(strings.TrimSpace(rule)) {
	case "GRADE3_HOMEROOM":
		if strings.HasPrefix(strings.TrimSpace(t.HomeroomClass), "3-") {
			return true, "고3 담임 제외"
		}
	case "EXAM_CHILD":
		for _, kw := range []string{"수험생", "고3자녀", "수능자녀"} {
			if strings.Contains(reasonJoined, kw) {
				return true, "수험생 자녀 제외"
			}
		}
	case "PREGNANT":
		for _, kw := range []string{"임산부", "임신"} {
			if strings.Contains(reasonJoined, kw) {
				return true, "임산부 보호 제외"
			}
		}
	case "SENIOR":
		if strings.Contains(reasonJoined, "원로") || (t.BirthYear > 0 && examYear-t.BirthYear >= 58) {
			return true, "원로교사 제외"
		}
	case "CONTRACT_TEACHER":
		if strings.Contains(reasonJoined, "기간제") {
			return true, "기간제교사 제외"
		}
	case "DEPT_HEAD":
		if t.IsDepartmentHead || strings.Contains(t.Position, "부장") {
			return true, "보직부장 제외"
		}
	}
	return false, ""
}

func isTransferGracePeriod(transferDate, examDate string) bool {
	normT := normalizeDateCmp(transferDate)
	if len(normT) != 10 {
		return false
	}
	tDate, err := time.Parse("2006-01-02", normT)
	if err != nil {
		return false
	}
	ref := time.Now()
	if strings.TrimSpace(examDate) != "" {
		if parsed, pErr := time.Parse("2006-01-02", strings.TrimSpace(examDate)); pErr == nil {
			ref = parsed
		}
	}
	days := ref.Sub(tDate).Hours() / 24
	return days >= 0 && days < 365
}

func compareIntDescPreferPositive(a, b int) int {
	if a == b {
		return 0
	}
	if a > 0 && b > 0 {
		if a > b {
			return -1
		}
		return 1
	}
	if a > 0 {
		return -1
	}
	return 1
}

func compareIntAscPreferPositive(a, b int) int {
	if a == b {
		return 0
	}
	if a > 0 && b > 0 {
		if a < b {
			return -1
		}
		return 1
	}
	if a > 0 {
		return -1
	}
	return 1
}

func lotteryDigest(seed, noticeID, examDate, teacherID string) string {
	s := strings.TrimSpace(seed)
	if s == "" {
		s = noticeID + "|" + examDate
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s", s, teacherID)))
	return hex.EncodeToString(sum[:])
}

func compareTieBreaker(a, b protocol.ExternalExamCandidate, notice *protocol.ExternalExamNotice) int {
	switch strings.ToUpper(strings.TrimSpace(notice.TieBreaker)) {
	case "AGE_YOUNG":
		if cmp := compareIntDescPreferPositive(a.Teacher.BirthYear, b.Teacher.BirthYear); cmp != 0 {
			return cmp
		}
	case "AGE_OLD":
		if cmp := compareIntAscPreferPositive(a.Teacher.BirthYear, b.Teacher.BirthYear); cmp != 0 {
			return cmp
		}
	case "SALARY_LOW":
		if cmp := compareIntAscPreferPositive(a.Teacher.SalaryStep, b.Teacher.SalaryStep); cmp != 0 {
			return cmp
		}
	case "SALARY_HIGH":
		if cmp := compareIntDescPreferPositive(a.Teacher.SalaryStep, b.Teacher.SalaryStep); cmp != 0 {
			return cmp
		}
	case "LOTTERY":
		ha := lotteryDigest(notice.LotterySeed, notice.ID, notice.ExamDate, a.Teacher.ID)
		hb := lotteryDigest(notice.LotterySeed, notice.ID, notice.ExamDate, b.Teacher.ID)
		if ha != hb {
			if ha < hb {
				return -1
			}
			return 1
		}
	}
	if a.Teacher.Name != b.Teacher.Name {
		if a.Teacher.Name < b.Teacher.Name {
			return -1
		}
		return 1
	}
	return 0
}

func compareVolunteersOverflow(
	a, b protocol.ExternalExamCandidate,
	notice *protocol.ExternalExamNotice,
	volunteerOrder map[string]int,
) int {
	switch strings.ToUpper(strings.TrimSpace(notice.VolunteerOverflowMode)) {
	case "FIRST_COME":
		oa, ob := volunteerOrder[a.Teacher.ID], volunteerOrder[b.Teacher.ID]
		if oa != ob {
			if oa < ob {
				return -1
			}
			return 1
		}
	case "LOW_COUNT_FIRST":
		if cmp := compareByCriteria(a, b, protocol.CriteriaLowCount, notice.ScoringMode); cmp != 0 {
			return cmp
		}
	case "LOTTERY":
		ha := lotteryDigest(notice.LotterySeed, notice.ID, notice.ExamDate, a.Teacher.ID)
		hb := lotteryDigest(notice.LotterySeed, notice.ID, notice.ExamDate, b.Teacher.ID)
		if ha != hb {
			if ha < hb {
				return -1
			}
			return 1
		}
	}
	return 0
}
