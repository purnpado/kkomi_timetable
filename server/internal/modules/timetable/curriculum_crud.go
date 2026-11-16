package timetable

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ClassCurriculumStatus는 학급별 기준 시수와 시수표 배당 합계 일치 여부를 나타냅니다.
type ClassCurriculumStatus struct {
	Grade         int    `json:"grade"`
	ClassName     string `json:"className"`
	RequiredHours int    `json:"requiredHours"`
	AssignedHours int    `json:"assignedHours"`
	Status        string `json:"status"` // "MATCH", "SHORT", "OVER"
}

// CurriculumSummary는 시수표 검증 요약 결과입니다.
type CurriculumSummary struct {
	ClassStatuses []ClassCurriculumStatus `json:"classStatuses"`
	TeacherTotals map[string]int          `json:"teacherTotals"`
	TotalRequired int                     `json:"totalRequired"`
	TotalAssigned int                     `json:"totalAssigned"`
}

// GetSchoolConfig는 학교 기본사항(학급수·요일별 시수·교과교실) 설정을 반환합니다.
func (s *Service) GetSchoolConfig() (protocol.SchoolConfig, error) {
	list, err := database.LoadCollection[protocol.SchoolConfig](s.store, "school_configs")
	if err == nil {
		for _, cfg := range list {
			if cfg.ID == "default" {
				return normalizeSchoolConfig(cfg), nil
			}
		}
	}
	return database.DefaultSchoolConfig(), nil
}

// SaveSchoolConfig는 학교 학급수, 요일별 시수 및 교과교실/특별실 목록을 저장합니다.
func (s *Service) SaveSchoolConfig(cfg protocol.SchoolConfig) (protocol.SchoolConfig, error) {
	cfg = normalizeSchoolConfig(cfg)
	if err := s.store.SaveRecord("school_configs", cfg.ID, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func normalizeSchoolConfig(cfg protocol.SchoolConfig) protocol.SchoolConfig {
	def := database.DefaultSchoolConfig()
	if cfg.ID == "" {
		cfg.ID = "default"
	}
	if strings.TrimSpace(cfg.SchoolName) == "" {
		cfg.SchoolName = def.SchoolName
	}
	if cfg.AcademicYear == 0 {
		cfg.AcademicYear = def.AcademicYear
	}
	if len(cfg.GradeClassCounts) == 0 {
		cfg.GradeClassCounts = def.GradeClassCounts
	}
	if cfg.VirtualClassCounts == nil {
		cfg.VirtualClassCounts = map[int]int{}
	}
	if len(cfg.DailyPeriodLimits) == 0 {
		cfg.DailyPeriodLimits = def.DailyPeriodLimits
	}
	return cfg
}

// ListTeachingLoads는 등록된 교사별·과목별·학급별 시수표 목록을 반환합니다.
func (s *Service) ListTeachingLoads() ([]protocol.TeachingLoad, error) {
	loads, err := database.LoadCollection[protocol.TeachingLoad](s.store, "teaching_loads")
	if err != nil {
		return nil, err
	}
	sort.Slice(loads, func(i, j int) bool {
		if loads[i].Grade != loads[j].Grade {
			return loads[i].Grade < loads[j].Grade
		}
		if loads[i].TeacherName != loads[j].TeacherName {
			return loads[i].TeacherName < loads[j].TeacherName
		}
		return loads[i].Subject < loads[j].Subject
	})
	return loads, nil
}

// SaveTeachingLoad는 교사별 시수표 항목 1행을 생성 또는 수정합니다.
func (s *Service) SaveTeachingLoad(ld protocol.TeachingLoad) (protocol.TeachingLoad, error) {
	ld.TeacherID = strings.TrimSpace(ld.TeacherID)
	ld.Subject = strings.TrimSpace(ld.Subject)
	if ld.TeacherID == "" || ld.Subject == "" {
		return ld, errors.New("담당교사와 과목명은 필수입니다")
	}
	if ld.Grade < 1 || ld.Grade > 3 {
		ld.Grade = 1
	}
	if ld.BlockHours <= 0 {
		ld.BlockHours = 1
	}
	if ld.ShortSubject == "" {
		runes := []rune(ld.Subject)
		if len(runes) > 2 {
			ld.ShortSubject = string(runes[:2])
		} else {
			ld.ShortSubject = ld.Subject
		}
	}
	if ld.SubjectCode == "" {
		ld.SubjectCode = subjectToCode(ld.Subject, ld.Grade)
	}
	sum := 0
	cleanedHours := make(map[string]int, len(ld.ClassHours))
	for cls, hrs := range ld.ClassHours {
		if hrs > 0 {
			cleanedHours[strings.TrimSpace(cls)] = hrs
			sum += hrs
		}
	}
	ld.ClassHours = cleanedHours
	ld.TotalHours = sum

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	for _, t := range teachers {
		if t.ID == ld.TeacherID {
			ld.TeacherName = t.Name
			if ld.DedicatedRoom == "" && t.DefaultRoom != "" {
				ld.DedicatedRoom = t.DefaultRoom
			}
			break
		}
	}
	if ld.ID == "" {
		ld.ID = fmt.Sprintf("ld-%d-%s-%s", ld.Grade, ld.TeacherID, strings.ReplaceAll(ld.Subject, " ", ""))
	}
	if err := s.store.SaveRecord("teaching_loads", ld.ID, ld); err != nil {
		return ld, err
	}
	return ld, nil
}

// DeleteTeachingLoad는 교사별 시수표 항목을 삭제합니다.
func (s *Service) DeleteTeachingLoad(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("삭제할 시수표 ID가 필요합니다")
	}
	return s.store.DeleteRecord("teaching_loads", id)
}

// ValidateCurriculum은 학급별 요일기준 시수와 교사별 시수표 배당 합계의 일치성을 검증합니다.
func (s *Service) ValidateCurriculum() (CurriculumSummary, error) {
	cfg, _ := s.GetSchoolConfig()
	loads, err := s.ListTeachingLoads()
	if err != nil {
		return CurriculumSummary{}, err
	}

	classAssigned := make(map[string]int)
	teacherTotals := make(map[string]int)
	for _, ld := range loads {
		rowSum := 0
		for cls, hrs := range ld.ClassHours {
			if hrs > 0 {
				classAssigned[cls] += hrs
				rowSum += hrs
			}
		}
		teacherTotals[ld.TeacherID] += rowSum
	}

	var statuses []ClassCurriculumStatus
	totalReq, totalAsg := 0, 0
	for g := 1; g <= 3; g++ {
		cnt := cfg.GradeClassCounts[g]
		req := 0
		for d := 1; d <= 5; d++ {
			req += cfg.DailyPeriodLimits[g][d]
		}
		for c := 1; c <= cnt; c++ {
			cls := fmt.Sprintf("%d-%d", g, c)
			asg := classAssigned[cls]
			st := "MATCH"
			if asg < req {
				st = "SHORT"
			} else if asg > req {
				st = "OVER"
			}
			statuses = append(statuses, ClassCurriculumStatus{
				Grade: g, ClassName: cls, RequiredHours: req, AssignedHours: asg, Status: st,
			})
			totalReq += req
			totalAsg += asg
		}
	}

	return CurriculumSummary{
		ClassStatuses: statuses,
		TeacherTotals: teacherTotals,
		TotalRequired: totalReq,
		TotalAssigned: totalAsg,
	}, nil
}
