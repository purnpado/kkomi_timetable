package timetable

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

type allocUnit struct {
	Load      protocol.TeachingLoad
	ClassName string
	Hours     int
}

type schedState struct {
	cfg         protocol.SchoolConfig
	tMap        map[string]protocol.Teacher
	slotBusy    map[string]bool
	teacherBusy map[string]bool
	roomBusy    map[string]bool
	dayHasSubj  map[string]int
	streakMap   map[string]map[int]bool
}

func newSchedState(cfg protocol.SchoolConfig, tMap map[string]protocol.Teacher) *schedState {
	return &schedState{
		cfg:         cfg,
		tMap:        tMap,
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

func (st *schedState) canPlace(u allocUnit, day, startP int, allowSameDayDup bool) bool {
	teacher := st.tMap[u.Load.TeacherID]
	if len(teacher.ItinerantDays) > 0 && !slices.Contains(teacher.ItinerantDays, day) {
		return false
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
		if wouldExceedThree(st.streakMap[u.Load.TeacherID], day, p) {
			return false
		}
	}
	return true
}

// AutoGenerateFromCurriculum은 등록된 교사별 시수표·요일별 기준시수·교과교실 제약을 모두 반영하고,
// 미배정 발생 시 1단계 스왑 복구 패스까지 수행해 충돌 없는 정규 시간표를 자동 편성합니다.
func (s *Service) AutoGenerateFromCurriculum(clearExisting bool) ([]protocol.TimetableCell, error) {
	cfg, _ := s.GetSchoolConfig()
	loads, err := s.ListTeachingLoads()
	if err != nil {
		return nil, err
	}
	if len(loads) == 0 {
		return nil, errors.New("등록된 교사별 시수표가 없습니다. 시수표 등록부에서 먼저 교과별 시수를 입력해 주세요")
	}

	teachersList, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}

	existing, _ := s.ListCells()
	st := newSchedState(cfg, tMap)
	for _, c := range existing {
		if clearExisting && !c.IsLocked {
			_ = s.store.DeleteRecord("timetable_cells", c.ID)
			continue
		}
		st.mark(c, true)
	}

	var units []allocUnit
	for _, ld := range loads {
		for cls, total := range ld.ClassHours {
			rem := total
			if ld.BlockHours >= 2 {
				for rem >= ld.BlockHours {
					units = append(units, allocUnit{Load: ld, ClassName: cls, Hours: ld.BlockHours})
					rem -= ld.BlockHours
				}
			}
			for rem > 0 {
				units = append(units, allocUnit{Load: ld, ClassName: cls, Hours: 1})
				rem--
			}
		}
	}

	// 제약 강도 우선순위 정렬: 동시수업 -> 순회교사 -> 연속 블록 -> 교과교실 -> 일반 단시간
	sort.SliceStable(units, func(i, j int) bool {
		score := func(u allocUnit) int {
			p := 0
			if u.Load.SimultaneousGroup != "" {
				p += 100
			}
			if len(tMap[u.Load.TeacherID].ItinerantDays) > 0 {
				p += 60
			}
			if u.Hours >= 2 {
				p += 40
			}
			if u.Load.DedicatedRoom != "" {
				p += 20
			}
			return p
		}
		if score(units[i]) != score(units[j]) {
			return score(units[i]) > score(units[j])
		}
		return units[i].ClassName < units[j].ClassName
	})

	var created []protocol.TimetableCell
	placeUnitAt := func(u allocUnit, day, startP int) error {
		roomName := effectiveRoom(u.Load, tMap[u.Load.TeacherID], u.ClassName)
		blockID := ""
		if u.Hours >= 2 {
			blockID = fmt.Sprintf("blk-%s-%d-%d", u.ClassName, day, startP)
		}
		for offset := 0; offset < u.Hours; offset++ {
			curP := startP + offset
			cell := protocol.TimetableCell{
				ID:            fmt.Sprintf("c-%d-%d-%s", day, curP, u.ClassName),
				DayOfWeek:     day,
				Period:        curP,
				ClassName:     u.ClassName,
				Grade:         parseGrade(u.ClassName, u.Load.Grade),
				Subject:       u.Load.Subject,
				SubjectCode:   u.Load.SubjectCode,
				TeacherID:     u.Load.TeacherID,
				CoTeacherID:   u.Load.CoTeacherID,
				Room:          roomName,
				ElectiveGroup: u.Load.SimultaneousGroup,
				BlockGroupID:  blockID,
			}
			if err := s.store.SaveRecord("timetable_cells", cell.ID, cell); err != nil {
				return err
			}
			st.mark(cell, true)
			created = append(created, cell)
		}
		return nil
	}

	// 1. 동시수업 그룹 배치
	simGroups := make(map[string][]allocUnit)
	var simOrder []string
	var normalUnits []allocUnit
	for _, u := range units {
		if g := strings.TrimSpace(u.Load.SimultaneousGroup); g != "" {
			if _, ok := simGroups[g]; !ok {
				simOrder = append(simOrder, g)
			}
			simGroups[g] = append(simGroups[g], u)
		} else {
			normalUnits = append(normalUnits, u)
		}
	}
	for _, gKey := range simOrder {
		grp := simGroups[gKey]
		placedGrp := false
		for day := 1; day <= 5 && !placedGrp; day++ {
			for p := 1; p <= 6 && !placedGrp; p++ {
				allOK := true
				for _, u := range grp {
					if !st.canPlace(u, day, p, true) {
						allOK = false
						break
					}
				}
				if allOK {
					for _, u := range grp {
						if err := placeUnitAt(u, day, p); err != nil {
							return created, err
						}
					}
					placedGrp = true
				}
			}
		}
	}

	// 2. 일반·연속·전용실 단위 배치
	var unplaced []allocUnit
	dayOffset := 0
	for _, u := range normalUnits {
		placed := false
		for _, allowDup := range []bool{false, true} {
			if placed {
				break
			}
			for step := 0; step < 5 && !placed; step++ {
				day := ((dayOffset + step) % 5) + 1
				maxP := st.maxPeriod(parseGrade(u.ClassName, u.Load.Grade), day)
				for p := 1; p <= maxP; p++ {
					if !st.canPlace(u, day, p, allowDup) {
						continue
					}
					if err := placeUnitAt(u, day, p); err != nil {
						return created, err
					}
					placed = true
					dayOffset = (dayOffset + 1) % 5
					break
				}
			}
		}
		if !placed {
			for k := 0; k < u.Hours; k++ {
				unplaced = append(unplaced, allocUnit{Load: u.Load, ClassName: u.ClassName, Hours: 1})
			}
		}
	}

	// 3. 미배정 시수 1단계 스왑 복구 패스 (빈 교시 확보를 위한 동일 학급 내 재배치)
	for _, u := range unplaced {
		grade := parseGrade(u.ClassName, u.Load.Grade)
		repaired := false
		for ed := 1; ed <= 5 && !repaired; ed++ {
			for ep := 1; ep <= st.maxPeriod(grade, ed) && !repaired; ep++ {
				if st.slotBusy[fmt.Sprintf("%s-%d-%d", u.ClassName, ed, ep)] {
					continue
				}
				for idx := range created {
					other := created[idx]
					if other.ClassName != u.ClassName || other.IsLocked || other.BlockGroupID != "" || other.ElectiveGroup != "" {
						continue
					}
					st.mark(other, false)
					otherUnit := allocUnit{
						Load: protocol.TeachingLoad{
							TeacherID: other.TeacherID, CoTeacherID: other.CoTeacherID,
							Grade: other.Grade, Subject: other.Subject, DedicatedRoom: other.Room,
						},
						ClassName: other.ClassName, Hours: 1,
					}
					if st.canPlace(otherUnit, ed, ep, true) && st.canPlace(u, other.DayOfWeek, other.Period, true) {
						oldID := other.ID
						_ = s.store.DeleteRecord("timetable_cells", oldID)
						targetD, targetP := other.DayOfWeek, other.Period
						other.DayOfWeek = ed
						other.Period = ep
						other.ID = fmt.Sprintf("c-%d-%d-%s", ed, ep, other.ClassName)
						_ = s.store.SaveRecord("timetable_cells", other.ID, other)
						created[idx] = other
						st.mark(other, true)
						if err := placeUnitAt(u, targetD, targetP); err != nil {
							return created, err
						}
						repaired = true
						break
					}
					st.mark(other, true)
				}
			}
		}
	}

	return created, nil
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
