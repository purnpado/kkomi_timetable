package internal_exam

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 교내 지필고사 감독 배정, 사전 봉인(Sealed)/당일 아침 일괄 공개(Unseal),
// 그리고 당일 돌발 결원 긴급 대타 추천/교체를 담당합니다.
type Service struct {
	store    *database.Store
	bus      *eventbus.Bus
	stop     chan struct{}
	stopOnce sync.Once
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
		if specs[i].ExamDate != specs[j].ExamDate {
			return specs[i].ExamDate < specs[j].ExamDate
		}
		if specs[i].Period != specs[j].Period {
			return specs[i].Period < specs[j].Period
		}
		return specs[i].Grade < specs[j].Grade
	})
	return specs
}

// StartScheduler는 시험 당일 지정 시각(기본 08:20) 도래 시 봉인된 고사장을 자동 일괄 공개하는 백그라운드 워커를 시작합니다.
func (s *Service) StartScheduler() {
	s.checkAndUnsealDue(time.Now())
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case now := <-ticker.C:
				s.checkAndUnsealDue(now)
			}
		}
	}()
}

func (s *Service) checkAndUnsealDue(now time.Time) {
	cfgs, err := database.LoadCollection[protocol.ExamConfig](s.store, "exam_configs")
	if err != nil {
		return
	}
	today := now.Format("2006-01-02")
	hhmm := now.Format("15:04")
	for _, cfg := range cfgs {
		if !cfg.IsSealed || cfg.InstantRevealMode || cfg.UnsealTime == "" || hhmm < cfg.UnsealTime {
			continue
		}
		dates := collectExamDates(cfg, nil)
		if len(dates) > 1 {
			if slices.Contains(dates, today) && !slices.Contains(cfg.UnsealedDates, today) {
				_, _ = s.UnsealExamDate(today, "자동공개 스케줄러")
			}
		} else if cfg.ExamDate == today {
			_, _ = s.UnsealExam(cfg.ID, "자동공개 스케줄러")
		}
	}
}

// StopScheduler는 스케줄러 고루틴을 안전하게 종료합니다.
func (s *Service) StopScheduler() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

// GetConfig는 현재 교내 시험 설정을 조회합니다. (초기화 직후 설정이 없으면 안전한 기본 초안을 반환)
func (s *Service) GetConfig() (protocol.ExamConfig, error) {
	cfgs, err := database.LoadCollection[protocol.ExamConfig](s.store, "exam_configs")
	if err != nil {
		return protocol.ExamConfig{}, err
	}
	if len(cfgs) == 0 {
		return protocol.ExamConfig{
			ID:                   "exam-draft",
			Title:                "신규 지필고사 (편성 대기)",
			UnsealTime:           "08:20",
			IsSealed:             true,
			SubjectTeacherPolicy: protocol.SubjectModeCorridorFirst,
		}, nil
	}
	return cfgs[0], nil
}

// GenerateAndSave는 배정 옵션 및 당일 부재 교사 제약을 반영해 감독 배정표를 원자적으로 생성합니다.
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
	cfg.CountsCommitted = false
	cfg.UnsealedDates = nil
	cfg.MaxDailyPerTeacher = max(0, cfg.MaxDailyPerTeacher)
	if len(specs) == 0 {
		specs = s.ListSpecs()
	}
	for i := range specs {
		if strings.TrimSpace(specs[i].ExamDate) == "" && strings.TrimSpace(cfg.ExamDate) != "" {
			specs[i].ExamDate = strings.TrimSpace(cfg.ExamDate)
		}
	}
	cfg.ExamDates = collectExamDates(cfg, specs)
	if strings.TrimSpace(cfg.ExamDate) == "" && len(cfg.ExamDates) > 0 {
		cfg.ExamDate = cfg.ExamDates[0]
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	unavailByDate := buildUnavailableByDate(s.store, cfg.ExamDates)
	slots := AssignSlotsMultiDay(cfg, specs, teachers, unavailByDate)

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if cErr := tx.ClearCollections("exam_specs", "exam_configs", "exam_slots"); cErr != nil {
			return cErr
		}
		for i, sp := range specs {
			if sErr := tx.SaveRecord("exam_specs", fmt.Sprintf("spec-%d", i+1), sp); sErr != nil {
				return sErr
			}
		}
		if sErr := tx.SaveRecord("exam_configs", cfg.ID, cfg); sErr != nil {
			return sErr
		}
		for _, slot := range slots {
			if sErr := tx.SaveRecord("exam_slots", slot.ID, slot); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if s.bus != nil {
		if cfg.IsSealed {
			msg := fmt.Sprintf("감독 교시만 안내되며 고사장은 %s 당일 %s에 일괄 공개됩니다.", cfg.ExamDate, cfg.UnsealTime)
			if len(cfg.ExamDates) > 1 {
				msg = fmt.Sprintf("%d일간 시험(%s~) 감독 배정이 봉인되었으며 각 시험 당일 %s에 순차 공개됩니다.", len(cfg.ExamDates), cfg.ExamDates[0], cfg.UnsealTime)
			}
			s.bus.Publish(protocol.DomainEvent{
				Type:    protocol.EventExamSealed,
				Title:   fmt.Sprintf("[%s] 감독 배정 사전 봉인 완료", cfg.Title),
				Message: msg,
				Payload: cfg,
			})
		}
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventExamScheduleUpdated,
			Title:   fmt.Sprintf("[%s] 교내 시험감독 배정표 갱신", cfg.Title),
			Message: fmt.Sprintf("총 %d개 감독 슬롯이 생성되었습니다.", len(slots)),
			Payload: map[string]interface{}{"examId": cfg.ID, "slotCount": len(slots)},
		})
	}
	return slots, nil
}

// ListSlotsForViewer는 조회자의 권한(교내 시험감독 관리자 vs 일반 교사)에 따라
// 봉인 상태인 시험일의 고사장명을 서버 패킷 레벨에서 마스킹("🔒 당일 08:20 공개")해 반환합니다.
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
		if slots[i].ExamDate != slots[j].ExamDate {
			return slots[i].ExamDate < slots[j].ExamDate
		}
		if slots[i].Period != slots[j].Period {
			return slots[i].Period < slots[j].Period
		}
		return slots[i].ID < slots[j].ID
	})

	if cfg.IsSealed && !isExamAdmin {
		maskedMsg := fmt.Sprintf("🔒 당일 %s 공개", cfg.UnsealTime)
		for i := range slots {
			if isSlotSealed(cfg, slots[i].ExamDate) {
				slots[i].RoomName = maskedMsg
				slots[i].ActualRoomHidden = true
			}
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

	unavail := buildUnavailableTeachersForDate(s.store, target.ExamDate)
	busyTeachers := make(map[string]bool)
	for _, sl := range slots {
		if sl.Period == target.Period && (target.ExamDate == "" || sl.ExamDate == "" || sl.ExamDate == target.ExamDate) {
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
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) || busyTeachers[t.ID] || unavail[t.ID] != "" {
			continue
		}
		if exempt, _ := IsEffectiveExamExempt(t); exempt {
			continue
		}
		if target.SlotRole != "CORRIDOR" && t.HomeroomClass != "" && roomPrefix != "" && strings.EqualFold(t.HomeroomClass, roomPrefix) {
			continue
		}
		if target.SlotRole != "CORRIDOR" && roomOrSpecMatchesTeacherSubject(target.RoomName, target.Subject, t.Subject) {
			continue
		}
		recs = append(recs, t)
	}
	usage := buildSlotUsageMaps(slots)
	cfg, _ := s.GetConfig()
	sort.Slice(recs, func(i, j int) bool {
		if cfg.MaxDailyPerTeacher > 0 {
			capI := usage.dailyCount[target.ExamDate][recs[i].ID] >= cfg.MaxDailyPerTeacher
			capJ := usage.dailyCount[target.ExamDate][recs[j].ID] >= cfg.MaxDailyPerTeacher
			if capI != capJ {
				return !capI
			}
		}
		consecI := isConsecutivePeriod(usage.dailyPeriods[target.ExamDate][recs[i].ID], target.Period)
		consecJ := isConsecutivePeriod(usage.dailyPeriods[target.ExamDate][recs[j].ID], target.Period)
		if consecI != consecJ {
			return !consecI
		}
		if (recs[i].HomeroomClass == "") != (recs[j].HomeroomClass == "") {
			return recs[i].HomeroomClass == ""
		}
		return recs[i].InternalExamCount < recs[j].InternalExamCount
	})
	return recs, nil
}

// ApplySubstitute는 당일 돌발 결원 슬롯을 검증 후 대체 교사로 교체하고, 봉인 상태를 준수하며 알림을 보냅니다.
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
			unavail := buildUnavailableTeachersForDate(s.store, slots[i].ExamDate)
			if vErr := validateSubstituteTeacher(slots[i], *newTeacher, slots, unavail); vErr != nil {
				return nil, vErr
			}
			oldTeacherID := slots[i].TeacherID
			oldName := slots[i].TeacherName
			slots[i].TeacherID = newTeacher.ID
			slots[i].TeacherName = newTeacher.Name
			slots[i].Note = fmt.Sprintf("긴급대타 (기존: %s)", oldName)
			if err := s.store.SaveRecord("exam_slots", slots[i].ID, slots[i]); err != nil {
				return nil, err
			}
			if s.bus != nil {
				cfg, _ := s.GetConfig()
				evtSlot := slots[i]
				roomText := evtSlot.RoomName
				if isSlotSealed(cfg, evtSlot.ExamDate) {
					roomText = fmt.Sprintf("🔒 당일 %s 공개", cfg.UnsealTime)
					evtSlot.RoomName = roomText
					evtSlot.ActualRoomHidden = true
				}
				var targetIDs []string
				if newTeacher.ID != "" {
					targetIDs = append(targetIDs, newTeacher.ID)
				}
				if oldTeacherID != "" && oldTeacherID != newTeacher.ID {
					targetIDs = append(targetIDs, oldTeacherID)
				}
				s.bus.Publish(protocol.DomainEvent{
					Type:      protocol.EventExamSubstituted,
					Title:     fmt.Sprintf("🚨 [긴급 대타 배정] %d교시 %s", evtSlot.Period, roomText),
					Message:   fmt.Sprintf("%s 선생님 결원으로 %s 선생님이 긴급 감독 배정되었습니다.", oldName, newTeacher.Name),
					Urgent:    true,
					TargetIDs: targetIDs,
					ActorName: actorName,
					Payload:   evtSlot,
				})
			}
			return &slots[i], nil
		}
	}
	return nil, fmt.Errorf("감독 슬롯(%s)을 찾을 수 없습니다", slotID)
}
