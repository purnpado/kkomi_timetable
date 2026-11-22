package timetable

import (
	"fmt"
	"slices"

	"kkomi-timetable/pkg/protocol"
)

// repairUnplacedWithEjectionChain은 FET 알고리즘 방식의 재귀적 축출·재배치(Ejection Chain)로
// 단일 스왑으로 해결되지 않는 미배정 시수를 최대 maxDepth 단계까지 연쇄 이동해 복구합니다.
func repairUnplacedWithEjectionChain(
	u allocUnit,
	st *schedState,
	created []protocol.TimetableCell,
	cfg protocol.SchoolConfig,
	tMap map[string]protocol.Teacher,
	maxDepth int,
	maxNodes int,
) ([]protocol.TimetableCell, []string, bool) {
	if u.Hours <= 0 {
		u.Hours = 1
	}
	work := make([]protocol.TimetableCell, len(created))
	copy(work, created)
	budget := maxNodes
	visited := make(map[string]bool)

	ok, newCells, warns := ejectAndPlaceRecursive(u, st, work, cfg, tMap, visited, 1, maxDepth, &budget)
	if !ok {
		return created, nil, false
	}
	return newCells, warns, true
}

func ejectAndPlaceRecursive(
	u allocUnit,
	st *schedState,
	work []protocol.TimetableCell,
	cfg protocol.SchoolConfig,
	tMap map[string]protocol.Teacher,
	visited map[string]bool,
	depth int,
	maxDepth int,
	budget *int,
) (bool, []protocol.TimetableCell, []string) {
	if *budget <= 0 || depth > maxDepth {
		return false, nil, nil
	}
	*budget--

	// 기저: 빈 교시에 직접 배정 가능하면 즉시 배치 완료
	for _, allowSoft := range []bool{false, true} {
		for d := 1; d <= 5; d++ {
			maxP := st.maxPeriod(parseGrade(u.ClassName, u.Load.Grade), d)
			for p := 1; p <= maxP; p++ {
				if st.canPlace(u, d, p, true, allowSoft) {
					placed := buildUnitCell(u, d, p, tMap, cfg)
					st.mark(placed, true)
					nextWork := append(work, placed)
					var w []string
					if allowSoft {
						w = st.softPlacementWarnings(u, d, p)
					}
					return true, nextWork, w
				}
			}
		}
	}

	if depth >= maxDepth {
		return false, nil, nil
	}

	grade := parseGrade(u.ClassName, u.Load.Grade)
	uTeachers := protocol.AllLoadTeachers(u.Load)
	uRoom := effectiveRoomWithConfig(u.Load, st.tMap[u.Load.TeacherID], u.ClassName, cfg)
	roomEnabled := protocol.IsSubjectClassroomEnabled(cfg) && uRoom != u.ClassName+"교실"
	maxConc := protocol.RoomMaxConcurrent(cfg, uRoom)

	// (d, p)를 방해하는 이동 가능한 단일 셀을 축출하고, u를 배치한 뒤 축출된 셀을 재귀 재배치
	for d := 1; d <= 5; d++ {
		if hasTeacherHardBlockForDay(uTeachers, d, st.tMap) {
			continue
		}
		maxP := st.maxPeriod(grade, d)
		for p := 1; p <= maxP; p++ {
			if hasTeacherHardBlockForSlot(uTeachers, d, p, st.constraints, st.tMap) {
				continue
			}

			blockerIndices := findSlotBlockerIndices(work, u.ClassName, uTeachers, uRoom, roomEnabled, maxConc, d, p)
			if len(blockerIndices) != 1 {
				continue
			}
			bIdx := blockerIndices[0]
			blocker := work[bIdx]
			if blocker.IsLocked || blocker.BlockGroupID != "" || blocker.ElectiveGroup != "" || visited[blocker.ID] {
				continue
			}

			// blocker 임시 축출 후 u 배치 시도
			st.mark(blocker, false)
			if !st.canPlace(u, d, p, true, true) {
				st.mark(blocker, true)
				continue
			}

			placedU := buildUnitCell(u, d, p, tMap, cfg)
			st.mark(placedU, true)
			visited[blocker.ID] = true

			// blocker를 work에서 제외한 슬라이스로 재귀 호출
			reduced := make([]protocol.TimetableCell, 0, len(work))
			reduced = append(reduced, work[:bIdx]...)
			reduced = append(reduced, work[bIdx+1:]...)
			reduced = append(reduced, placedU)

			bUnit := allocUnit{
				Load: protocol.TeachingLoad{
					TeacherID: blocker.TeacherID, CoTeacherID: blocker.CoTeacherID, CoTeacherIDs: blocker.CoTeacherIDs,
					Grade: blocker.Grade, Subject: blocker.Subject, ShortSubject: blocker.Subject,
					SubjectCode: blocker.SubjectCode, DedicatedRoom: blocker.Room, BlockHours: 1,
				},
				ClassName: blocker.ClassName,
				Hours:     1,
			}

			ok, finalWork, childWarns := ejectAndPlaceRecursive(bUnit, st, reduced, cfg, tMap, visited, depth+1, maxDepth, budget)
			delete(visited, blocker.ID)
			if ok {
				warns := append(st.softPlacementWarnings(u, d, p), childWarns...)
				return true, finalWork, warns
			}

			// 실패 시 상태 완전 롤백
			st.mark(placedU, false)
			st.mark(blocker, true)
		}
	}
	return false, nil, nil
}

func buildUnitCell(u allocUnit, day, period int, tMap map[string]protocol.Teacher, cfg protocol.SchoolConfig) protocol.TimetableCell {
	roomName := effectiveRoomWithConfig(u.Load, tMap[u.Load.TeacherID], u.ClassName, cfg)
	return protocol.TimetableCell{
		ID:            fmt.Sprintf("c-%d-%d-%s", day, period, u.ClassName),
		DayOfWeek:     day,
		Period:        period,
		ClassName:     u.ClassName,
		Grade:         parseGrade(u.ClassName, u.Load.Grade),
		Subject:       u.Load.Subject,
		SubjectCode:   u.Load.SubjectCode,
		TeacherID:     u.Load.TeacherID,
		CoTeacherID:   u.Load.CoTeacherID,
		CoTeacherIDs:  append([]string(nil), u.Load.CoTeacherIDs...),
		Room:          roomName,
		ElectiveGroup: u.Load.SimultaneousGroup,
	}
}

func hasTeacherHardBlockForDay(teachers []string, day int, tMap map[string]protocol.Teacher) bool {
	for _, tid := range teachers {
		t := tMap[tid]
		if len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, day) {
			return true
		}
	}
	return false
}

func hasTeacherHardBlockForSlot(teachers []string, day, period int, constraints []protocol.TeacherConstraint, tMap map[string]protocol.Teacher) bool {
	for _, tid := range teachers {
		if hardConstraintReason(tid, day, period, constraints, tMap) != "" {
			return true
		}
	}
	return false
}

func findSlotBlockerIndices(
	work []protocol.TimetableCell,
	className string,
	uTeachers []string,
	uRoom string,
	roomEnabled bool,
	maxConc int,
	day, period int,
) []int {
	var indices []int
	seen := make(map[int]bool)
	roomMatches := make([]int, 0, 4)

	for idx, c := range work {
		if c.DayOfWeek != day || c.Period != period {
			continue
		}
		if c.ClassName == className {
			if !seen[idx] {
				seen[idx] = true
				indices = append(indices, idx)
			}
		}
		cTeachers := protocol.AllCellTeachers(c)
		for _, ut := range uTeachers {
			if slices.Contains(cTeachers, ut) && !seen[idx] {
				seen[idx] = true
				indices = append(indices, idx)
			}
		}
		if roomEnabled && c.Room == uRoom {
			roomMatches = append(roomMatches, idx)
		}
	}
	if roomEnabled && len(roomMatches) >= maxConc {
		for _, rIdx := range roomMatches {
			if !seen[rIdx] {
				seen[rIdx] = true
				indices = append(indices, rIdx)
			}
		}
	}
	return indices
}

// diagnoseUnplacedUnit은 자동 편성에 실패한 시수의 모든 가용 교시를 분석해 근본 원인 및 해결 권고안을 도출합니다.
func diagnoseUnplacedUnit(u allocUnit, st *schedState, tMap map[string]protocol.Teacher) protocol.UnplacedDiagnosis {
	grade := parseGrade(u.ClassName, u.Load.Grade)
	teachers := protocol.AllLoadTeachers(u.Load)
	roomName := effectiveRoomWithConfig(u.Load, tMap[u.Load.TeacherID], u.ClassName, st.cfg)
	maxConc := protocol.RoomMaxConcurrent(st.cfg, roomName)
	roomEnabled := protocol.IsSubjectClassroomEnabled(st.cfg) && roomName != u.ClassName+"교실"

	totalSlots, freeClassSlots := 0, 0
	itinerantFail, hardFail, teacherBusy, roomBusy, streakFail := 0, 0, 0, 0, 0

	for d := 1; d <= 5; d++ {
		maxP := st.maxPeriod(grade, d)
		for p := 1; p <= maxP; p++ {
			totalSlots++
			if st.slotBusy[fmt.Sprintf("%s-%d-%d", u.ClassName, d, p)] {
				continue
			}
			freeClassSlots++
			if hasTeacherHardBlockForDay(teachers, d, st.tMap) {
				itinerantFail++
				continue
			}
			if hasTeacherHardBlockForSlot(teachers, d, p, st.constraints, st.tMap) {
				hardFail++
				continue
			}
			tBusy := false
			for _, tid := range teachers {
				if st.teacherBusy[fmt.Sprintf("%s-%d-%d", tid, d, p)] {
					tBusy = true
					break
				}
			}
			if tBusy {
				teacherBusy++
				continue
			}
			if roomEnabled && st.roomBusyCount[fmt.Sprintf("%s-%d-%d", roomName, d, p)] >= maxConc {
				roomBusy++
				continue
			}
			for _, tid := range teachers {
				if wouldBlockExceedThree(st.streakMap[tid], d, p, 1) {
					streakFail++
					break
				}
			}
		}
	}

	var details, suggestions []string
	primary := "복합 제약(교사 공강·학급 빈 교시 불일치)"
	switch {
	case freeClassSlots == 0:
		primary = fmt.Sprintf("%s반 주당 가용 교시(%d) 전량 소진(학급 시수 초과 배당)", u.ClassName, totalSlots)
		suggestions = append(suggestions, "시수 일치성 검사에서 학급별 기준 시수와 배당 합계를 일치시키세요.")
	case roomBusy >= teacherBusy && roomBusy >= hardFail+itinerantFail && roomBusy > 0:
		primary = fmt.Sprintf("교과교실 '%s' 동시 수용(%d학급) 경합", roomName, maxConc)
		suggestions = append(suggestions, fmt.Sprintf("학교 설정에서 '%s' 동시 수용 학급수를 늘리거나 교과교실제를 탄력 운영하세요.", roomName))
	case hardFail+itinerantFail >= teacherBusy && hardFail+itinerantFail > 0:
		primary = "담당/협력 교사의 순회 출강일·고정 공강 제약 과다"
		suggestions = append(suggestions, "교사의 고정 공강(HARD_AVOID) 일부를 기피 교시(SOFT_AVOID)로 완화해 보세요.")
	case teacherBusy > 0:
		primary = "학급 빈 시간대에 담당 교사 모두 타 학급 수업 중"
		suggestions = append(suggestions, "타 학급 수업 또는 2연강/동시수업 그룹 배정 순서를 재조정해 보세요.")
	case streakFail > 0:
		primary = "교사 3연강 초과 제한에 의한 배치 차단"
		suggestions = append(suggestions, "해당 요일의 연속 배정 수업을 다른 요일로 분산해 주세요.")
	}

	details = append(details, fmt.Sprintf("%s반 전체 %d교시 중 빈 교시 %d칸 (순회제한 %d, 고정공강 %d, 타학급중복 %d, 특별실경합 %d, 3연강초과 %d)",
		u.ClassName, totalSlots, freeClassSlots, itinerantFail, hardFail, teacherBusy, roomBusy, streakFail))
	if u.Load.DedicatedRoom != "" {
		details = append(details, fmt.Sprintf("지정 교과교실: %s (동시 수용 %d학급)", roomName, maxConc))
	}

	return protocol.UnplacedDiagnosis{
		UnitID:            u.Load.ID,
		ClassName:         u.ClassName,
		Subject:           u.Subject(),
		TeacherID:         u.Load.TeacherID,
		TeacherName:       tMap[u.Load.TeacherID].Name,
		BlockHours:        u.Load.BlockHours,
		SimultaneousGroup: u.Load.SimultaneousGroup,
		DedicatedRoom:     roomName,
		PrimaryCause:      primary,
		DetailReasons:     details,
		Suggestions:       suggestions,
	}
}
