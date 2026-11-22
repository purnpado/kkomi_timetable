package event_calendar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 부서별 주중·월중 행사 취합, 행사 담당교사 최종 선택(Commit) 및
// 시종시간 변경·수업블록·출장 자동 연동을 담당합니다.
type Service struct {
	store *database.Store
	bus   *eventbus.Bus
}

// NewService는 학사일정·행사 관리 서비스를 생성합니다.
func NewService(store *database.Store, bus *eventbus.Bus) *Service {
	return &Service{store: store, bus: bus}
}

// ListEvents는 주중·월중 행사 목록을 일자순으로 반환합니다.
func (s *Service) ListEvents() ([]protocol.SchoolEvent, error) {
	events, err := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].EventDate != events[j].EventDate {
			return events[i].EventDate < events[j].EventDate
		}
		return events[i].StartPeriod < events[j].StartPeriod
	})
	return events, nil
}

// ListBellSchedule은 현재 적용 중인 정규 교시별 시종시간표를 반환합니다.
func (s *Service) ListBellSchedule() ([]protocol.BellPeriod, error) {
	bells, err := database.LoadCollection[protocol.BellPeriod](s.store, "bell_schedule")
	if err != nil {
		return nil, err
	}
	sort.Slice(bells, func(i, j int) bool {
		return bells[i].Period < bells[j].Period
	})
	return bells, nil
}

// EffectiveBellSchedule은 정규 시종시간표를 바탕으로 지정 일자(dateStr)에 확정된
// [시종시간 변경] 행사 오버라이드를 동적으로 합성해 반환합니다. (정규 마스터 DB는 오염되지 않음)
func (s *Service) EffectiveBellSchedule(dateStr string) ([]protocol.BellPeriod, error) {
	targetDate := strings.TrimSpace(dateStr)
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	}
	base, err := s.ListBellSchedule()
	if err != nil {
		return nil, err
	}
	events, err := s.ListEvents()
	if err != nil {
		return base, nil
	}

	merged := make(map[int]protocol.BellPeriod, len(base))
	for _, b := range base {
		if b.Period > 0 {
			merged[b.Period] = b
		}
	}
	for _, ev := range events {
		if ev.Status != protocol.StatusApproved || ev.LinkType != protocol.EventLinkBellChange || !protocol.EventCoversDate(ev, targetDate) {
			continue
		}
		for _, ov := range ev.BellOverride {
			if ov.Period <= 0 {
				continue
			}
			if ov.Label == "" {
				if prev, ok := merged[ov.Period]; ok && prev.Label != "" {
					ov.Label = prev.Label
				} else {
					ov.Label = fmt.Sprintf("%d교시", ov.Period)
				}
			}
			merged[ov.Period] = ov
		}
	}

	out := make([]protocol.BellPeriod, 0, len(merged))
	for _, b := range merged {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Period < out[j].Period
	})
	return out, nil
}

// SaveBellSchedule은 마스터 또는 시간표 담당교사가 정규 일과시간(1~7교시 시작·종료 시각)을 저장합니다.
func (s *Service) SaveBellSchedule(bells []protocol.BellPeriod, actorName string) ([]protocol.BellPeriod, error) {
	if len(bells) == 0 {
		return nil, fmt.Errorf("저장할 일과시간 데이터가 비어 있습니다")
	}
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if err := tx.ClearCollections("bell_schedule"); err != nil {
			return err
		}
		for _, b := range bells {
			if b.Period <= 0 {
				continue
			}
			if b.Label == "" {
				b.Label = fmt.Sprintf("%d교시", b.Period)
			}
			if err := tx.SaveRecord("bell_schedule", fmt.Sprintf("period-%d", b.Period), b); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	saved, err := s.ListBellSchedule()
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventBellScheduleChanged,
			Title:     "⏰ 정규 일과시간표(시종시간) 변경 반영",
			Message:   "1~7교시 일과시간 설정이 갱신되어 전 교원 시간표 및 방해 금지 타이머에 적용되었습니다.",
			ActorName: actorName,
			Payload:   saved,
		})
	}
	return saved, nil
}

func normalizeAndValidateEvent(ev protocol.SchoolEvent) (protocol.SchoolEvent, error) {
	ev.Title = strings.TrimSpace(ev.Title)
	ev.Department = strings.TrimSpace(ev.Department)
	ev.EventDate = strings.TrimSpace(ev.EventDate)
	ev.EndDate = strings.TrimSpace(ev.EndDate)
	ev.Location = strings.TrimSpace(ev.Location)
	ev.HolidayKind = strings.TrimSpace(ev.HolidayKind)
	ev.BellPreset = strings.TrimSpace(ev.BellPreset)
	ev.Recurrence = strings.ToUpper(strings.TrimSpace(ev.Recurrence))
	ev.RecurrenceEnd = strings.TrimSpace(ev.RecurrenceEnd)

	if ev.Title == "" {
		return ev, fmt.Errorf("행사명을 입력해 주세요")
	}
	if _, err := time.Parse("2006-01-02", ev.EventDate); err != nil {
		return ev, fmt.Errorf("행사 일자(YYYY-MM-DD) 형식이 올바르지 않습니다")
	}
	if ev.EndDate == "" {
		ev.EndDate = ev.EventDate
	} else {
		if _, err := time.Parse("2006-01-02", ev.EndDate); err != nil {
			return ev, fmt.Errorf("종료 일자(YYYY-MM-DD) 형식이 올바르지 않습니다")
		}
		if ev.EndDate < ev.EventDate {
			return ev, fmt.Errorf("종료 일자(%s)는 시작 일자(%s)보다 같거나 이후여야 합니다", ev.EndDate, ev.EventDate)
		}
	}
	if ev.StartPeriod < 0 || ev.EndPeriod < 0 {
		return ev, fmt.Errorf("교시는 0(종일) 이상이어야 합니다")
	}
	if ev.StartPeriod > 0 && ev.EndPeriod == 0 {
		ev.EndPeriod = ev.StartPeriod
	}
	if ev.StartPeriod > 0 && ev.EndPeriod < ev.StartPeriod {
		return ev, fmt.Errorf("종료 교시(%d)는 시작 교시(%d)보다 크거나 같아야 합니다", ev.EndPeriod, ev.StartPeriod)
	}
	if ev.LinkType == "" {
		ev.LinkType = protocol.EventLinkSimpleNotice
	}
	if ev.LinkType == protocol.EventLinkHoliday {
		ev.StartPeriod = 0
		ev.EndPeriod = 0
		if len(ev.TargetGrades) == 0 && len(ev.TargetClasses) == 0 {
			ev.TargetGrades = []int{1, 2, 3}
		}
		if ev.HolidayKind == "" {
			ev.HolidayKind = "PUBLIC_HOLIDAY"
		}
	}
	if ev.LinkType == protocol.EventLinkBellChange && ev.BellPreset != "" && len(ev.BellOverride) == 0 {
		if periods := BuildBellOverrideFromPreset(ev.BellPreset); len(periods) > 0 {
			ev.BellOverride = periods
		}
	}
	return ev, nil
}

// ProposeEvent는 부서별로 행사를 등록(STAGING 대기열)합니다. 반복 일정이 지정되면 일괄 생성합니다.
func (s *Service) ProposeEvent(ev protocol.SchoolEvent) (protocol.SchoolEvent, error) {
	norm, err := normalizeAndValidateEvent(ev)
	if err != nil {
		return ev, err
	}
	baseID := strings.TrimSpace(norm.ID)
	if baseID == "" {
		baseID = fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	norm.Status = protocol.StatusStaging

	if pairs := expandRecurrencePairs(norm); len(pairs) > 1 {
		groupID := strings.TrimSpace(norm.RecurrenceGroup)
		if groupID == "" {
			groupID = fmt.Sprintf("rec-%d", time.Now().UnixNano())
		}
		var first protocol.SchoolEvent
		err := s.store.WithTx(func(tx *database.TxStore) error {
			for idx, p := range pairs {
				inst := norm
				if idx == 0 {
					inst.ID = baseID
				} else {
					inst.ID = fmt.Sprintf("%s-%d", baseID, idx+1)
				}
				inst.EventDate = p[0]
				inst.EndDate = p[1]
				inst.RecurrenceGroup = groupID
				if sErr := tx.SaveRecord("school_events", inst.ID, inst); sErr != nil {
					return sErr
				}
				if idx == 0 {
					first = inst
				}
			}
			return nil
		})
		if err != nil {
			return norm, err
		}
		return first, nil
	}

	norm.ID = baseID
	if err := s.store.SaveRecord("school_events", norm.ID, norm); err != nil {
		return norm, err
	}
	return norm, nil
}

// UpdateEvent는 등록된 행사를 수정합니다. 행사 담당교사는 모든 행사를 수정 가능하며, 일반 교사는 본인 대기 행사만 수정할 수 있습니다.
func (s *Service) UpdateEvent(ev protocol.SchoolEvent, actorName string, isEventAdmin bool) (protocol.SchoolEvent, error) {
	id := strings.TrimSpace(ev.ID)
	if id == "" {
		return ev, fmt.Errorf("수정할 행사 ID가 필요합니다")
	}
	var saved protocol.SchoolEvent
	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.SchoolEvent](tx, "school_events", id)
		if lErr != nil || loaded == nil {
			return fmt.Errorf("행사(%s)를 찾을 수 없습니다", id)
		}
		if !isEventAdmin {
			if loaded.Status != protocol.StatusStaging || strings.TrimSpace(loaded.ProposedBy) != strings.TrimSpace(actorName) {
				return fmt.Errorf("본인이 등록한 승인 대기 상태의 행사만 수정할 수 있습니다")
			}
		}
		ev.ID = loaded.ID
		ev.Status = loaded.Status
		ev.ProposedBy = loaded.ProposedBy
		ev.CommittedBy = loaded.CommittedBy
		if strings.TrimSpace(ev.RecurrenceGroup) == "" {
			ev.RecurrenceGroup = loaded.RecurrenceGroup
		}
		norm, vErr := normalizeAndValidateEvent(ev)
		if vErr != nil {
			return vErr
		}
		if sErr := tx.SaveRecord("school_events", norm.ID, norm); sErr != nil {
			return sErr
		}
		saved = norm
		return nil
	})
	if err != nil {
		return ev, err
	}
	if saved.Status == protocol.StatusApproved && s.bus != nil {
		if saved.LinkType == protocol.EventLinkBellChange && len(saved.BellOverride) > 0 {
			effective, _ := s.EffectiveBellSchedule(saved.EventDate)
			s.bus.Publish(protocol.DomainEvent{
				Type:      protocol.EventBellScheduleChanged,
				Title:     fmt.Sprintf("⏰ [%s] 시종시간 변경 행사 수정", saved.Title),
				Message:   fmt.Sprintf("%s 시종시간 변경 내용이 갱신되었습니다.", saved.EventDate),
				ActorName: actorName,
				Payload:   effective,
			})
		}
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventSchoolEventPublished,
			Title:     fmt.Sprintf("📅 [%s] 학사 행사 수정 반영", saved.Title),
			Message:   fmt.Sprintf("%s (%s) 확정 행사 정보가 수정되었습니다.", saved.EventDate, saved.Department),
			ActorName: actorName,
			Payload:   saved,
		})
	}
	return saved, nil
}

// CheckConflicts는 등록·수정하려는 행사의 장소 및 동일 학년 교시 충돌 목록을 검사합니다.
func (s *Service) CheckConflicts(ev protocol.SchoolEvent) ([]protocol.EventConflict, error) {
	existing, err := s.ListEvents()
	if err != nil {
		return nil, err
	}
	return DetectConflicts(ev, existing), nil
}

// CommitEvent는 행사 담당교사가 [최종 선택(서버 반영)]을 클릭하여 행사를 확정·반려할 때 호출됩니다.
func (s *Service) CommitEvent(eventID, reviewerName string, approve bool) (*protocol.SchoolEvent, error) {
	var target *protocol.SchoolEvent
	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.SchoolEvent](tx, "school_events", eventID)
		if lErr != nil {
			return fmt.Errorf("행사(%s)를 찾을 수 없습니다", eventID)
		}
		if loaded.Status != protocol.StatusStaging {
			return fmt.Errorf("이미 처리된 행사입니다 (현재 상태: %s)", loaded.Status)
		}
		loaded.CommittedBy = reviewerName
		if approve {
			loaded.Status = protocol.StatusApproved
		} else {
			loaded.Status = protocol.StatusRejected
		}
		if sErr := tx.SaveRecord("school_events", loaded.ID, *loaded); sErr != nil {
			return sErr
		}
		target = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !approve {
		return target, nil
	}

	// [시종시간 변경] 행사도 정규 bell_schedule 마스터는 덮어쓰지 않고 해당 일자 동적 오버레이로 적용
	if target.LinkType == protocol.EventLinkBellChange && len(target.BellOverride) > 0 && s.bus != nil {
		effective, _ := s.EffectiveBellSchedule(target.EventDate)
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventBellScheduleChanged,
			Title:     fmt.Sprintf("⏰ [%s] %s 일자 시종시간 변경 적용", target.Title, target.EventDate),
			Message:   fmt.Sprintf("%s 하루 동안 단축/변경된 시종시간표가 적용됩니다. (정규 시종표 보존)", target.EventDate),
			Urgent:    true,
			ActorName: reviewerName,
			Payload:   effective,
		})
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventSchoolEventPublished,
			Title:     fmt.Sprintf("📅 [%s] 학사 행사 확정 반영", target.Title),
			Message:   fmt.Sprintf("%s (%s) 행사가 확정되어 해당 일자 시간표와 연동되었습니다.", target.EventDate, target.Department),
			ActorName: reviewerName,
			Payload:   *target,
		})
	}
	return target, nil
}

// DeleteEvent는 행사를 삭제하고 연동된 대기열·당일 시종 상태를 정리하도록 도메인 이벤트를 발행합니다.
func (s *Service) DeleteEvent(eventID, actorName string) error {
	if strings.TrimSpace(eventID) == "" {
		return fmt.Errorf("삭제할 행사 ID가 필요합니다")
	}
	target, err := database.LoadRecord[protocol.SchoolEvent](s.store, "school_events", eventID)
	if err != nil || target == nil {
		return fmt.Errorf("행사(%s)를 찾을 수 없습니다", eventID)
	}
	if err := s.store.DeleteRecord("school_events", eventID); err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventSchoolEventDeleted,
			Title:     fmt.Sprintf("🗑 [%s] 학사 행사 삭제", target.Title),
			Message:   fmt.Sprintf("%s 행사가 삭제되어 해당 일자 수업·결보강 대기가 자동 정리되었습니다.", target.EventDate),
			ActorName: actorName,
			Payload:   *target,
		})
		if target.Status == protocol.StatusApproved && target.LinkType == protocol.EventLinkBellChange {
			effective, _ := s.EffectiveBellSchedule(time.Now().Format("2006-01-02"))
			s.bus.Publish(protocol.DomainEvent{
				Type:      protocol.EventBellScheduleChanged,
				Title:     "⏰ 시종시간 변경 행사 삭제로 시종표 원복",
				Message:   fmt.Sprintf("%s 시종 변경 행사가 삭제되어 해당 일자 시종시간표가 복원되었습니다.", target.EventDate),
				ActorName: actorName,
				Payload:   effective,
			})
		}
	}
	return nil
}
