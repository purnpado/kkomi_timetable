package internal_exam

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

func effectiveSpecDate(cfg protocol.ExamConfig, sp ExamPeriodSpec) string {
	if d := strings.TrimSpace(sp.ExamDate); d != "" {
		return d
	}
	if d := strings.TrimSpace(cfg.ExamDate); d != "" {
		return d
	}
	if len(cfg.ExamDates) > 0 {
		return strings.TrimSpace(cfg.ExamDates[0])
	}
	return ""
}

func collectExamDates(cfg protocol.ExamConfig, specs []ExamPeriodSpec) []string {
	seen := make(map[string]bool)
	add := func(raw string) {
		if d := strings.TrimSpace(raw); d != "" {
			seen[d] = true
		}
	}
	for _, d := range cfg.ExamDates {
		add(d)
	}
	add(cfg.ExamDate)
	for _, sp := range specs {
		add(sp.ExamDate)
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func isSlotSealed(cfg protocol.ExamConfig, examDate string) bool {
	if !cfg.IsSealed {
		return false
	}
	return !slices.Contains(cfg.UnsealedDates, strings.TrimSpace(examDate))
}

func isConsecutivePeriod(periods map[int]bool, p int) bool {
	return periods != nil && (periods[p-1] || periods[p+1])
}

func isConsecutiveExcluding(periods map[int]bool, p, excludeP int) bool {
	if periods == nil {
		return false
	}
	hasPrev := p-1 != excludeP && periods[p-1]
	hasNext := p+1 != excludeP && periods[p+1]
	return hasPrev || hasNext
}

func canTeacherFitSlot(
	t protocol.Teacher,
	sl protocol.ExamSlot,
	allowSubjectCorridor bool,
	tByID map[string]protocol.Teacher,
	unavailByDate map[string]map[string]string,
) bool {
	if sl.SlotRole == "HONORARY" {
		return false
	}
	if isUnavailableOnDate(unavailByDate, sl.ExamDate, t.ID) {
		return false
	}
	if sl.SlotRole == "CORRIDOR" || sl.SlotRole == "STUDY_HALL" {
		if !allowSubjectCorridor {
			return false
		}
		if sl.SlotRole == "CORRIDOR" {
			if curT, ok := tByID[sl.TeacherID]; ok && roomOrSpecMatchesTeacherSubject("", sl.Subject, curT.Subject) {
				return roomOrSpecMatchesTeacherSubject("", sl.Subject, t.Subject)
			}
		}
		return true
	}
	if isSpecialClassExamRoom(sl.RoomName) && !isSpecialClassSupervisor(t) {
		if curT, ok := tByID[sl.TeacherID]; !ok || isSpecialClassSupervisor(curT) {
			return false
		}
	}
	roomClass := extractRoomBaseClass(sl.RoomName)
	if roomClass != "" && strings.EqualFold(strings.TrimSpace(t.HomeroomClass), roomClass) {
		return false
	}
	if roomOrSpecMatchesTeacherSubject(sl.RoomName, sl.Subject, t.Subject) {
		return false
	}
	return true
}

func computeMultiDayFairnessStats(slots []protocol.ExamSlot, exemptIDs map[string]bool) (dayCount, consecutivePairs, maxDaily int) {
	dateSet := make(map[string]bool)
	byDayTeacher := make(map[string][]int)
	for _, sl := range slots {
		d := strings.TrimSpace(sl.ExamDate)
		if d != "" {
			dateSet[d] = true
		}
		if sl.TeacherID == "" || exemptIDs[sl.TeacherID] {
			continue
		}
		k := d + "|" + sl.TeacherID
		byDayTeacher[k] = append(byDayTeacher[k], sl.Period)
	}
	dayCount = len(dateSet)
	for _, plist := range byDayTeacher {
		if len(plist) > maxDaily {
			maxDaily = len(plist)
		}
		if len(plist) < 2 {
			continue
		}
		sort.Ints(plist)
		prev := -10
		for _, p := range plist {
			if p == prev {
				continue
			}
			if p == prev+1 {
				consecutivePairs++
			}
			prev = p
		}
	}
	return dayCount, consecutivePairs, maxDaily
}

type slotUsageMaps struct {
	usedPerPeriod map[string]map[string]bool
	dailyPeriods  map[string]map[string]map[int]bool
	dailyCount    map[string]map[string]int
	currentCount  map[string]int
	roomHistory   map[string]map[string]bool
}

func newSlotUsageMaps() *slotUsageMaps {
	return &slotUsageMaps{
		usedPerPeriod: make(map[string]map[string]bool),
		dailyPeriods:  make(map[string]map[string]map[int]bool),
		dailyCount:    make(map[string]map[string]int),
		currentCount:  make(map[string]int),
		roomHistory:   make(map[string]map[string]bool),
	}
}

func buildSlotUsageMaps(slots []protocol.ExamSlot) *slotUsageMaps {
	u := newSlotUsageMaps()
	for _, sl := range slots {
		u.markWithRoom(sl.TeacherID, sl.ExamDate, sl.Period, sl.RoomName, true)
	}
	return u
}

func (u *slotUsageMaps) hasVisitedRoom(teacherID, baseClass string) bool {
	if baseClass == "" || u.roomHistory[teacherID] == nil {
		return false
	}
	return u.roomHistory[teacherID][strings.TrimSpace(baseClass)]
}

func (u *slotUsageMaps) mark(teacherID, date string, period int, add bool) {
	u.markWithRoom(teacherID, date, period, "", add)
}

func (u *slotUsageMaps) markWithRoom(teacherID, date string, period int, roomName string, add bool) {
	if teacherID == "" {
		return
	}
	pk := fmt.Sprintf("%s#%d", date, period)
	if u.usedPerPeriod[pk] == nil {
		u.usedPerPeriod[pk] = make(map[string]bool)
	}
	if u.dailyPeriods[date] == nil {
		u.dailyPeriods[date] = make(map[string]map[int]bool)
	}
	if u.dailyPeriods[date][teacherID] == nil {
		u.dailyPeriods[date][teacherID] = make(map[int]bool)
	}
	if u.dailyCount[date] == nil {
		u.dailyCount[date] = make(map[string]int)
	}
	if add {
		u.usedPerPeriod[pk][teacherID] = true
		u.dailyPeriods[date][teacherID][period] = true
		u.dailyCount[date][teacherID]++
		u.currentCount[teacherID]++
		if bc := extractRoomBaseClass(roomName); bc != "" {
			if u.roomHistory[teacherID] == nil {
				u.roomHistory[teacherID] = make(map[string]bool)
			}
			u.roomHistory[teacherID][bc] = true
		}
	} else {
		u.usedPerPeriod[pk][teacherID] = false
		u.dailyPeriods[date][teacherID][period] = false
		if u.dailyCount[date][teacherID] > 0 {
			u.dailyCount[date][teacherID]--
		}
		if u.currentCount[teacherID] > 0 {
			u.currentCount[teacherID]--
		}
	}
}

func resolveConsecutivePairs(
	slots []protocol.ExamSlot,
	regularTeachers []protocol.Teacher,
	load map[string]*roleLoad,
	unavailByDate map[string]map[string]string,
	maxDaily int,
) {
	if len(regularTeachers) < 2 || len(slots) == 0 {
		return
	}
	tByID := make(map[string]protocol.Teacher, len(regularTeachers))
	for _, t := range regularTeachers {
		tByID[t.ID] = t
	}
	usage := buildSlotUsageMaps(slots)

	for iter := 0; iter < 120; iter++ {
		changed := false
		for i := range slots {
			if slots[i].SlotRole == "HONORARY" {
				continue
			}
			oldID := slots[i].TeacherID
			oldT, ok := tByID[oldID]
			if !ok {
				continue
			}
			d1, p1 := slots[i].ExamDate, slots[i].Period
			if !isConsecutivePeriod(usage.dailyPeriods[d1][oldID], p1) {
				continue
			}

			// 1순위: 타 교시 슬롯과의 맞교환 (총 시수 균형 보존, 동일 역할 우선)
			for _, sameRoleOnly := range []bool{true, false} {
				if changed {
					break
				}
				for j := range slots {
					if i == j || slots[j].SlotRole == "HONORARY" {
						continue
					}
					if sameRoleOnly && slots[i].SlotRole != slots[j].SlotRole {
						continue
					}
					d2, p2 := slots[j].ExamDate, slots[j].Period
					if d1 == d2 && p1 == p2 {
						continue
					}
					otherID := slots[j].TeacherID
					otherT, okOther := tByID[otherID]
					if !okOther || otherID == oldID {
						continue
					}
					k1 := fmt.Sprintf("%s#%d", d1, p1)
					k2 := fmt.Sprintf("%s#%d", d2, p2)
					if usage.usedPerPeriod[k1][otherID] || usage.usedPerPeriod[k2][oldID] {
						continue
					}
					if maxDaily > 0 && d1 != d2 && (usage.dailyCount[d1][otherID] >= maxDaily || usage.dailyCount[d2][oldID] >= maxDaily) {
						continue
					}
					if !canTeacherFitSlot(otherT, slots[i], true, tByID, unavailByDate) ||
						!canTeacherFitSlot(oldT, slots[j], true, tByID, unavailByDate) {
						continue
					}
					if (otherT.ExamMaxCount > 0 && usage.currentCount[otherID] >= otherT.ExamMaxCount) ||
						(oldT.ExamMaxCount > 0 && usage.currentCount[oldID] >= oldT.ExamMaxCount) {
						// 맞교환은 currentCount 변동 없음
					}
					exclOld := -1
					exclOther := -1
					if d1 == d2 {
						exclOld, exclOther = p1, p2
					}
					if isConsecutiveExcluding(usage.dailyPeriods[d1][otherID], p1, exclOther) ||
						isConsecutiveExcluding(usage.dailyPeriods[d2][oldID], p2, exclOld) {
						continue
					}
					usage.mark(oldID, d1, p1, false)
					usage.mark(otherID, d2, p2, false)
					usage.mark(otherID, d1, p1, true)
					usage.mark(oldID, d2, p2, true)
					if slots[i].SlotRole != slots[j].SlotRole {
						load[oldID].add(slots[i].SlotRole, -1)
						load[oldID].add(slots[j].SlotRole, 1)
						load[otherID].add(slots[j].SlotRole, -1)
						load[otherID].add(slots[i].SlotRole, 1)
					}
					slots[i].TeacherID, slots[i].TeacherName = otherT.ID, otherT.Name
					slots[j].TeacherID, slots[j].TeacherName = oldT.ID, oldT.Name
					changed = true
					break
				}
			}
			if changed {
				break
			}

			// 2순위: 동일 교시 비연강 여유 교사에게 직접 이관 (전체 편차가 허용 범위일 때만)
			minTot, maxTot := 1<<30, -1
			for _, rt := range regularTeachers {
				tot := load[rt.ID].Total
				if tot < minTot {
					minTot = tot
				}
				if tot > maxTot {
					maxTot = tot
				}
			}
			allowedMax := max(maxTot, minTot+1)
			k1 := fmt.Sprintf("%s#%d", d1, p1)
			for _, cand := range regularTeachers {
				if cand.ID == oldID || usage.usedPerPeriod[k1][cand.ID] {
					continue
				}
				if load[cand.ID].Total+1 > allowedMax || load[oldID].Total-1 < minTot {
					continue
				}
				if maxDaily > 0 && usage.dailyCount[d1][cand.ID] >= maxDaily {
					continue
				}
				if cand.ExamMaxCount > 0 && usage.currentCount[cand.ID] >= cand.ExamMaxCount {
					continue
				}
				if isConsecutivePeriod(usage.dailyPeriods[d1][cand.ID], p1) {
					continue
				}
				if canTeacherFitSlot(cand, slots[i], true, tByID, unavailByDate) {
					usage.mark(oldID, d1, p1, false)
					usage.mark(cand.ID, d1, p1, true)
					load[oldID].add(slots[i].SlotRole, -1)
					load[cand.ID].add(slots[i].SlotRole, 1)
					slots[i].TeacherID, slots[i].TeacherName = cand.ID, cand.Name
					changed = true
					break
				}
			}
			if changed {
				break
			}
		}
		if !changed {
			break
		}
	}
}

// UnsealExamDate는 다중일 지필고사에서 특정 시험 일자만 부분 공개하며, 모든 시험일이 공개되면 전체 봉인을 해제합니다.
func (s *Service) UnsealExamDate(dateStr, actorName string) (protocol.ExamConfig, error) {
	trimmed := strings.TrimSpace(dateStr)
	if trimmed == "" {
		return s.UnsealExam("", actorName)
	}
	cfg, err := s.GetConfig()
	if err != nil {
		return protocol.ExamConfig{}, err
	}
	allDates := collectExamDates(cfg, s.ListSpecs())
	if !slices.Contains(cfg.UnsealedDates, trimmed) {
		cfg.UnsealedDates = append(cfg.UnsealedDates, trimmed)
		sort.Strings(cfg.UnsealedDates)
	}
	if len(allDates) > 0 {
		allRevealed := true
		for _, d := range allDates {
			if !slices.Contains(cfg.UnsealedDates, d) {
				allRevealed = false
				break
			}
		}
		if allRevealed {
			cfg.IsSealed = false
		}
	} else {
		cfg.IsSealed = false
	}
	if err := s.store.SaveRecord("exam_configs", cfg.ID, cfg); err != nil {
		return cfg, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExamUnsealed,
			Title:     fmt.Sprintf("📢 [%s] %s 고사장이 공개되었습니다!", cfg.Title, trimmed),
			Message:   fmt.Sprintf("%s 시험 고사장 배정이 공개되었습니다. 시작 10분 전 평가본부로 집결해 주세요.", trimmed),
			Urgent:    true,
			ActorName: actorName,
			Payload:   cfg,
		})
	}
	return cfg, nil
}
