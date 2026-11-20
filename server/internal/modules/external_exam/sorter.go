package external_exam

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

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
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			continue // 학부모 명예감독 및 시스템 기본 최고관리자(admin)는 외부 파견 대상에서 제외
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
		if notice.ExcludeExempt && a.IsExempt != b.IsExempt {
			return !a.IsExempt
		}
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
		at := normalizeDateCmp(a.Teacher.TransferDate)
		bt := normalizeDateCmp(b.Teacher.TransferDate)
		if at != bt {
			if at > bt {
				return -1
			}
			return 1
		}
	case protocol.CriteriaOldestDispatch:
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

func normalizeDateCmp(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.NewReplacer(".", "-", "/", "-").Replace(s)
	parts := strings.Split(s, "-")
	if len(parts) == 3 {
		return fmt.Sprintf("%04s-%02s-%02s", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]))
	}
	return s
}

func daysSince(dateStr string, now time.Time) int {
	if strings.TrimSpace(dateStr) == "" {
		return 99999
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
