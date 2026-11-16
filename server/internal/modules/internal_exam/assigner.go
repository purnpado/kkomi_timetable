package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// ExamPeriodSpec은 지필고사 교시별 시험 과목, 과목별 시험시간 및 고사실 정의입니다.
type ExamPeriodSpec struct {
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

// IsEffectiveExamExempt는 교사가 특수반·별도 고사실(독감·감염병 등) 등 교내 시험감독 제외 대상인지 판정합니다.
func IsEffectiveExamExempt(t protocol.Teacher) (bool, string) {
	if t.InternalExamExempt {
		if strings.TrimSpace(t.InternalExamExemptReason) != "" {
			return true, strings.TrimSpace(t.InternalExamExemptReason)
		}
		return true, "특수보직·별도 운영 교내 감독 제외"
	}
	joined := strings.Join([]string{t.Position, t.Subject, t.InternalExamExemptReason}, " ")
	for _, kw := range []string{"특수반", "특수학급", "특수교육", "특수"} {
		if strings.Contains(joined, kw) {
			return true, "특수반(개별고사장 전담)"
		}
	}
	for _, kw := range []string{"별도 고사실", "별도고사실", "별독 고사실", "별독고사실", "독감", "감염병"} {
		if strings.Contains(joined, kw) {
			return true, "별도 고사실(독감·감염병 등 분리시험)"
		}
	}
	return false, ""
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

// AssignSlots는 핵심 배제 규칙(담임 자기반 제외, 시험과목 고사장 제외, 특수반·보건실 제외)을 준수하면서
// 1년 동안 전교원의 감독 시수가 균등(정감독 단독 및 정·부감독 동시 운영 모두 지원)하도록 배정표를 생성합니다.
func AssignSlots(cfg protocol.ExamConfig, specs []ExamPeriodSpec, teachers []protocol.Teacher) []protocol.ExamSlot {
	var slots []protocol.ExamSlot
	load := make(map[string]*roleLoad, len(teachers))
	var regularTeachers, honoraryParents []protocol.Teacher

	for _, t := range teachers {
		if t.IsParentHonorary {
			honoraryParents = append(honoraryParents, t)
			continue
		}
		m, s, c, tot := EffectiveBaseCounts(t)
		load[t.ID] = &roleLoad{Main: m, Sub: s, Corridor: c, Total: tot}
		if exempt, _ := IsEffectiveExamExempt(t); !exempt {
			regularTeachers = append(regularTeachers, t)
		}
	}

	balanceMode := cfg.BalanceMode
	if balanceMode == "" {
		balanceMode = "TOTAL_AND_ROLE"
	}

	for _, spec := range specs {
		usedInPeriod := make(map[string]bool)

		// 1단계: 해당 교시 시험 과목 담당교사를 해당 학년 층 복도감독으로 최우선 배치
		corridorAssigned := false
		if cfg.SubjectTeacherPolicy == "" || cfg.SubjectTeacherPolicy == protocol.SubjectModeCorridorFirst {
			var subjCands []protocol.Teacher
			for _, t := range regularTeachers {
				if roomOrSpecMatchesTeacherSubject("", spec.Subject, t.Subject) && !usedInPeriod[t.ID] {
					subjCands = append(subjCands, t)
				}
			}
			sort.Slice(subjCands, func(i, j int) bool {
				li, lj := load[subjCands[i].ID], load[subjCands[j].ID]
				if li.Total != lj.Total {
					return li.Total < lj.Total
				}
				if li.Corridor != lj.Corridor {
					return li.Corridor < lj.Corridor
				}
				return subjCands[i].ID < subjCands[j].ID
			})
			if len(subjCands) > 0 {
				t := subjCands[0]
				usedInPeriod[t.ID] = true
				load[t.ID].add("CORRIDOR", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "CORRIDOR", fmt.Sprintf("%d학년 층 복도감독 (%s 과목 질의응답)", spec.Grade, spec.Subject), t, "담당과목 시험 교시 복도감독 우선배치"))
				corridorAssigned = true
			}
		}
		if !corridorAssigned {
			if cand := pickBestTeacherForRole(regularTeachers, usedInPeriod, load, "", spec.Subject, true, "CORRIDOR", balanceMode); cand != nil {
				usedInPeriod[cand.ID] = true
				load[cand.ID].add("CORRIDOR", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "CORRIDOR", fmt.Sprintf("%d학년 층 복도감독", spec.Grade), *cand, "복도감독 배치"))
			}
		}

		// 2단계: 모든 고사장의 정감독(MAIN)을 우선 일괄 선발 (정·부감독 균형 보장)
		for _, room := range spec.ClassRooms {
			if mainT := pickBestTeacherForRole(regularTeachers, usedInPeriod, load, room, spec.Subject, false, "MAIN", balanceMode); mainT != nil {
				usedInPeriod[mainT.ID] = true
				load[mainT.ID].add("MAIN", 1)
				slots = append(slots, buildExamSlot(cfg, spec, "MAIN", room+" 고사장", *mainT, ""))
			}
		}

		// 3단계: 학부모 명예감독 또는 부감독(SUB) 배정 (DeputyMode=true일 때만 교원 부감독 배치)
		for _, room := range spec.ClassRooms {
			if cfg.HonoraryParentOn {
				if parent := pickHonoraryParent(honoraryParents, usedInPeriod, room, spec.Grade); parent != nil {
					usedInPeriod[parent.ID] = true
					slots = append(slots, buildExamSlot(cfg, spec, "HONORARY", room+" 고사장", *parent, "학부모 명예감독 (자녀 학년/반 자동 배제)"))
					continue
				}
			}
			if cfg.DeputyMode {
				if subT := pickBestTeacherForRole(regularTeachers, usedInPeriod, load, room, spec.Subject, false, "SUB", balanceMode); subT != nil {
					usedInPeriod[subT.ID] = true
					load[subT.ID].add("SUB", 1)
					slots = append(slots, buildExamSlot(cfg, spec, "SUB", room+" 고사장", *subT, ""))
				}
			}
		}
	}

	// 4단계: 연간 총 감독횟수 및 정·부감독 역할 편차 평탄화 패스
	equalizeAnnualLoads(slots, regularTeachers, load, cfg.DeputyMode)
	sortSlotsOrder(slots)
	return slots
}

func buildExamSlot(cfg protocol.ExamConfig, spec ExamPeriodSpec, role, roomName string, t protocol.Teacher, note string) protocol.ExamSlot {
	suffix := strings.ToLower(role)
	if role == "CORRIDOR" {
		return protocol.ExamSlot{
			ID: fmt.Sprintf("slot-%s-p%d-g%d-corr-%s", cfg.ID, spec.Period, spec.Grade, t.ID),
			ExamID: cfg.ID, ExamDate: cfg.ExamDate, Period: spec.Period, StartTime: spec.StartTime,
			EndTime: spec.EndTime, DurationMinutes: spec.DurationMinutes, Grade: spec.Grade, Subject: spec.Subject,
			SlotRole: role, RoomName: roomName, TeacherID: t.ID, TeacherName: t.Name, Note: note,
		}
	}
	roomCode := strings.TrimSuffix(roomName, " 고사장")
	return protocol.ExamSlot{
		ID: fmt.Sprintf("slot-%s-p%d-%s-%s", cfg.ID, spec.Period, roomCode, suffix),
		ExamID: cfg.ID, ExamDate: cfg.ExamDate, Period: spec.Period, StartTime: spec.StartTime,
		EndTime: spec.EndTime, DurationMinutes: spec.DurationMinutes, Grade: spec.Grade, Subject: spec.Subject,
		SlotRole: role, RoomName: roomName, TeacherID: t.ID, TeacherName: t.Name, Note: note,
	}
}

func pickBestTeacherForRole(
	teachers []protocol.Teacher,
	used map[string]bool,
	load map[string]*roleLoad,
	roomLabel string,
	forbiddenSubject string,
	allowSubject bool,
	targetRole string,
	balanceMode string,
) *protocol.Teacher {
	baseClass := extractRoomBaseClass(roomLabel)
	var candidates []protocol.Teacher
	for _, t := range teachers {
		if used[t.ID] {
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
		li, lj := load[candidates[i].ID], load[candidates[j].ID]
		if balanceMode == "STRICT_ROLE" {
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

func pickHonoraryParent(parents []protocol.Teacher, used map[string]bool, roomClass string, grade int) *protocol.Teacher {
	gradePrefix := fmt.Sprintf("%d-", grade)
	for i := range parents {
		p := parents[i]
		if used[p.ID] {
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
		if slots[i].Period != slots[j].Period {
			return slots[i].Period < slots[j].Period
		}
		if slots[i].RoomName != slots[j].RoomName {
			return slots[i].RoomName < slots[j].RoomName
		}
		return roleRank[slots[i].SlotRole] < roleRank[slots[j].SlotRole]
	})
}
