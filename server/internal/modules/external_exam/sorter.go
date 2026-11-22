package external_exam

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
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

// CalculateCandidates는 가중치 점수제·별도 트랙·전입 유예·종목 제외·초과 선발 및 동점 기준을 반영해 차출 순번을 계산합니다.
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
	cfg, _ := s.GetSchoolConfig()

	weightedMap := make(map[string]float64)
	confirmedAllCount := make(map[string]int)
	catCountMap := make(map[string]int)
	catLastDateMap := make(map[string]string)

	for _, n := range notices {
		if !n.IsConfirmed {
			continue
		}
		w := categoryWeight(cfg, n.ExamCategory)
		sameCat := strings.EqualFold(strings.TrimSpace(n.ExamCategory), strings.TrimSpace(notice.ExamCategory))
		for _, tid := range n.ConfirmedIDs {
			weightedMap[tid] += w
			confirmedAllCount[tid]++
			if sameCat {
				catCountMap[tid]++
				if n.ExamDate > catLastDateMap[tid] {
					catLastDateMap[tid] = n.ExamDate
				}
			}
		}
	}

	volunteerOrder := make(map[string]int, len(notice.VolunteerIDs))
	for i, tid := range notice.VolunteerIDs {
		volunteerOrder[tid] = i
	}

	order := notice.PriorityOrder
	if len(order) == 0 {
		order = PresetOrder(notice.PresetMode)
	}
	now := time.Now()
	examYear := now.Year()
	if len(notice.ExamDate) >= 4 {
		if y, yErr := strconv.Atoi(notice.ExamDate[:4]); yErr == nil && y > 2000 {
			examYear = y
		}
	}

	type candWrap struct {
		cand       protocol.ExternalExamCandidate
		catReasons []string
	}
	var wrapped []candWrap

	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			continue
		}
		isVol := slices.Contains(notice.VolunteerIDs, t.ID)
		isExempt := len(t.ExemptReasons) > 0
		var catReasons []string
		for _, rule := range notice.CategoryExcludeRules {
			if hit, rsn := matchesCategoryExcludeRule(t, rule, examYear); hit {
				isExempt = true
				catReasons = append(catReasons, rsn)
			}
		}
		isSel := slices.Contains(notice.ConfirmedIDs, t.ID)
		refLastDate := t.LastExternalDispatch
		if notice.ScoringMode == "SEPARATE_TRACK" {
			refLastDate = catLastDateMap[t.ID]
		}
		days := daysSince(refLastDate, now)

		effScore := weightedMap[t.ID]
		if unrecorded := t.ExternalExamCount - confirmedAllCount[t.ID]; unrecorded > 0 {
			effScore += float64(unrecorded)
		}
		isGrace := notice.TransferGraceYear && isTransferGracePeriod(t.TransferDate, notice.ExamDate)

		wrapped = append(wrapped, candWrap{
			cand: protocol.ExternalExamCandidate{
				Teacher:        t,
				IsVolunteer:    isVol,
				IsExempt:       isExempt,
				IsSelected:     isSel,
				IsGracePeriod:  isGrace,
				EffectiveScore: math.Round(effScore*10) / 10,
				CategoryCount:  catCountMap[t.ID],
				DaysSinceLast:  days,
			},
			catReasons: catReasons,
		})
	}

	volOverflow := len(notice.VolunteerIDs) > notice.RequiredCount
	sort.Slice(wrapped, func(i, j int) bool {
		a, b := wrapped[i].cand, wrapped[j].cand
		if notice.ExcludeExempt && a.IsExempt != b.IsExempt {
			return !a.IsExempt
		}
		if notice.TransferGraceYear && !a.IsVolunteer && !b.IsVolunteer && a.IsGracePeriod != b.IsGracePeriod {
			return !a.IsGracePeriod
		}
		for _, crit := range order {
			if crit == protocol.CriteriaVolunteer && a.IsVolunteer && b.IsVolunteer && volOverflow {
				if ovCmp := compareVolunteersOverflow(a, b, notice, volunteerOrder); ovCmp != 0 {
					return ovCmp < 0
				}
			}
			if cmp := compareByCriteria(a, b, crit, notice.ScoringMode); cmp != 0 {
				return cmp < 0
			}
		}
		if tie := compareTieBreaker(a, b, notice); tie != 0 {
			return tie < 0
		}
		return a.Teacher.ID < b.Teacher.ID
	})

	candidates := make([]protocol.ExternalExamCandidate, len(wrapped))
	draftSeq := 1
	for i := range wrapped {
		c := wrapped[i].cand
		c.Rank = i + 1
		c.ReasonBadge = buildReasonBadge(c, notice, wrapped[i].catReasons, draftSeq)
		if !c.IsVolunteer && !c.IsExempt && !c.IsGracePeriod {
			draftSeq++
		}
		candidates[i] = c
	}
	return candidates, notice, nil
}

func compareByCriteria(a, b protocol.ExternalExamCandidate, crit protocol.ExternalExamCriteria, scoringMode string) int {
	switch crit {
	case protocol.CriteriaVolunteer:
		if a.IsVolunteer != b.IsVolunteer {
			if a.IsVolunteer {
				return -1
			}
			return 1
		}
	case protocol.CriteriaLowCount:
		switch scoringMode {
		case "WEIGHTED_SCORE":
			if math.Abs(a.EffectiveScore-b.EffectiveScore) > 0.001 {
				if a.EffectiveScore < b.EffectiveScore {
					return -1
				}
				return 1
			}
		case "SEPARATE_TRACK":
			if a.CategoryCount != b.CategoryCount {
				if a.CategoryCount < b.CategoryCount {
					return -1
				}
				return 1
			}
		default:
			if a.Teacher.ExternalExamCount != b.Teacher.ExternalExamCount {
				if a.Teacher.ExternalExamCount < b.Teacher.ExternalExamCount {
					return -1
				}
				return 1
			}
		}
	case protocol.CriteriaLateTransfer, protocol.CriteriaEarlyTransfer:
		at := normalizeDateCmp(a.Teacher.TransferDate)
		bt := normalizeDateCmp(b.Teacher.TransferDate)
		if at != bt {
			if crit == protocol.CriteriaEarlyTransfer {
				if at < bt {
					return -1
				}
				return 1
			}
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

func buildReasonBadge(c protocol.ExternalExamCandidate, notice *protocol.ExternalExamNotice, catReasons []string, draftSeq int) string {
	scoreInfo := fmt.Sprintf("누적 %d회", c.Teacher.ExternalExamCount)
	switch notice.ScoringMode {
	case "WEIGHTED_SCORE":
		scoreInfo = fmt.Sprintf("환산 %.1f점", c.EffectiveScore)
	case "SEPARATE_TRACK":
		scoreInfo = fmt.Sprintf("동종목 %d회", c.CategoryCount)
	}
	if c.IsExempt {
		allReasons := append([]string(nil), c.Teacher.ExemptReasons...)
		allReasons = append(allReasons, catReasons...)
		return fmt.Sprintf("[면제/제외: %s]", strings.Join(allReasons, ", "))
	}
	if c.IsVolunteer {
		return fmt.Sprintf("[1순위: 자발적 지원 · %s]", scoreInfo)
	}
	if c.IsGracePeriod {
		return fmt.Sprintf("[전입 1년차 유예: %s 전입 · %s]", c.Teacher.TransferDate, scoreInfo)
	}
	lastTxt := "미파견"
	if c.Teacher.LastExternalDispatch != "" {
		lastTxt = fmt.Sprintf("최근 %s (%d일 경과)", c.Teacher.LastExternalDispatch, c.DaysSinceLast)
	}
	return fmt.Sprintf("[차출후보 %d번: %s 전입 · %s · %s]", draftSeq, c.Teacher.TransferDate, scoreInfo, lastTxt)
}
