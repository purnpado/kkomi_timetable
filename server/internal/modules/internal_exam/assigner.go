package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// ExamPeriodSpec은 지필고사 일자·교시별 시험 과목, 과목별 시험시간 및 고사실 정의입니다.
type ExamPeriodSpec struct {
	ExamDate        string   `json:"examDate,omitempty"` // 다중일 시험 시 해당 교시의 시험 일자 (YYYY-MM-DD)
	Period          int      `json:"period"`
	Grade           int      `json:"grade"`
	Subject         string   `json:"subject"`
	StartTime       string   `json:"startTime"`       // 예: "08:50"
	EndTime         string   `json:"endTime"`         // 예: "09:35"
	DurationMinutes int      `json:"durationMinutes"` // 예: 45
	ClassRooms      []string `json:"classRooms"`      // 예: ["1-1", "1-2"]
}

type roleLoad struct {
	Main     int
	Sub      int
	Corridor int
	Total    int
}

func (l *roleLoad) add(role string, delta int) {
	switch role {
	case "MAIN":
		l.Main += delta
	case "SUB":
		l.Sub += delta
	case "CORRIDOR":
		l.Corridor += delta
	}
	l.Total = l.Main + l.Sub + l.Corridor
}

func extractRoomBaseClass(roomLabel string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(roomLabel), " 고사장"))
	if idx := strings.Index(s, "("); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

func roomOrSpecMatchesTeacherSubject(roomLabel, specSubject, teacherSubject string) bool {
	ts := strings.TrimSpace(teacherSubject)
	if ts == "" {
		return false
	}
	tokens := splitSubjectTokens(specSubject)
	if start := strings.Index(roomLabel, "("); start >= 0 {
		if end := strings.LastIndex(roomLabel, ")"); end > start {
			tokens = append(tokens, splitSubjectTokens(roomLabel[start+1:end])...)
		}
	}
	for _, tok := range tokens {
		if strings.EqualFold(tok, ts) || (len([]rune(ts)) >= 2 && strings.Contains(tok, ts)) {
			return true
		}
	}
	return false
}

func splitSubjectTokens(raw string) []string {
	f := func(r rune) bool { return r == ',' || r == '/' || r == ':' || r == ';' }
	var out []string
	for _, part := range strings.FieldsFunc(raw, f) {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// EffectiveBaseCounts는 교사의 1년 기실시(이전 시험 완료) 역할별 감독 횟수를 반환합니다.
func EffectiveBaseCounts(t protocol.Teacher) (mainCnt, subCnt, corrCnt, totalCnt int) {
	sum := t.AnnualMainCount + t.AnnualSubCount + t.AnnualCorridorCount
	if sum > 0 {
		return t.AnnualMainCount, t.AnnualSubCount, t.AnnualCorridorCount, sum
	}
	if t.InternalExamCount > 0 {
		m := t.InternalExamCount/2 + t.InternalExamCount%2
		s := t.InternalExamCount - m
		return m, s, 0, t.InternalExamCount
	}
	return 0, 0, 0, 0
}

// AssignSlots는 핵심 배제 규칙(담임 자기반 제외, 시험과목 고사장 제외, 특수반·관리자 제외)을 준수하면서
// 1년 동안 전교원의 감독 시수가 균등하도록 배정표를 생성합니다.
func AssignSlots(cfg protocol.ExamConfig, specs []ExamPeriodSpec, teachers []protocol.Teacher) []protocol.ExamSlot {
	return AssignSlotsWithUnavailable(cfg, specs, teachers, nil)
}

// AssignSlotsWithUnavailable은 당일 부재 교사(순회 비출강·외부감독·행사출장)까지 제외하고 감독 배정표를 생성합니다.
func AssignSlotsWithUnavailable(
	cfg protocol.ExamConfig,
	specs []ExamPeriodSpec,
	teachers []protocol.Teacher,
	unavailable map[string]string,
) []protocol.ExamSlot {
	var unavailByDate map[string]map[string]string
	if len(unavailable) > 0 {
		unavailByDate = make(map[string]map[string]string)
		dates := collectExamDates(cfg, specs)
		if len(dates) == 0 {
			dates = []string{""}
		}
		for _, d := range dates {
			unavailByDate[d] = unavailable
		}
	}
	return AssignSlotsMultiDay(cfg, specs, teachers, unavailByDate)
}

// AssignSlotsMultiDay는 다중일(3~5일) 지필고사를 통합 편성하면서 일자별 부재 교사,
// 연속 교시(연강) 감독 방지 및 1일 최대 감독 상한·연간 균등화를 동시 보장합니다.
func AssignSlotsMultiDay(
	cfg protocol.ExamConfig,
	specs []ExamPeriodSpec,
	teachers []protocol.Teacher,
	unavailByDate map[string]map[string]string,
) []protocol.ExamSlot {
	sortedSpecs := append([]ExamPeriodSpec(nil), specs...)
	sort.SliceStable(sortedSpecs, func(i, j int) bool {
		di, dj := effectiveSpecDate(cfg, sortedSpecs[i]), effectiveSpecDate(cfg, sortedSpecs[j])
		if di != dj {
			return di < dj
		}
		if sortedSpecs[i].Period != sortedSpecs[j].Period {
			return sortedSpecs[i].Period < sortedSpecs[j].Period
		}
		return sortedSpecs[i].Grade < sortedSpecs[j].Grade
	})

	allDates := collectExamDates(cfg, sortedSpecs)
	if len(allDates) == 0 {
		allDates = []string{""}
	}

	var slots []protocol.ExamSlot
	load := make(map[string]*roleLoad, len(teachers))
	var regularTeachers, specialTeachers, honoraryParents []protocol.Teacher

	for _, t := range teachers {
		if t.IsParentHonorary {
			honoraryParents = append(honoraryParents, t)
			continue
		}
		if protocol.IsSystemAdminAccount(t) {
			continue
		}
		if len(unavailByDate) > 0 {
			availAny := false
			for _, d := range allDates {
				if !isUnavailableOnDate(unavailByDate, d, t.ID) {
					availAny = true
					break
				}
			}
			if !availAny {
				continue
			}
		}
		m, s, c, tot := EffectiveBaseCounts(t)
		load[t.ID] = &roleLoad{Main: m, Sub: s, Corridor: c, Total: tot}
		if exempt, _ := IsEffectiveExamExempt(t); !exempt {
			regularTeachers = append(regularTeachers, t)
		} else if isSpecialClassSupervisor(t) {
			specialTeachers = append(specialTeachers, t)
		}
	}

	balanceMode := cfg.BalanceMode
	if balanceMode == "" {
		balanceMode = "TOTAL_AND_ROLE"
	}

	usage := &slotUsageMaps{
		usedPerPeriod: make(map[string]map[string]bool),
		dailyPeriods:  make(map[string]map[string]map[int]bool),
		dailyCount:    make(map[string]map[string]int),
	}

	for _, spec := range sortedSpecs {
		examDate := effectiveSpecDate(cfg, spec)
		periodKey := fmt.Sprintf("%s#%d", examDate, spec.Period)
		if usage.usedPerPeriod[periodKey] == nil {
			usage.usedPerPeriod[periodKey] = make(map[string]bool)
		}
		usedInPeriod := usage.usedPerPeriod[periodKey]
		ctx := rolePickCtx{
			examDate:      examDate,
			period:        spec.Period,
			balanceMode:   balanceMode,
			avoidConsec:   cfg.AvoidConsecutive,
			maxDaily:      cfg.MaxDailyPerTeacher,
			unavailByDate: unavailByDate,
			usage:         usage,
		}

		// 1단계: 해당 교시 시험 과목 담당교사를 해당 학년 층 복도감독으로 최우선 배치
		corridorAssigned := false
		if cfg.SubjectTeacherPolicy == "" || cfg.SubjectTeacherPolicy == protocol.SubjectModeCorridorFirst {
			var subjCands []protocol.Teacher
			for _, t := range regularTeachers {
				if !usedInPeriod[t.ID] && !isUnavailableOnDate(unavailByDate, examDate, t.ID) &&
					roomOrSpecMatchesTeacherSubject("", spec.Subject, t.Subject) {
					subjCands = append(subjCands, t)
				}
			}
			sort.Slice(subjCands, func(i, j int) bool {
				ci := ctx.penalty(subjCands[i].ID)
				cj := ctx.penalty(subjCands[j].ID)
				if ci != cj {
					return ci < cj
				}
				li, lj := load[subjCands[i].ID], load[subjCands[j].ID]
				if li.Total != lj.Total {
					return li.Total < lj.Total
				}
				if li.Corridor != lj.Corridor {
					return li.Corridor < lj.Corridor
				}
				di, dj := usage.dailyCount[examDate][subjCands[i].ID], usage.dailyCount[examDate][subjCands[j].ID]
				if di != dj {
					return di < dj
				}
				return subjCands[i].ID < subjCands[j].ID
			})
			if len(subjCands) > 0 {
				t := subjCands[0]
				usage.mark(t.ID, examDate, spec.Period, true)
				load[t.ID].add("CORRIDOR", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "CORRIDOR", fmt.Sprintf("%d학년 층 복도감독 (%s 과목 질의응답)", spec.Grade, spec.Subject), t, "담당과목 시험 교시 복도감독 우선배치"))
				corridorAssigned = true
			}
		}
		if !corridorAssigned {
			if cand := pickBestTeacherForRole(regularTeachers, load, "", spec.Subject, true, "CORRIDOR", ctx); cand != nil {
				usage.mark(cand.ID, examDate, spec.Period, true)
				load[cand.ID].add("CORRIDOR", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "CORRIDOR", fmt.Sprintf("%d학년 층 복도감독", spec.Grade), *cand, "복도감독 배치"))
			}
		}

		// 2단계: 모든 고사장의 정감독(MAIN)을 우선 일괄 선발 (특수학급고사실은 특수교사 최우선 전담)
		for _, room := range spec.ClassRooms {
			if isSpecialClassExamRoom(room) {
				if specT := pickSpecialTeacherForRoom(specialTeachers, load, ctx); specT != nil {
					usage.mark(specT.ID, examDate, spec.Period, true)
					load[specT.ID].add("MAIN", 1)
					slots = append(slots, buildExamSlot(cfg, spec, "MAIN", room+" 고사장", *specT, "특수학급고사실 특수교사 전담 배정"))
					continue
				}
				if fbT := pickBestTeacherForRole(regularTeachers, load, room, spec.Subject, false, "MAIN", ctx); fbT != nil {
					usage.mark(fbT.ID, examDate, spec.Period, true)
					load[fbT.ID].add("MAIN", 1)
					slots = append(slots, buildExamSlot(cfg, spec, "MAIN", room+" 고사장", *fbT, "특수교사 부재/미등록으로 일반교사 대체 배정"))
				}
				continue
			}
			if mainT := pickBestTeacherForRole(regularTeachers, load, room, spec.Subject, false, "MAIN", ctx); mainT != nil {
				usage.mark(mainT.ID, examDate, spec.Period, true)
				load[mainT.ID].add("MAIN", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "MAIN", room+" 고사장", *mainT, ""))
			}
		}

		// 3단계: 학부모 명예감독 또는 부감독(SUB) 배정 (단, 별도/특수학급고사실은 1인 정감독만 배정)
		for _, room := range spec.ClassRooms {
			if isIsolatedExamRoom(room) {
				continue
			}
			if cfg.HonoraryParentOn {
				if parent := pickHonoraryParent(honoraryParents, usedInPeriod, unavailByDate, examDate, room, spec.Grade); parent != nil {
					usage.mark(parent.ID, examDate, spec.Period, true)
					slots = append(slots, buildExamSlot(cfg, spec, "HONORARY", room+" 고사장", *parent, "학부모 명예감독 (자녀 학년/반 자동 배제)"))
					continue
				}
			}
			if cfg.DeputyMode {
				if subT := pickBestTeacherForRole(regularTeachers, load, room, spec.Subject, false, "SUB", ctx); subT != nil {
					usage.mark(subT.ID, examDate, spec.Period, true)
					load[subT.ID].add("SUB", 1)
					slots = append(slots, buildExamSlot(cfg, spec, "SUB", room+" 고사장", *subT, ""))
				}
			}
		}
	}

	// 4단계: 연간 총 감독횟수·정/부감독 편차 평탄화 (일자별 부재·일일 상한·연강 제약 준수)
	equalizeAnnualLoadsWithConstraints(slots, regularTeachers, load, cfg.DeputyMode, unavailByDate, cfg.AvoidConsecutive, cfg.MaxDailyPerTeacher)
	// 5단계: 연강 감독 방지 옵션 시 잔여 연강 쌍 해소
	if cfg.AvoidConsecutive {
		resolveConsecutivePairs(slots, regularTeachers, load, unavailByDate, cfg.MaxDailyPerTeacher)
	}
	sortSlotsOrder(slots)
	return slots
}

type rolePickCtx struct {
	examDate      string
	period        int
	balanceMode   string
	avoidConsec   bool
	maxDaily      int
	unavailByDate map[string]map[string]string
	usage         *slotUsageMaps
}

func (c rolePickCtx) penalty(teacherID string) int {
	p := 0
	if c.maxDaily > 0 && c.usage.dailyCount[c.examDate][teacherID] >= c.maxDaily {
		p += 10
	}
	if c.avoidConsec && isConsecutivePeriod(c.usage.dailyPeriods[c.examDate][teacherID], c.period) {
		p += 4
	}
	return p
}

func buildExamSlot(cfg protocol.ExamConfig, spec ExamPeriodSpec, role, roomName string, t protocol.Teacher, note string) protocol.ExamSlot {
	suffix := strings.ToLower(role)
	examDate := effectiveSpecDate(cfg, spec)
	dateSeg := examDate
	if dateSeg == "" {
		dateSeg = "d0"
	}
	if role == "CORRIDOR" {
		return protocol.ExamSlot{
			ID:     fmt.Sprintf("slot-%s-%s-p%d-g%d-corr-%s", cfg.ID, dateSeg, spec.Period, spec.Grade, t.ID),
			ExamID: cfg.ID, ExamDate: examDate, Period: spec.Period, StartTime: spec.StartTime,
			EndTime: spec.EndTime, DurationMinutes: spec.DurationMinutes, Grade: spec.Grade, Subject: spec.Subject,
			SlotRole: role, RoomName: roomName, TeacherID: t.ID, TeacherName: t.Name, Note: note,
		}
	}
	roomCode := strings.TrimSuffix(roomName, " 고사장")
	return protocol.ExamSlot{
		ID:     fmt.Sprintf("slot-%s-%s-p%d-g%d-%s-%s", cfg.ID, dateSeg, spec.Period, spec.Grade, roomCode, suffix),
		ExamID: cfg.ID, ExamDate: examDate, Period: spec.Period, StartTime: spec.StartTime,
		EndTime: spec.EndTime, DurationMinutes: spec.DurationMinutes, Grade: spec.Grade, Subject: spec.Subject,
		SlotRole: role, RoomName: roomName, TeacherID: t.ID, TeacherName: t.Name, Note: note,
	}
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
	var candidates []protocol.Teacher
	for _, t := range teachers {
		if used[t.ID] || isUnavailableOnDate(ctx.unavailByDate, ctx.examDate, t.ID) {
			continue
		}
		if baseClass != "" && strings.EqualFold(strings.TrimSpace(t.HomeroomClass), baseClass) {
			continue
		}
		if !allowSubject && roomOrSpecMatchesTeacherSubject(roomLabel, forbiddenSubject, t.Subject) {
			continue
		}
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		pi, pj := ctx.penalty(candidates[i].ID), ctx.penalty(candidates[j].ID)
		if pi != pj {
			return pi < pj
		}
		li, lj := load[candidates[i].ID], load[candidates[j].ID]
		if ctx.balanceMode == "STRICT_ROLE" {
			ri, rj := roleCount(li, targetRole), roleCount(lj, targetRole)
			if ri != rj {
				return ri < rj
			}
			if li.Total != lj.Total {
				return li.Total < lj.Total
			}
		} else {
			if li.Total != lj.Total {
				return li.Total < lj.Total
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
	roleRank := map[string]int{"CORRIDOR": 0, "MAIN": 1, "SUB": 2, "HONORARY": 3}
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
