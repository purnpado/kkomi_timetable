package internal_exam

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

type rolePickCtx struct {
	examDate             string
	period               int
	grade                int
	balanceMode          string
	avoidConsec          bool
	preventSameRoom      bool
	maxDaily             int
	subjectExcludeMode   string
	authorIDs            []string
	gradeSubjectTeachers map[string]map[string]bool
	unavailByDate        map[string]map[string]string
	usage                *slotUsageMaps
}

func (c rolePickCtx) penalty(teacherID, baseClass string, t protocol.Teacher) int {
	p := 0
	if c.maxDaily > 0 && c.usage.dailyCount[c.examDate][teacherID] >= c.maxDaily {
		p += 10
	}
	if (c.avoidConsec || t.ExamNoConsecutive) && isConsecutivePeriod(c.usage.dailyPeriods[c.examDate][teacherID], c.period) {
		p += 4
	}
	if c.preventSameRoom && baseClass != "" && c.usage.hasVisitedRoom(teacherID, baseClass) {
		p += 50
	}
	return p
}

func shouldExcludeForSubject(
	mode string,
	specGrade int,
	roomLabel, specSubject string,
	authorIDs []string,
	t protocol.Teacher,
	gradeSubjectTeachers map[string]map[string]bool,
) bool {
	if mode == "AUTHORS_ONLY" {
		if len(authorIDs) > 0 {
			return slices.Contains(authorIDs, t.ID)
		}
		return roomOrSpecMatchesTeacherSubject(roomLabel, specSubject, t.Subject)
	}
	if mode == "GRADE_ONLY" {
		tokens := effectiveRoomSubjects(roomLabel, specSubject)
		hasTracked := false
		for _, tok := range tokens {
			key := fmt.Sprintf("%d|%s", specGrade, strings.ToLower(strings.TrimSpace(tok)))
			if m := gradeSubjectTeachers[key]; len(m) > 0 {
				hasTracked = true
				if m[t.ID] {
					return true
				}
			}
		}
		if hasTracked {
			return false
		}
	}
	return roomOrSpecMatchesTeacherSubject(roomLabel, specSubject, t.Subject)
}

func pickBestTeacherForRole(
	teachers []protocol.Teacher,
	load map[string]*roleLoad,
	roomLabel string,
	forbiddenSubject string,
	allowSubject bool,
	targetRole string,
	ctx rolePickCtx,
) *protocol.Teacher {
	baseClass := extractRoomBaseClass(roomLabel)
	pk := fmt.Sprintf("%s#%d", ctx.examDate, ctx.period)
	used := ctx.usage.usedPerPeriod[pk]

	filterCands := func(enforceMaxCap bool) []protocol.Teacher {
		var candidates []protocol.Teacher
		for _, t := range teachers {
			if used[t.ID] || isUnavailableOnDate(ctx.unavailByDate, ctx.examDate, t.ID) {
				continue
			}
			if t.ExamNoConsecutive && isConsecutivePeriod(ctx.usage.dailyPeriods[ctx.examDate][t.ID], ctx.period) {
				continue
			}
			if enforceMaxCap && t.ExamMaxCount > 0 && ctx.usage.currentCount[t.ID] >= t.ExamMaxCount {
				continue
			}
			if baseClass != "" && strings.EqualFold(strings.TrimSpace(t.HomeroomClass), baseClass) {
				continue
			}
			if !allowSubject && shouldExcludeForSubject(ctx.subjectExcludeMode, ctx.grade, roomLabel, forbiddenSubject, ctx.authorIDs, t, ctx.gradeSubjectTeachers) {
				continue
			}
			candidates = append(candidates, t)
		}
		return candidates
	}

	candidates := filterCands(true)
	if len(candidates) == 0 {
		candidates = filterCands(false)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		pi := ctx.penalty(candidates[i].ID, baseClass, candidates[i])
		pj := ctx.penalty(candidates[j].ID, baseClass, candidates[j])
		if pi != pj {
			return pi < pj
		}
		li, lj := load[candidates[i].ID], load[candidates[j].ID]
		effTotI := li.Total + candidates[i].ExamReductionWeight
		effTotJ := lj.Total + candidates[j].ExamReductionWeight
		if ctx.balanceMode == "STRICT_ROLE" {
			ri, rj := roleCount(li, targetRole), roleCount(lj, targetRole)
			if ri != rj {
				return ri < rj
			}
			if effTotI != effTotJ {
				return effTotI < effTotJ
			}
		} else {
			if effTotI != effTotJ {
				return effTotI < effTotJ
			}
			ri, rj := roleCount(li, targetRole), roleCount(lj, targetRole)
			if ri != rj {
				return ri < rj
			}
		}
		di, dj := ctx.usage.dailyCount[ctx.examDate][candidates[i].ID], ctx.usage.dailyCount[ctx.examDate][candidates[j].ID]
		if di != dj {
			return di < dj
		}
		if (candidates[i].HomeroomClass == "") != (candidates[j].HomeroomClass == "") {
			return candidates[i].HomeroomClass == ""
		}
		return candidates[i].ID < candidates[j].ID
	})
	picked := candidates[0]
	return &picked
}

func roleCount(l *roleLoad, role string) int {
	switch role {
	case "MAIN":
		return l.Main
	case "SUB":
		return l.Sub
	case "CORRIDOR":
		return l.Corridor
	}
	return l.Total
}

func pickHonoraryParent(
	parents []protocol.Teacher,
	used map[string]bool,
	unavailByDate map[string]map[string]string,
	examDate, roomClass string,
	grade int,
) *protocol.Teacher {
	gradePrefix := fmt.Sprintf("%d-", grade)
	for i := range parents {
		p := parents[i]
		if used[p.ID] || isUnavailableOnDate(unavailByDate, examDate, p.ID) {
			continue
		}
		childClass := strings.TrimSpace(p.ParentOfStudentClass)
		if childClass != "" && (strings.EqualFold(childClass, roomClass) || strings.HasPrefix(childClass, gradePrefix)) {
			continue
		}
		return &p
	}
	return nil
}

func sortSlotsOrder(slots []protocol.ExamSlot) {
	roleRank := map[string]int{"CORRIDOR": 0, "MAIN": 1, "SUB": 2, "HONORARY": 3, "STUDY_HALL": 4}
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].ExamDate != slots[j].ExamDate {
			return slots[i].ExamDate < slots[j].ExamDate
		}
		if slots[i].Period != slots[j].Period {
			return slots[i].Period < slots[j].Period
		}
		if slots[i].RoomName != slots[j].RoomName {
			return slots[i].RoomName < slots[j].RoomName
		}
		return roleRank[slots[i].SlotRole] < roleRank[slots[j].SlotRole]
	})
}

func buildGradeSubjectTeacherMap(store *database.Store) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	if store == nil {
		return out
	}
	add := func(grade int, subj, tid string) {
		if grade <= 0 || strings.TrimSpace(subj) == "" || strings.TrimSpace(tid) == "" {
			return
		}
		for _, tok := range splitSubjectTokens(subj) {
			k := fmt.Sprintf("%d|%s", grade, strings.ToLower(tok))
			if out[k] == nil {
				out[k] = make(map[string]bool)
			}
			out[k][tid] = true
		}
	}
	cells, _ := database.LoadCollection[protocol.TimetableCell](store, "timetable_cells")
	for _, c := range cells {
		for _, tid := range protocol.AllCellTeachers(c) {
			add(c.Grade, c.Subject, tid)
		}
	}
	loads, _ := database.LoadCollection[protocol.TeachingLoad](store, "teaching_loads")
	for _, ld := range loads {
		for _, tid := range protocol.AllLoadTeachers(ld) {
			add(ld.Grade, ld.Subject, tid)
		}
	}
	return out
}
