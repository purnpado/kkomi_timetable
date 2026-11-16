package internal_exam

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 교내 지필고사 감독 배정, 사전 봉인(Sealed)/당일 아침 일괄 공개(Unseal),
// 그리고 당일 돌발 결원 긴급 대타 추천/교체를 담당합니다.
type Service struct {
	store *database.Store
	bus   *eventbus.Bus
	stop  chan struct{}
}

// NewService는 교내 시험감독 서비스를 생성합니다.
func NewService(store *database.Store, bus *eventbus.Bus) *Service {
	svc := &Service{
		store: store,
		bus:   bus,
		stop:  make(chan struct{}),
	}
	svc.ensureSeedSlots()
	return svc
}

func (s *Service) ensureSeedSlots() {
	existing, err := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
	if err != nil || len(existing) > 0 {
		return
	}
	cfgs, err := database.LoadCollection[protocol.ExamConfig](s.store, "exam_configs")
	if err != nil || len(cfgs) == 0 {
		return
	}
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil || len(teachers) == 0 {
		return
	}
	specs := DefaultPeriodSpecs()
	for i, sp := range specs {
		_ = s.store.SaveRecord("exam_specs", fmt.Sprintf("spec-%d", i+1), sp)
	}
	slots := AssignSlots(cfgs[0], specs, teachers)
	for _, slot := range slots {
		_ = s.store.SaveRecord("exam_slots", slot.ID, slot)
	}
}

// DefaultPeriodSpecs는 과목별로 상이한 시험시간이 반영된 기본 지필고사 편성안을 반환합니다.
func DefaultPeriodSpecs() []ExamPeriodSpec {
	return []ExamPeriodSpec{
		{Period: 1, Grade: 1, Subject: "국어", StartTime: "08:50", EndTime: "09:35", DurationMinutes: 45, ClassRooms: []string{"1-1", "1-2"}},
		{Period: 2, Grade: 2, Subject: "수학", StartTime: "09:50", EndTime: "10:40", DurationMinutes: 50, ClassRooms: []string{"2-1", "2-2"}},
		{Period: 3, Grade: 3, Subject: "영어", StartTime: "10:55", EndTime: "11:35", DurationMinutes: 40, ClassRooms: []string{"3-1", "3-2"}},
	}
}

// ListSpecs는 저장된 교시·과목별 시험시간 편성안을 반환합니다.
func (s *Service) ListSpecs() []ExamPeriodSpec {
	specs, err := database.LoadCollection[ExamPeriodSpec](s.store, "exam_specs")
	if err != nil || len(specs) == 0 {
		return DefaultPeriodSpecs()
	}
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Period != specs[j].Period {
			return specs[i].Period < specs[j].Period
		}
		return specs[i].Grade < specs[j].Grade
	})
	return specs
}

// StartScheduler는 시험 당일 지정 시각(기본 08:20) 도래 시 봉인된 고사장을 자동 일괄 공개하는 백그라운드 워커를 시작합니다.
func (s *Service) StartScheduler() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case now := <-ticker.C:
				cfgs, err := database.LoadCollection[protocol.ExamConfig](s.store, "exam_configs")
				if err != nil {
					continue
				}
				today := now.Format("2006-01-02")
				hhmm := now.Format("15:04")
				for _, cfg := range cfgs {
					if cfg.IsSealed && !cfg.InstantRevealMode && cfg.ExamDate == today && cfg.UnsealTime != "" && hhmm >= cfg.UnsealTime {
						_, _ = s.UnsealExam(cfg.ID, "자동공개 스케줄러")
					}
				}
			}
		}
	}()
}

// StopScheduler는 스케줄러 고루틴을 안전하게 종료합니다.
func (s *Service) StopScheduler() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// GetConfig는 현재 교내 시험 설정을 조회합니다.
func (s *Service) GetConfig() (protocol.ExamConfig, error) {
	cfgs, err := database.LoadCollection[protocol.ExamConfig](s.store, "exam_configs")
	if err != nil || len(cfgs) == 0 {
		return protocol.ExamConfig{}, fmt.Errorf("교내 지필고사 설정이 없습니다")
	}
	return cfgs[0], nil
}

// GenerateAndSave는 배정 옵션(복도감독 우선배치, 정/부감독, 학부모 명예감독 On/Off, 즉시공개/사전봉인)으로 감독 배정표를 생성합니다.
func (s *Service) GenerateAndSave(cfg protocol.ExamConfig, specs []ExamPeriodSpec) ([]protocol.ExamSlot, error) {
	if cfg.ID == "" {
		cfg.ID = "exam-2026-2-final"
	}
	if cfg.UnsealTime == "" {
		cfg.UnsealTime = "08:20"
	}
	if cfg.SubjectTeacherPolicy == "" {
		cfg.SubjectTeacherPolicy = protocol.SubjectModeCorridorFirst
	}
	if cfg.InstantRevealMode {
		cfg.IsSealed = false
	}
	if len(specs) == 0 {
		specs = s.ListSpecs()
	}

	// 기존 시험 설정·시간 편성안 및 슬롯 정리 후 재생성
	_ = s.store.ClearCollections("exam_specs", "exam_configs", "exam_slots")
	for i, sp := range specs {
		_ = s.store.SaveRecord("exam_specs", fmt.Sprintf("spec-%d", i+1), sp)
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	slots := AssignSlots(cfg, specs, teachers)
	if err := s.store.SaveRecord("exam_configs", cfg.ID, cfg); err != nil {
		return nil, err
	}
	for _, slot := range slots {
		if err := s.store.SaveRecord("exam_slots", slot.ID, slot); err != nil {
			return nil, err
		}
	}

	if s.bus != nil && cfg.IsSealed {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventExamSealed,
			Title:   fmt.Sprintf("[%s] 감독 배정 사전 봉인 완료", cfg.Title),
			Message: fmt.Sprintf("감독 교시만 안내되며 고사장은 %s 당일 %s에 일괄 공개됩니다.", cfg.ExamDate, cfg.UnsealTime),
			Payload: cfg,
		})
	}
	return slots, nil
}

// ListSlotsForViewer는 조회자의 권한(교내 시험감독 관리자 vs 일반 교사)에 따라
// 봉인 상태일 경우 서버 패킷 레벨에서 고사장명을 마스킹("🔒 당일 08:20 공개")해 반환합니다.
func (s *Service) ListSlotsForViewer(isExamAdmin bool) ([]protocol.ExamSlot, protocol.ExamConfig, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return nil, protocol.ExamConfig{}, err
	}
	slots, err := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
	if err != nil {
		return nil, cfg, err
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Period != slots[j].Period {
			return slots[i].Period < slots[j].Period
		}
		return slots[i].ID < slots[j].ID
	})

	if cfg.IsSealed && !isExamAdmin {
		maskedMsg := fmt.Sprintf("🔒 당일 %s 공개", cfg.UnsealTime)
		for i := range slots {
			slots[i].RoomName = maskedMsg
			slots[i].ActualRoomHidden = true
		}
	}
	return slots, cfg, nil
}

// UnsealExam은 봉인된 시험의 고사장을 즉시 일괄 공개하고 전 교원에게 긴급 토스트를 발송합니다.
func (s *Service) UnsealExam(examID, actorName string) (protocol.ExamConfig, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return protocol.ExamConfig{}, err
	}
	cfg.IsSealed = false
	if err := s.store.SaveRecord("exam_configs", cfg.ID, cfg); err != nil {
		return cfg, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExamUnsealed,
			Title:     fmt.Sprintf("📢 [%s] 오늘 시험 고사장이 공개되었습니다!", cfg.Title),
			Message:   "감독 배정 고사장을 확인하시고 시작 10분 전 평가본부로 집결해 주시기 바랍니다.",
			Urgent:    true,
			ActorName: actorName,
			Payload:   cfg,
		})
	}
	return cfg, nil
}

// RecommendSubstitutes는 당일 아침 결원 발생 시 해당 교시 미배정(공강) 및 비담임 교사를 우선 추천합니다.
func (s *Service) RecommendSubstitutes(slotID string) ([]protocol.Teacher, error) {
	slots, err := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
	if err != nil {
		return nil, err
	}
	var target *protocol.ExamSlot
	for i := range slots {
		if slots[i].ID == slotID {
			target = &slots[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("감독 슬롯(%s)을 찾을 수 없습니다", slotID)
	}

	busyTeachers := make(map[string]bool)
	for _, sl := range slots {
		if sl.Period == target.Period {
			busyTeachers[sl.TeacherID] = true
		}
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	roomPrefix := extractRoomBaseClass(target.RoomName)
	var recs []protocol.Teacher
	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) || busyTeachers[t.ID] {
			continue
		}
		if exempt, _ := IsEffectiveExamExempt(t); exempt {
			continue
		}
		if t.HomeroomClass != "" && roomPrefix != "" && strings.EqualFold(t.HomeroomClass, roomPrefix) {
			continue
		}
		if target.SlotRole != "CORRIDOR" && roomOrSpecMatchesTeacherSubject(target.RoomName, target.Subject, t.Subject) {
			continue
		}
		recs = append(recs, t)
	}
	sort.Slice(recs, func(i, j int) bool {
		if (recs[i].HomeroomClass == "") != (recs[j].HomeroomClass == "") {
			return recs[i].HomeroomClass == ""
		}
		return recs[i].InternalExamCount < recs[j].InternalExamCount
	})
	return recs, nil
}

// ApplySubstitute는 당일 돌발 결원 슬롯을 대체 교사로 원클릭 교체하고 긴급 알림을 보냅니다.
func (s *Service) ApplySubstitute(slotID, newTeacherID, actorName string) (*protocol.ExamSlot, error) {
	slots, err := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
	if err != nil {
		return nil, err
	}
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	var newTeacher *protocol.Teacher
	for i := range teachers {
		if teachers[i].ID == newTeacherID {
			newTeacher = &teachers[i]
			break
		}
	}
	if newTeacher == nil {
		return nil, fmt.Errorf("대체 교사(%s)를 찾을 수 없습니다", newTeacherID)
	}

	for i := range slots {
		if slots[i].ID == slotID {
			oldName := slots[i].TeacherName
			slots[i].TeacherID = newTeacher.ID
			slots[i].TeacherName = newTeacher.Name
			slots[i].Note = fmt.Sprintf("긴급대타 (기존: %s)", oldName)
			if err := s.store.SaveRecord("exam_slots", slots[i].ID, slots[i]); err != nil {
				return nil, err
			}
			if s.bus != nil {
				s.bus.Publish(protocol.DomainEvent{
					Type:      protocol.EventExamSubstituted,
					Title:     fmt.Sprintf("🚨 [긴급 대타 배정] %d교시 %s", slots[i].Period, slots[i].RoomName),
					Message:   fmt.Sprintf("%s 선생님 결원으로 %s 선생님이 긴급 감독 배정되었습니다.", oldName, newTeacher.Name),
					Urgent:    true,
					TargetIDs: []string{newTeacher.ID},
					ActorName: actorName,
					Payload:   slots[i],
				})
			}
			return &slots[i], nil
		}
	}
	return nil, fmt.Errorf("감독 슬롯(%s)을 찾을 수 없습니다", slotID)
}

// ElectiveRoomItem은 고교학점제 선택과목 분반 또는 교과교실 기반 시험 고사실 항목입니다.
type ElectiveRoomItem struct {
	Label     string `json:"label"`
	Subject   string `json:"subject"`
	Grade     int    `json:"grade"`
	GroupName string `json:"groupName"`
}

// ExamRoomCatalog는 학급 고사실, 교과교실, 고교선택과목 고사실 및 별도고사실 목록입니다.
type ExamRoomCatalog struct {
	GradeClassRooms map[int][]string   `json:"gradeClassRooms"`
	SpecialRooms    []string           `json:"specialRooms"`
	ElectiveRooms   []ElectiveRoomItem `json:"electiveRooms"`
	IsolatedRooms   []string           `json:"isolatedRooms"`
}

// BuildRoomCatalog는 학교 기본설정·시수표·시간표에서 정규반, 교과교실, 고교선택과목 고사실 및 별도 고사실 목록을 수집합니다.
func (s *Service) BuildRoomCatalog() ExamRoomCatalog {
	cat := ExamRoomCatalog{
		GradeClassRooms: map[int][]string{},
		IsolatedRooms:   []string{"별도고사실(독감·감염병)", "특수학급고사실"},
	}
	cfgs, _ := database.LoadCollection[protocol.SchoolConfig](s.store, "school_configs")
	specialSet := make(map[string]bool)
	classSets := map[int]map[string]bool{1: {}, 2: {}, 3: {}}
	for _, sc := range cfgs {
		for g, cnt := range sc.GradeClassCounts {
			if classSets[g] == nil {
				classSets[g] = map[string]bool{}
			}
			for i := 1; i <= cnt; i++ {
				classSets[g][fmt.Sprintf("%d-%d", g, i)] = true
			}
		}
		for _, rm := range sc.SpecialRooms {
			if rm.Name != "" {
				specialSet[rm.Name] = true
			}
		}
	}

	electiveSeen := make(map[string]bool)
	cells, _ := database.LoadCollection[protocol.TimetableCell](s.store, "timetable_cells")
	for _, c := range cells {
		if c.Grade >= 1 && c.Grade <= 3 && c.ClassName != "" {
			classSets[c.Grade][c.ClassName] = true
		}
		if c.Room != "" && c.Room != c.ClassName+"교실" {
			specialSet[c.Room] = true
		}
		if c.ElectiveGroup != "" || (c.Room != "" && c.Room != c.ClassName+"교실") {
			roomHost := c.Room
			if roomHost == "" {
				roomHost = c.ClassName
			}
			label := fmt.Sprintf("%s(%s)", roomHost, c.Subject)
			if !electiveSeen[label] {
				electiveSeen[label] = true
				cat.ElectiveRooms = append(cat.ElectiveRooms, ElectiveRoomItem{
					Label: label, Subject: c.Subject, Grade: c.Grade, GroupName: c.ElectiveGroup,
				})
			}
		}
	}

	loads, _ := database.LoadCollection[protocol.TeachingLoad](s.store, "teaching_loads")
	for _, ld := range loads {
		if ld.DedicatedRoom != "" {
			specialSet[ld.DedicatedRoom] = true
		}
		if ld.SimultaneousGroup != "" || ld.DedicatedRoom != "" {
			host := ld.DedicatedRoom
			if host == "" {
				host = fmt.Sprintf("%d학년분반", ld.Grade)
			}
			label := fmt.Sprintf("%s(%s)", host, ld.Subject)
			if !electiveSeen[label] {
				electiveSeen[label] = true
				cat.ElectiveRooms = append(cat.ElectiveRooms, ElectiveRoomItem{
					Label: label, Subject: ld.Subject, Grade: ld.Grade, GroupName: ld.SimultaneousGroup,
				})
			}
		}
	}

	for g := 1; g <= 3; g++ {
		for cls := range classSets[g] {
			cat.GradeClassRooms[g] = append(cat.GradeClassRooms[g], cls)
		}
		sort.Strings(cat.GradeClassRooms[g])
	}
	for rm := range specialSet {
		cat.SpecialRooms = append(cat.SpecialRooms, rm)
	}
	sort.Strings(cat.SpecialRooms)
	sort.Slice(cat.ElectiveRooms, func(i, j int) bool {
		if cat.ElectiveRooms[i].Grade != cat.ElectiveRooms[j].Grade {
			return cat.ElectiveRooms[i].Grade < cat.ElectiveRooms[j].Grade
		}
		return cat.ElectiveRooms[i].Label < cat.ElectiveRooms[j].Label
	})
	return cat
}
