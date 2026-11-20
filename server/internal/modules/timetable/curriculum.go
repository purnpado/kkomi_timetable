package timetable

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// AutoGenerateFromCurriculum은 등록된 교사별 시수표·요일별 기준시수·교과교실 제약을 모두 반영해
// 충돌 없는 정규 시간표를 자동 편성합니다.
func (s *Service) AutoGenerateFromCurriculum(clearExisting bool) ([]protocol.TimetableCell, error) {
	res, err := s.AutoGenerateFromCurriculumDetailed(clearExisting)
	return res.Cells, err
}

// AutoGenerateFromCurriculumDetailed는 자동 편성 결과와 함께 미배정 건수·상세 경고를 반환합니다.
func (s *Service) AutoGenerateFromCurriculumDetailed(clearExisting bool) (AutoGenerateResult, error) {
	cfg, _ := s.GetSchoolConfig()
	loads, err := s.ListTeachingLoads()
	if err != nil {
		return AutoGenerateResult{}, err
	}
	if len(loads) == 0 {
		return AutoGenerateResult{}, errors.New("등록된 교사별 시수표가 없습니다. 시수표 등록부에서 먼저 교과별 시수를 입력해 주세요")
	}

	teachersList, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return AutoGenerateResult{}, err
	}
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}

	existing, _ := s.ListCells()
	st := newSchedState(cfg, tMap)
	var deleteIDs []string
	keptHours := make(map[string]int)
	for _, c := range existing {
		if clearExisting && !c.IsLocked {
			deleteIDs = append(deleteIDs, c.ID)
			continue
		}
		st.mark(c, true)
		keptHours[fmt.Sprintf("%s|%s|%s", c.ClassName, c.Subject, c.TeacherID)]++
	}

	var units []allocUnit
	for _, ld := range loads {
		if t, ok := tMap[ld.TeacherID]; ok && (protocol.IsSystemAdminAccount(t) || t.IsParentHonorary) {
			continue
		}
		clsKeys := make([]string, 0, len(ld.ClassHours))
		for cls := range ld.ClassHours {
			clsKeys = append(clsKeys, cls)
		}
		sort.Strings(clsKeys)
		for _, cls := range clsKeys {
			k := fmt.Sprintf("%s|%s|%s", cls, ld.Subject, ld.TeacherID)
			rem := ld.ClassHours[cls] - keptHours[k]
			if rem <= 0 {
				keptHours[k] = -rem
				continue
			}
			keptHours[k] = 0
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
		if units[i].ClassName != units[j].ClassName {
			return units[i].ClassName < units[j].ClassName
		}
		return units[i].Load.ID < units[j].Load.ID
	})

	var created []protocol.TimetableCell
	placeUnitInMem := func(u allocUnit, day, startP int) {
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
			st.mark(cell, true)
			created = append(created, cell)
		}
	}

	// 1. 동시수업 그룹: 멤버별 회차(1시간차, 2시간차...) 단위로 묶어 동일 교시 동시 배치
	simQueues := make(map[string]map[string][]allocUnit)
	var simOrder []string
	var normalUnits []allocUnit
	for _, u := range units {
		if g := strings.TrimSpace(u.Load.SimultaneousGroup); g != "" {
			if _, ok := simQueues[g]; !ok {
				simOrder = append(simOrder, g)
				simQueues[g] = make(map[string][]allocUnit)
			}
			mk := u.Load.ID + "|" + u.ClassName
			simQueues[g][mk] = append(simQueues[g][mk], u)
		} else {
			normalUnits = append(normalUnits, u)
		}
	}

	var warnings []string
	var unplaced []allocUnit
	simDayOffset := 0
	for _, gKey := range simOrder {
		memberMap := simQueues[gKey]
		memberKeys := make([]string, 0, len(memberMap))
		maxRounds := 0
		for mk, q := range memberMap {
			memberKeys = append(memberKeys, mk)
			if len(q) > maxRounds {
				maxRounds = len(q)
			}
		}
		sort.Strings(memberKeys)

		for round := 0; round < maxRounds; round++ {
			var roundUnits []allocUnit
			for _, mk := range memberKeys {
				if round < len(memberMap[mk]) {
					roundUnits = append(roundUnits, memberMap[mk][round])
				}
			}
			if hasIntraGroupConflict(roundUnits, tMap) {
				warnings = append(warnings, fmt.Sprintf("동시수업 그룹 '%s' %d회차 내부 교사/학급/교실 중복으로 일반 배치 전환", gKey, round+1))
				normalUnits = append(normalUnits, roundUnits...)
				continue
			}
			placedGrp := false
			for _, allowDup := range []bool{false, true} {
				if placedGrp {
					break
				}
				for step := 0; step < 5 && !placedGrp; step++ {
					day := ((simDayOffset + step) % 5) + 1
					for p := 1; p <= 7 && !placedGrp; p++ {
						allOK := true
						for _, u := range roundUnits {
							if !st.canPlace(u, day, p, allowDup) {
								allOK = false
								break
							}
						}
						if allOK {
							for _, u := range roundUnits {
								placeUnitInMem(u, day, p)
							}
							placedGrp = true
							simDayOffset = (simDayOffset + 1) % 5
						}
					}
				}
			}
			if !placedGrp {
				for _, u := range roundUnits {
					for k := 0; k < u.Hours; k++ {
						unplaced = append(unplaced, allocUnit{Load: u.Load, ClassName: u.ClassName, Hours: 1})
					}
				}
			}
		}
	}

	// 2. 일반·연속·전용실 단위 배치
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
					placeUnitInMem(u, day, p)
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

	// 3. 미배정 시수 1단계 스왑 복구 패스
	var finalUnplaced []allocUnit
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
						targetD, targetP := other.DayOfWeek, other.Period
						other.DayOfWeek = ed
						other.Period = ep
						other.ID = fmt.Sprintf("c-%d-%d-%s", ed, ep, other.ClassName)
						created[idx] = other
						st.mark(other, true)
						placeUnitInMem(u, targetD, targetP)
						repaired = true
						break
					}
					st.mark(other, true)
				}
			}
		}
		if !repaired {
			finalUnplaced = append(finalUnplaced, u)
			warnings = append(warnings, fmt.Sprintf("[미배정] %s반 %s(%s) 1시간", u.ClassName, u.Subject(), tMap[u.Load.TeacherID].Name))
		}
	}

	if err := s.store.WithTx(func(tx *database.TxStore) error {
		for _, id := range deleteIDs {
			if dErr := tx.DeleteRecord("timetable_cells", id); dErr != nil {
				return dErr
			}
		}
		for _, c := range created {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return AutoGenerateResult{}, err
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventTimetableCellsUpdated,
			Title:   "시수표 기반 정규 시간표 자동 편성 완료",
			Message: fmt.Sprintf("총 %d교시 배정 (미배정 %d건)", len(created), len(finalUnplaced)),
			Payload: map[string]int{"createdCount": len(created), "unplacedCount": len(finalUnplaced)},
		})
	}
	return AutoGenerateResult{
		Cells:         created,
		UnplacedCount: len(finalUnplaced),
		Warnings:      warnings,
	}, nil
}
