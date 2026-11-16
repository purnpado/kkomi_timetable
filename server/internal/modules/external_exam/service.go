package external_exam

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 대외 시험감독(수능·모평·검정고시·임용 등) 공고, 자발적 지원 및
// 학교별 맞춤형 다단계 차출 순번 산정기를 담당합니다. (계좌/수당 기능 없음)
type Service struct {
	store *database.Store
	bus   *eventbus.Bus
}

// NewService는 외부 시험감독 모듈 서비스를 생성합니다.
func NewService(store *database.Store, bus *eventbus.Bus) *Service {
	return &Service{store: store, bus: bus}
}

// ListNotices는 외부 시험 공고 목록을 반환합니다.
func (s *Service) ListNotices() ([]protocol.ExternalExamNotice, error) {
	return database.LoadCollection[protocol.ExternalExamNotice](s.store, "external_exams")
}

// CreateNotice는 신규 대외 시험 공고를 등록합니다.
func (s *Service) CreateNotice(notice protocol.ExternalExamNotice) (protocol.ExternalExamNotice, error) {
	if notice.ID == "" {
		notice.ID = fmt.Sprintf("ext-%d", time.Now().UnixNano())
	}
	if len(notice.PriorityOrder) == 0 {
		notice.PriorityOrder = PresetOrder(notice.PresetMode)
	}
	if err := s.store.SaveRecord("external_exams", notice.ID, notice); err != nil {
		return notice, err
	}
	return notice, nil
}

// PresetOrder는 학교별 차출 프리셋 이름에 따른 기본 우선순위 조건 배열을 반환합니다.
func PresetOrder(presetMode string) []protocol.ExternalExamCriteria {
	switch presetMode {
	case "PRESET_COOLDOWN": // 방식 B: 순번/쿨타임형 (미파견/오래된 순 우선)
		return []protocol.ExternalExamCriteria{
			protocol.CriteriaVolunteer,
			protocol.CriteriaOldestDispatch,
			protocol.CriteriaLateTransfer,
			protocol.CriteriaLowCount,
		}
	case "PRESET_RECENT_TRANSFER": // 방식 C: 최근 전입자 우선형
		return []protocol.ExternalExamCriteria{
			protocol.CriteriaVolunteer,
			protocol.CriteriaLateTransfer,
			protocol.CriteriaOldestDispatch,
			protocol.CriteriaNonHomeroom,
		}
	default: // 방식 A: 혼합형(권장)
		return []protocol.ExternalExamCriteria{
			protocol.CriteriaVolunteer,
			protocol.CriteriaLowCount,
			protocol.CriteriaLateTransfer,
			protocol.CriteriaOldestDispatch,
		}
	}
}

// ToggleVolunteer는 교사의 자발적 외부감독 지원 또는 지원 취소를 처리합니다.
func (s *Service) ToggleVolunteer(noticeID, teacherID string) (*protocol.ExternalExamNotice, error) {
	notices, err := s.ListNotices()
	if err != nil {
		return nil, err
	}
	for i := range notices {
		if notices[i].ID == noticeID {
			if slices.Contains(notices[i].VolunteerIDs, teacherID) {
				filtered := make([]string, 0, len(notices[i].VolunteerIDs))
				for _, id := range notices[i].VolunteerIDs {
					if id != teacherID {
						filtered = append(filtered, id)
					}
				}
				notices[i].VolunteerIDs = filtered
			} else {
				notices[i].VolunteerIDs = append(notices[i].VolunteerIDs, teacherID)
			}
			if err := s.store.SaveRecord("external_exams", notices[i].ID, notices[i]); err != nil {
				return nil, err
			}
			return &notices[i], nil
		}
	}
	return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
}

// UpdateRules는 담당교사가 학교 내규에 맞게 차출 기준 프리셋 또는 커스텀 우선순위 순서를 변경할 때 호출됩니다.
func (s *Service) UpdateRules(noticeID, presetMode string, priorityOrder []protocol.ExternalExamCriteria, excludeExempt bool) (*protocol.ExternalExamNotice, error) {
	notices, err := s.ListNotices()
	if err != nil {
		return nil, err
	}
	for i := range notices {
		if notices[i].ID == noticeID {
			notices[i].PresetMode = presetMode
			if presetMode != "CUSTOM" && len(priorityOrder) == 0 {
				notices[i].PriorityOrder = PresetOrder(presetMode)
			} else {
				notices[i].PriorityOrder = priorityOrder
			}
			notices[i].ExcludeExempt = excludeExempt
			if err := s.store.SaveRecord("external_exams", notices[i].ID, notices[i]); err != nil {
				return nil, err
			}
			return &notices[i], nil
		}
	}
	return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
}

// CalculateCandidates는 다단계 차출 우선순위 조합기를 통해 모든 교사의 순번과 투명한 산정 근거 배지를 계산합니다.
func (s *Service) CalculateCandidates(noticeID string) ([]protocol.ExternalExamCandidate, *protocol.ExternalExamNotice, error) {
	notices, err := s.ListNotices()
	if err != nil {
		return nil, nil, err
	}
	var notice *protocol.ExternalExamNotice
	for i := range notices {
		if notices[i].ID == noticeID {
			notice = &notices[i]
			break
		}
	}
	if notice == nil {
		return nil, nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, nil, err
	}

	order := notice.PriorityOrder
	if len(order) == 0 {
		order = PresetOrder(notice.PresetMode)
	}
	now := time.Now()
	var candidates []protocol.ExternalExamCandidate

	for _, t := range teachers {
		if t.IsParentHonorary {
			continue // 학부모 명예감독은 외부 교원 파견 대상에서 제외
		}
		isVol := slices.Contains(notice.VolunteerIDs, t.ID)
		isExempt := len(t.ExemptReasons) > 0
		isSel := slices.Contains(notice.ConfirmedIDs, t.ID)
		days := daysSince(t.LastExternalDispatch, now)

		candidates = append(candidates, protocol.ExternalExamCandidate{
			Teacher:       t,
			IsVolunteer:   isVol,
			IsExempt:      isExempt,
			IsSelected:    isSel,
			DaysSinceLast: days,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		// 면제 대상자 제외 옵션이 켜져 있으면 면제자는 무조건 후순위(또는 비활성)로 이동
		if notice.ExcludeExempt && a.IsExempt != b.IsExempt {
			return !a.IsExempt
		}
		// 설정된 우선순위 조건 배열 순차 비교
		for _, crit := range order {
			if cmp := compareByCriteria(a, b, crit); cmp != 0 {
				return cmp < 0
			}
		}
		return a.Teacher.ID < b.Teacher.ID
	})

	draftSeq := 1
	for i := range candidates {
		candidates[i].Rank = i + 1
		candidates[i].ReasonBadge = buildReasonBadge(candidates[i], draftSeq)
		if !candidates[i].IsVolunteer && !candidates[i].IsExempt {
			draftSeq++
		}
	}
	return candidates, notice, nil
}

// ConfirmDispatch는 외부 시험감독 담당교사가 [최종 선택(파견 명단 확정)]을 실행하여
// 확정 명단을 저장하고 교사 누적 횟수/최근 파견일을 갱신하며 시간표 결보강 대기로 자동 이관합니다.
func (s *Service) ConfirmDispatch(noticeID string, selectedIDs []string, actorName string) (*protocol.ExternalExamNotice, error) {
	notices, err := s.ListNotices()
	if err != nil {
		return nil, err
	}
	var notice *protocol.ExternalExamNotice
	for i := range notices {
		if notices[i].ID == noticeID {
			notice = &notices[i]
			break
		}
	}
	if notice == nil {
		return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
	}

	notice.ConfirmedIDs = selectedIDs
	notice.IsConfirmed = true
	notice.LinkedToTimetable = true

	// 확정된 교사의 누적 파견 횟수 및 마지막 파견일 갱신
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	for i := range teachers {
		if slices.Contains(selectedIDs, teachers[i].ID) {
			teachers[i].ExternalExamCount++
			if notice.ExamDate > teachers[i].LastExternalDispatch {
				teachers[i].LastExternalDispatch = notice.ExamDate
			}
			_ = s.store.SaveRecord("teachers", teachers[i].ID, teachers[i])
		}
	}

	if err := s.store.SaveRecord("external_exams", notice.ID, *notice); err != nil {
		return nil, err
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExternalExamConfirmed,
			Title:     fmt.Sprintf("[%s] 외부 시험감독 파견 명단 확정 (%d명)", notice.Title, len(selectedIDs)),
			Message:   fmt.Sprintf("파견 교사의 %s 평일 수업은 시간표 담당교사 결보강 대기열로 자동 이관되었습니다.", notice.ExamDate),
			Urgent:    true,
			TargetIDs: selectedIDs,
			ActorName: actorName,
			Payload:   *notice,
		})
	}
	return notice, nil
}

func compareByCriteria(a, b protocol.ExternalExamCandidate, crit protocol.ExternalExamCriteria) int {
	switch crit {
	case protocol.CriteriaVolunteer:
		if a.IsVolunteer != b.IsVolunteer {
			if a.IsVolunteer {
				return -1
			}
			return 1
		}
	case protocol.CriteriaLowCount:
		if a.Teacher.ExternalExamCount != b.Teacher.ExternalExamCount {
			if a.Teacher.ExternalExamCount < b.Teacher.ExternalExamCount {
				return -1
			}
			return 1
		}
	case protocol.CriteriaLateTransfer:
		// 전입일이 더 늦을수록(문자열 날짜가 클수록) 우선
		if a.Teacher.TransferDate != b.Teacher.TransferDate {
			if a.Teacher.TransferDate > b.Teacher.TransferDate {
				return -1
			}
			return 1
		}
	case protocol.CriteriaOldestDispatch:
		// 경과 일수가 클수록(미파견 99999 포함) 우선
		if a.DaysSinceLast != b.DaysSinceLast {
			if a.DaysSinceLast > b.DaysSinceLast {
				return -1
			}
			return 1
		}
	case protocol.CriteriaNonHomeroom:
		aNon := a.Teacher.HomeroomClass == ""
		bNon := b.Teacher.HomeroomClass == ""
		if aNon != bNon {
			if aNon {
				return -1
			}
			return 1
		}
	case protocol.CriteriaNonDeptHead:
		if a.Teacher.IsDepartmentHead != b.Teacher.IsDepartmentHead {
			if !a.Teacher.IsDepartmentHead {
				return -1
			}
			return 1
		}
	}
	return 0
}

func daysSince(dateStr string, now time.Time) int {
	if strings.TrimSpace(dateStr) == "" {
		return 99999 // 미경험자(미파견) 최우선
	}
	parsed, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return 99999
	}
	days := int(now.Sub(parsed).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

func buildReasonBadge(c protocol.ExternalExamCandidate, draftSeq int) string {
	if c.IsExempt {
		return fmt.Sprintf("[면제 대상: %s]", strings.Join(c.Teacher.ExemptReasons, ", "))
	}
	if c.IsVolunteer {
		return fmt.Sprintf("[1순위: 자발적 지원 · 누적 %d회]", c.Teacher.ExternalExamCount)
	}
	lastTxt := "미파견"
	if c.Teacher.LastExternalDispatch != "" {
		lastTxt = fmt.Sprintf("최근 %s (%d일 경과)", c.Teacher.LastExternalDispatch, c.DaysSinceLast)
	}
	return fmt.Sprintf("[차출후보 %d번: %s 전입 · 누적 %d회 · %s]", draftSeq, c.Teacher.TransferDate, c.Teacher.ExternalExamCount, lastTxt)
}
