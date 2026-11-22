package timetable

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// AutoGenerateResult는 시수표 기반 시간표 자동 편성 결과 및 미배정 경고·원인 진단 요약을 담습니다.
type AutoGenerateResult struct {
	Cells                 []protocol.TimetableCell      `json:"cells"`
	UnplacedCount         int                           `json:"unplacedCount"`
	EjectionRepairedCount int                           `json:"ejectionRepairedCount"`
	Warnings              []string                      `json:"warnings"`
	Diagnostics           []protocol.UnplacedDiagnosis    `json:"diagnostics,omitempty"`
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
	cfg           protocol.SchoolConfig
	tMap          map[string]protocol.Teacher
	constraints   []protocol.TeacherConstraint
	slotBusy      map[string]bool
	teacherBusy   map[string]bool
	roomBusyCount map[string]int
	dayHasSubj    map[string]int
	streakMap     map[string]map[int]bool
}

func newSchedState(cfg protocol.SchoolConfig, tMap map[string]protocol.Teacher, constraints []protocol.TeacherConstraint) *schedState {
	return &schedState{
		cfg:           cfg,
		tMap:          tMap,
		constraints:   constraints,
		slotBusy:      make(map[string]bool),
		teacherBusy:   make(map[string]bool),
		roomBusyCount: make(map[string]int),
		dayHasSubj:    make(map[string]int),
		streakMap:     make(map[string]map[int]bool),
	}
}

func (st *schedState) mark(c protocol.TimetableCell, add bool) {
	st.slotBusy[fmt.Sprintf("%s-%d-%d", c.ClassName, c.DayOfWeek, c.Period)] = add
	for _, tid := range protocol.AllCellTeachers(c) {
		st.teacherBusy[fmt.Sprintf("%s-%d-%d", tid, c.DayOfWeek, c.Period)] = add
		if st.streakMap[tid] == nil {
			st.streakMap[tid] = make(map[int]bool)
		}
		st.streakMap[tid][c.DayOfWeek*10+c.Period] = add
	}
	if protocol.IsSubjectClassroomEnabled(st.cfg) && c.Room != "" && c.Room != c.ClassName+"교실" {
		rk := fmt.Sprintf("%s-%d-%d", c.Room, c.DayOfWeek, c.Period)
		if add {
			st.roomBusyCount[rk]++
		} else if st.roomBusyCount[rk] > 0 {
			st.roomBusyCount[rk]--
		}
	}
	dk := fmt.Sprintf("%s-%d-%s", c.ClassName, c.DayOfWeek, c.Subject)
	if add {
		st.dayHasSubj[dk]++
	} else if st.dayHasSubj[dk] > 0 {
		st.dayHasSubj[dk]--
	}
}

func (st *schedState) maxPeriod(grade, day int) int {
	if mp := st.cfg.DailyPeriodLimits[grade][day]; mp > 0 {
		return mp
	}
	return 6
}

func (st *schedState) canPlace(u allocUnit, day, startP int, allowSameDayDup, allowSoftAvoid bool) bool {
	teachers := protocol.AllLoadTeachers(u.Load)
	if len(teachers) == 0 {
		return false
	}
	for _, tid := range teachers {
		t := st.tMap[tid]
		if protocol.IsSystemAdminAccount(t) || t.IsParentHonorary {
			return false
		}
		if len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, day) {
			return false
		}
	}
	grade := parseGrade(u.ClassName, u.Load.Grade)
	if startP+u.Hours-1 > st.maxPeriod(grade, day) {
		return false
	}
	roomName := effectiveRoomWithConfig(u.Load, st.tMap[u.Load.TeacherID], u.ClassName, st.cfg)
	maxConc := protocol.RoomMaxConcurrent(st.cfg, roomName)
	roomEnabled := protocol.IsSubjectClassroomEnabled(st.cfg) && roomName != u.ClassName+"교실"
	if !allowSameDayDup && st.dayHasSubj[fmt.Sprintf("%s-%d-%s", u.ClassName, day, u.Load.Subject)] > 0 {
		return false
	}
	for offset := 0; offset < u.Hours; offset++ {
		p := startP + offset
		if st.slotBusy[fmt.Sprintf("%s-%d-%d", u.ClassName, day, p)] {
			return false
		}
		for _, tid := range teachers {
			if hardConstraintReason(tid, day, p, st.constraints, st.tMap) != "" {
				return false
			}
			if !allowSoftAvoid && softConstraintReason(tid, day, p, st.constraints, st.tMap) != "" {
				return false
			}
			if st.teacherBusy[fmt.Sprintf("%s-%d-%d", tid, day, p)] {
				return false
			}
		}
		if roomEnabled && st.roomBusyCount[fmt.Sprintf("%s-%d-%d", roomName, day, p)] >= maxConc {
			return false
		}
	}
	// 연속 블록 전체를 동시 점유한다고 가정해 4연강 이상 초과를 차단
	for _, tid := range teachers {
		if wouldBlockExceedThree(st.streakMap[tid], day, startP, u.Hours) {
			return false
		}
	}
	return true
}

func hasIntraGroupConflict(units []allocUnit, tMap map[string]protocol.Teacher, cfg protocol.SchoolConfig) bool {
	seenClass := make(map[string]bool)
	seenTeacher := make(map[string]bool)
	roomCounts := make(map[string]int)
	roomEnabled := protocol.IsSubjectClassroomEnabled(cfg)
	for _, u := range units {
		if seenClass[u.ClassName] {
			return true
		}
		seenClass[u.ClassName] = true
		for _, tid := range protocol.AllLoadTeachers(u.Load) {
			if seenTeacher[tid] {
				return true
			}
			seenTeacher[tid] = true
		}
		if roomEnabled {
			rm := effectiveRoomWithConfig(u.Load, tMap[u.Load.TeacherID], u.ClassName, cfg)
			if rm != u.ClassName+"교실" {
				roomCounts[rm]++
				if roomCounts[rm] > protocol.RoomMaxConcurrent(cfg, rm) {
					return true
				}
			}
		}
	}
	return false
}

func effectiveRoomWithConfig(ld protocol.TeachingLoad, teacher protocol.Teacher, className string, cfg protocol.SchoolConfig) string {
	if !protocol.IsSubjectClassroomEnabled(cfg) {
		return className + "교실"
	}
	return effectiveRoom(ld, teacher, className)
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
		for idx, tid := range protocol.AllLoadTeachers(u.Load) {
			if r := softConstraintReason(tid, day, p, st.constraints, st.tMap); r != "" {
				tag := ""
				if idx > 0 {
					tag = " 코티칭"
				}
				warns = append(warns, fmt.Sprintf("[기피교시 배정] %s반 %s %d교시 %s%s (%s)", u.ClassName, dayLabel, p, u.Subject(), tag, r))
			}
		}
	}
	return warns
}
