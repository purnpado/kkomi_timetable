package event_calendar

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
)

// DefaultBellPresets는 학교 행사·단축수업 시 원클릭으로 적용할 수 있는 시종시간 변경 프리셋을 반환합니다.
func DefaultBellPresets() []protocol.BellPresetDef {
	return []protocol.BellPresetDef{
		{
			Key:         "SHORT_40",
			Name:        "40분 단축수업",
			Description: "1~7교시 40분 단축 운영(오전 1~4교시 11:50 종료, 5~7교시 12:40~15:00)",
			Periods: []protocol.BellPeriod{
				{Period: 1, Label: "1교시", StartTime: "08:40", EndTime: "09:20"},
				{Period: 2, Label: "2교시", StartTime: "09:30", EndTime: "10:10"},
				{Period: 3, Label: "3교시", StartTime: "10:20", EndTime: "11:00"},
				{Period: 4, Label: "4교시", StartTime: "11:10", EndTime: "11:50"},
				{Period: 5, Label: "5교시", StartTime: "12:40", EndTime: "13:20"},
				{Period: 6, Label: "6교시", StartTime: "13:30", EndTime: "14:10"},
				{Period: 7, Label: "7교시", StartTime: "14:20", EndTime: "15:00"},
			},
		},
		{
			Key:         "SHORT_45",
			Name:        "45분 단축수업",
			Description: "1~7교시 45분 단축 운영(오전 1~4교시 12:10 종료, 5~7교시 13:00~15:35)",
			Periods: []protocol.BellPeriod{
				{Period: 1, Label: "1교시", StartTime: "08:40", EndTime: "09:25"},
				{Period: 2, Label: "2교시", StartTime: "09:35", EndTime: "10:20"},
				{Period: 3, Label: "3교시", StartTime: "10:30", EndTime: "11:15"},
				{Period: 4, Label: "4교시", StartTime: "11:25", EndTime: "12:10"},
				{Period: 5, Label: "5교시", StartTime: "13:00", EndTime: "13:45"},
				{Period: 6, Label: "6교시", StartTime: "13:55", EndTime: "14:40"},
				{Period: 7, Label: "7교시", StartTime: "14:50", EndTime: "15:35"},
			},
		},
		{
			Key:         "MORNING_EVENT",
			Name:        "오전 행사(1~4교시) 후 5교시 수업",
			Description: "오전 1~4교시 전교 행사 진행 후 점심시간을 거쳐 13:10부터 5~7교시 운영",
			Periods: []protocol.BellPeriod{
				{Period: 1, Label: "1교시(오전행사)", StartTime: "08:40", EndTime: "09:20"},
				{Period: 2, Label: "2교시(오전행사)", StartTime: "09:30", EndTime: "10:10"},
				{Period: 3, Label: "3교시(오전행사)", StartTime: "10:20", EndTime: "11:00"},
				{Period: 4, Label: "4교시(오전행사)", StartTime: "11:10", EndTime: "12:10"},
				{Period: 5, Label: "5교시", StartTime: "13:10", EndTime: "14:00"},
				{Period: 6, Label: "6교시", StartTime: "14:10", EndTime: "15:00"},
				{Period: 7, Label: "7교시", StartTime: "15:10", EndTime: "16:00"},
			},
		},
	}
}

// BuildBellOverrideFromPreset은 프리셋 키(SHORT_40, SHORT_45, MORNING_EVENT)에 맞는 교시별 시각 슬라이스를 반환합니다.
func BuildBellOverrideFromPreset(preset string) []protocol.BellPeriod {
	key := strings.ToUpper(strings.TrimSpace(preset))
	for _, def := range DefaultBellPresets() {
		if def.Key == key {
			copied := make([]protocol.BellPeriod, len(def.Periods))
			copy(copied, def.Periods)
			return copied
		}
	}
	return nil
}

// DetectConflicts는 신규 또는 수정하려는 행사와 기존 행사 간 장소·동일 학년/학급 교시 충돌을 탐지합니다.
func DetectConflicts(ev protocol.SchoolEvent, existing []protocol.SchoolEvent) []protocol.EventConflict {
	if strings.TrimSpace(ev.EventDate) == "" {
		return nil
	}
	candDates := protocol.EventDates(ev)
	if len(candDates) == 0 {
		return nil
	}
	candDateSet := make(map[string]bool, len(candDates))
	for _, d := range candDates {
		candDateSet[d] = true
	}

	var conflicts []protocol.EventConflict
	locTrim := strings.TrimSpace(ev.Location)

	for _, other := range existing {
		if other.Status == protocol.StatusRejected || (ev.ID != "" && other.ID == ev.ID) {
			continue
		}
		if !periodsOverlap(ev, other) {
			continue
		}
		var overlapDates []string
		for _, od := range protocol.EventDates(other) {
			if candDateSet[od] {
				overlapDates = append(overlapDates, od)
			}
		}
		if len(overlapDates) == 0 {
			continue
		}
		firstDate := overlapDates[0]

		otherLoc := strings.TrimSpace(other.Location)
		if locTrim != "" && otherLoc != "" && strings.EqualFold(locTrim, otherLoc) {
			conflicts = append(conflicts, protocol.EventConflict{
				Type:       "LOCATION",
				Date:       firstDate,
				EventID:    other.ID,
				EventTitle: other.Title,
				Message:    fmt.Sprintf("%s %s 장소('%s')가 기존 행사 '%s'와 중복됩니다.", firstDate, formatPeriodSpan(other), locTrim, other.Title),
			})
		}

		isBlockPair := ev.LinkType == protocol.EventLinkClassBlock ||
			ev.LinkType == protocol.EventLinkHoliday ||
			other.LinkType == protocol.EventLinkClassBlock ||
			other.LinkType == protocol.EventLinkHoliday
		bothTargeted := (len(ev.TargetGrades) > 0 || len(ev.TargetClasses) > 0) &&
			(len(other.TargetGrades) > 0 || len(other.TargetClasses) > 0)
		if (isBlockPair || bothTargeted) && targetsOverlap(ev, other) {
			conflicts = append(conflicts, protocol.EventConflict{
				Type:       "GRADE_PERIOD",
				Date:       firstDate,
				EventID:    other.ID,
				EventTitle: other.Title,
				Message:    fmt.Sprintf("%s %s 대상 학년·학급이 기존 행사 '%s'와 교시 충돌합니다.", firstDate, formatPeriodSpan(other), other.Title),
			})
		}
	}
	return conflicts
}

func periodsOverlap(a, b protocol.SchoolEvent) bool {
	if a.StartPeriod == 0 || b.StartPeriod == 0 {
		return true
	}
	aEnd := a.EndPeriod
	if aEnd == 0 {
		aEnd = a.StartPeriod
	}
	bEnd := b.EndPeriod
	if bEnd == 0 {
		bEnd = b.StartPeriod
	}
	return a.StartPeriod <= bEnd && b.StartPeriod <= aEnd
}

func targetsOverlap(a, b protocol.SchoolEvent) bool {
	aEmpty := len(a.TargetGrades) == 0 && len(a.TargetClasses) == 0
	bEmpty := len(b.TargetGrades) == 0 && len(b.TargetClasses) == 0
	if aEmpty || bEmpty {
		return true
	}
	if len(a.TargetClasses) > 0 && len(b.TargetClasses) > 0 {
		for _, ca := range a.TargetClasses {
			if slices.Contains(b.TargetClasses, ca) {
				return true
			}
		}
		if len(a.TargetGrades) == 0 && len(b.TargetGrades) == 0 {
			return false
		}
	}
	gradesA := collectEffectiveGrades(a)
	gradesB := collectEffectiveGrades(b)
	for _, ga := range gradesA {
		if slices.Contains(gradesB, ga) {
			return true
		}
	}
	return false
}

func collectEffectiveGrades(ev protocol.SchoolEvent) []int {
	var grades []int
	for _, g := range ev.TargetGrades {
		if g > 0 && !slices.Contains(grades, g) {
			grades = append(grades, g)
		}
	}
	for _, cn := range ev.TargetClasses {
		if parts := strings.SplitN(strings.TrimSpace(cn), "-", 2); len(parts) >= 1 {
			if g, err := strconv.Atoi(parts[0]); err == nil && g > 0 && !slices.Contains(grades, g) {
				grades = append(grades, g)
			}
		}
	}
	return grades
}

func formatPeriodSpan(ev protocol.SchoolEvent) string {
	if ev.StartPeriod == 0 {
		return "종일"
	}
	end := ev.EndPeriod
	if end <= ev.StartPeriod {
		return fmt.Sprintf("%d교시", ev.StartPeriod)
	}
	return fmt.Sprintf("%d~%d교시", ev.StartPeriod, end)
}

// expandRecurrencePairs는 반복 주기(WEEKLY/MONTHLY)와 종료일에 맞춰 최대 12회의 (시작일, 종료일) 쌍을 생성합니다.
func expandRecurrencePairs(ev protocol.SchoolEvent) [][2]string {
	rec := strings.ToUpper(strings.TrimSpace(ev.Recurrence))
	if rec != "WEEKLY" && rec != "MONTHLY" {
		return nil
	}
	recEndStr := strings.TrimSpace(ev.RecurrenceEnd)
	if recEndStr == "" {
		return nil
	}
	st, err1 := time.Parse("2006-01-02", strings.TrimSpace(ev.EventDate))
	endT, err2 := time.Parse("2006-01-02", protocol.EventEndDate(ev))
	recEnd, err3 := time.Parse("2006-01-02", recEndStr)
	if err1 != nil || err2 != nil || err3 != nil || recEnd.Before(st) {
		return nil
	}
	spanDays := int(endT.Sub(st).Hours() / 24)
	if spanDays < 0 {
		spanDays = 0
	}
	var pairs [][2]string
	for i := 0; i < 12; i++ {
		var cur time.Time
		if rec == "WEEKLY" {
			cur = st.AddDate(0, 0, 7*i)
		} else {
			cur = st.AddDate(0, i, 0)
		}
		if cur.After(recEnd) {
			break
		}
		curEnd := cur.AddDate(0, 0, spanDays)
		pairs = append(pairs, [2]string{cur.Format("2006-01-02"), curEnd.Format("2006-01-02")})
	}
	return pairs
}
