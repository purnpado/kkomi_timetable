package timetable

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// AutoGenerateResult는 시수표 기반 시간표 자동 편성 결과 및 미배정 경고 요약을 담습니다.
type AutoGenerateResult struct {
	Cells         []protocol.TimetableCell `json:"cells"`
	UnplacedCount int                      `json:"unplacedCount"`
	Warnings      []string                 `json:"warnings"`
}

type allocUnit struct {
	Load      protocol.TeachingLoad
	ClassName string
	Hours     int
}

func (u allocUnit) Subject() string {
	return u.Load.Subject
}

type schedState struct {
	cfg         protocol.SchoolConfig
	tMap        map[string]protocol.Teacher
	constraints []protocol.TeacherConstraint
	slotBusy    map[string]bool
	teacherBusy map[string]bool
	roomBusy    map[string]bool
	dayHasSubj  map[string]int
	streakMap   map[string]map[int]bool
}

func newSchedState(cfg protocol.SchoolConfig, tMap map[string]protocol.Teacher, constraints []protocol.TeacherConstraint) *schedState {
	return &schedState{
		cfg:         cfg,
		tMap:        tMap,
		constraints: constraints,
		slotBusy:    make(map[string]bool),
		teacherBusy: make(map[string]bool),
		roomBusy:    make(map[string]bool),
		dayHasSubj:  make(map[string]int),
		streakMap:   make(map[string]map[int]bool),
	}
}

func (st *schedState) mark(c protocol.TimetableCell, add bool) {
	st.slotBusy[fmt.Sprintf("%s-%d-%d", c.ClassName, c.DayOfWeek, c.Period)] = add
	st.teacherBusy[fmt.Sprintf("%s-%d-%d", c.TeacherID, c.DayOfWeek, c.Period)] = add
	if c.CoTeacherID != "" {
		st.teacherBusy[fmt.Sprintf("%s-%d-%d", c.CoTeacherID, c.DayOfWeek, c.Period)] = add
		if st.streakMap[c.CoTeacherID] == nil {
			st.streakMap[c.CoTeacherID] = make(map[int]bool)
		}
		st.streakMap[c.CoTeacherID][c.DayOfWeek*10+c.Period] = add
	}
	if c.Room != "" && c.Room != c.ClassName+"교실" {
		st.roomBusy[fmt.Sprintf("%s-%d-%d", c.Room, c.DayOfWeek, c.Period)] = add
	}
	dk := fmt.Sprintf("%s-%d-%s", c.ClassName, c.DayOfWeek, c.Subject)
	if add {
		st.dayHasSubj[dk]++
	} else if st.dayHasSubj[dk] > 0 {
		st.dayHasSubj[dk]--
	}
	if st.streakMap[c.TeacherID] == nil {
		st.streakMap[c.TeacherID] = make(map[int]bool)
	}
	st.streakMap[c.TeacherID][c.DayOfWeek*10+c.Period] = add
}

func (st *schedState) maxPeriod(grade, day int) int {
	if mp := st.cfg.DailyPeriodLimits[grade][day]; mp > 0 {
		return mp
	}
	return 6
}

func (st *schedState) canPlace(u allocUnit, day, startP int, allowSameDayDup, allowSoftAvoid bool) bool {
	teacher := st.tMap[u.Load.TeacherID]
	if protocol.IsSystemAdminAccount(teacher) || teacher.IsParentHonorary {
		return false
	}
	if len(teacher.ItinerantDays) > 0 && !slices.Contains(teacher.ItinerantDays, day) {
		return false
	}
	if u.Load.CoTeacherID != "" {
		coT := st.tMap[u.Load.CoTeacherID]
		if len(coT.ItinerantDays) > 0 && !slices.Contains(coT.ItinerantDays, day) {
			return false
		}
	}
	grade := parseGrade(u.ClassName, u.Load.Grade)
	if startP+u.Hours-1 > st.maxPeriod(grade, day) {
		return false
	}
	roomName := effectiveRoom(u.Load, teacher, u.ClassName)
	if !allowSameDayDup && st.dayHasSubj[fmt.Sprintf("%s-%d-%s", u.ClassName, day, u.Load.Subject)] > 0 {
		return false
	}
	for offset := 0; offset < u.Hours; offset++ {
		p := startP + offset
		if hardConstraintReason(u.Load.TeacherID, day, p, st.constraints, st.tMap) != "" {
			return false
		}
		if u.Load.CoTeacherID != "" && hardConstraintReason(u.Load.CoTeacherID, day, p, st.constraints, st.tMap) != "" {
			return false
		}
		if !allowSoftAvoid {
			if softConstraintReason(u.Load.TeacherID, day, p, st.constraints, st.tMap) != "" {
				return false
			}
			if u.Load.CoTeacherID != "" && softConstraintReason(u.Load.CoTeacherID, day, p, st.constraints, st.tMap) != "" {
				return false
			}
		}
		if st.slotBusy[fmt.Sprintf("%s-%d-%d", u.ClassName, day, p)] {
			return false
		}
		if st.teacherBusy[fmt.Sprintf("%s-%d-%d", u.Load.TeacherID, day, p)] {
			return false
		}
		if u.Load.CoTeacherID != "" && st.teacherBusy[fmt.Sprintf("%s-%d-%d", u.Load.CoTeacherID, day, p)] {
			return false
		}
		if roomName != u.ClassName+"교실" && st.roomBusy[fmt.Sprintf("%s-%d-%d", roomName, day, p)] {
			return false
		}
	}
	// 연속 블록 전체를 동시 점유한다고 가정해 4연강 이상 초과를 차단
	if wouldBlockExceedThree(st.streakMap[u.Load.TeacherID], day, startP, u.Hours) {
		return false
	}
	if u.Load.CoTeacherID != "" && wouldBlockExceedThree(st.streakMap[u.Load.CoTeacherID], day, startP, u.Hours) {
		return false
	}
	return true
}

func hasIntraGroupConflict(units []allocUnit, tMap map[string]protocol.Teacher) bool {
	seenClass := make(map[string]bool)
	seenTeacher := make(map[string]bool)
	seenRoom := make(map[string]bool)
	for _, u := range units {
		if seenClass[u.ClassName] {
			return true
		}
		seenClass[u.ClassName] = true
		if seenTeacher[u.Load.TeacherID] {
			return true
		}
		seenTeacher[u.Load.TeacherID] = true
		if u.Load.CoTeacherID != "" {
			if seenTeacher[u.Load.CoTeacherID] {
				return true
			}
			seenTeacher[u.Load.CoTeacherID] = true
		}
		rm := effectiveRoom(u.Load, tMap[u.Load.TeacherID], u.ClassName)
		if rm != u.ClassName+"교실" {
			if seenRoom[rm] {
				return true
			}
			seenRoom[rm] = true
		}
	}
	return false
}

func effectiveRoom(ld protocol.TeachingLoad, teacher protocol.Teacher, className string) string {
	if strings.TrimSpace(ld.DedicatedRoom) != "" {
		return strings.TrimSpace(ld.DedicatedRoom)
	}
	if strings.TrimSpace(teacher.DefaultRoom) != "" {
		return strings.TrimSpace(teacher.DefaultRoom)
	}
	return className + "교실"
}

func parseGrade(className string, fallback int) int {
	if g, err := strconv.Atoi(strings.Split(className, "-")[0]); err == nil && g > 0 {
		return g
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func (st *schedState) softPlacementWarnings(u allocUnit, day, startP int) []string {
	var warns []string
	dayLabel := dayNames[day]
	for offset := 0; offset < u.Hours; offset++ {
		p := startP + offset
		if r := softConstraintReason(u.Load.TeacherID, day, p, st.constraints, st.tMap); r != "" {
			warns = append(warns, fmt.Sprintf("[기피교시 배정] %s반 %s %d교시 %s (%s)", u.ClassName, dayLabel, p, u.Subject(), r))
		}
		if u.Load.CoTeacherID != "" {
			if r := softConstraintReason(u.Load.CoTeacherID, day, p, st.constraints, st.tMap); r != "" {
				warns = append(warns, fmt.Sprintf("[기피교시 배정] %s반 %s %d교시 %s 코티칭 (%s)", u.ClassName, dayLabel, p, u.Subject(), r))
			}
		}
	}
	return warns
}
