package event_calendar

import (
	"fmt"
	"sort"
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

// ListBellSchedule은 현재 적용 중인 교시별 시종시간표를 반환합니다.
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

// ProposeEvent는 부서별로 행사를 등록(STAGING 대기열)합니다.
func (s *Service) ProposeEvent(ev protocol.SchoolEvent) (protocol.SchoolEvent, error) {
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	ev.Status = protocol.StatusStaging
	if err := s.store.SaveRecord("school_events", ev.ID, ev); err != nil {
		return ev, err
	}
	return ev, nil
}

// CommitEvent는 행사 담당교사가 [최종 선택(서버 반영)]을 클릭하여 행사를 확정할 때 호출됩니다.
func (s *Service) CommitEvent(eventID, reviewerName string, approve bool) (*protocol.SchoolEvent, error) {
	events, err := s.ListEvents()
	if err != nil {
		return nil, err
	}
	var target *protocol.SchoolEvent
	for i := range events {
		if events[i].ID == eventID {
			target = &events[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("행사(%s)를 찾을 수 없습니다", eventID)
	}

	target.CommittedBy = reviewerName
	if !approve {
		target.Status = protocol.StatusRejected
		if err := s.store.SaveRecord("school_events", target.ID, *target); err != nil {
			return nil, err
		}
		return target, nil
	}

	target.Status = protocol.StatusApproved
	if err := s.store.SaveRecord("school_events", target.ID, *target); err != nil {
		return nil, err
	}

	// [시종시간 변경] 속성인 경우 시종시간 마스터 업데이트
	if target.LinkType == protocol.EventLinkBellChange && len(target.BellOverride) > 0 {
		for _, b := range target.BellOverride {
			_ = s.store.SaveRecord("bell_schedule", fmt.Sprintf("period-%d", b.Period), b)
		}
		if s.bus != nil {
			s.bus.Publish(protocol.DomainEvent{
				Type:      protocol.EventBellScheduleChanged,
				Title:     fmt.Sprintf("⏰ [%s] 시종시간 변경 적용", target.Title),
				Message:   "단축/변경된 시종시간표가 전 교원 클라이언트(방해 금지 모드 포함)에 반영되었습니다.",
				Urgent:    true,
				ActorName: reviewerName,
				Payload:   target.BellOverride,
			})
		}
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventSchoolEventPublished,
			Title:     fmt.Sprintf("📅 [%s] 학사 행사 확정 반영", target.Title),
			Message:   fmt.Sprintf("%s (%s) 행사가 확정되어 시간표와 연동되었습니다.", target.EventDate, target.Department),
			ActorName: reviewerName,
			Payload:   *target,
		})
	}
	return target, nil
}
