package timetable

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ListConstraints는 등록된 교사별 공강·기피 교시 제약을 교사명·요일·교시 순으로 반환합니다.
func (s *Service) ListConstraints() ([]protocol.TeacherConstraint, error) {
	list, err := database.LoadCollection[protocol.TeacherConstraint](s.store, "teacher_constraints")
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].TeacherName != list[j].TeacherName {
			return list[i].TeacherName < list[j].TeacherName
		}
		if list[i].DayOfWeek != list[j].DayOfWeek {
			return list[i].DayOfWeek < list[j].DayOfWeek
		}
		return list[i].Period < list[j].Period
	})
	return list, nil
}

// SaveConstraint는 교사 1명의 특정 요일·교시 제약 조건을 등록 또는 갱신합니다.
func (s *Service) SaveConstraint(c protocol.TeacherConstraint, actorName string) (protocol.TeacherConstraint, error) {
	c.TeacherID = strings.TrimSpace(c.TeacherID)
	if c.TeacherID == "" {
		return c, errors.New("대상 교사를 선택해 주세요")
	}
	if c.DayOfWeek < 0 || c.DayOfWeek > 5 {
		return c, errors.New("요일은 0(전체) 또는 1(월)~5(금) 범위여야 합니다")
	}
	if c.Period < 0 || c.Period > 7 {
		return c, errors.New("교시는 0(종일) 또는 1~7교시 범위여야 합니다")
	}
	c.ConstraintType = normalizeConstraintType(c.ConstraintType)

	t, err := database.LoadRecord[protocol.Teacher](s.store, "teachers", c.TeacherID)
	if err != nil || t == nil {
		return c, fmt.Errorf("대상 교사(%s)를 찾을 수 없습니다", c.TeacherID)
	}
	if protocol.IsSystemAdminAccount(*t) || t.IsParentHonorary {
		return c, errors.New("관리자 및 명예감독 계정에는 수업 제약을 설정할 수 없습니다")
	}
	c.TeacherName = t.Name
	if strings.TrimSpace(c.Reason) == "" {
		c.Reason = defaultConstraintReason(c.ConstraintType)
	}
	if c.ID == "" {
		c.ID = fmt.Sprintf("tc-%s-%d-%d", c.TeacherID, c.DayOfWeek, c.Period)
	}
	if c.CreatedAt == "" {
		c.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if err := s.store.SaveRecord("teacher_constraints", c.ID, c); err != nil {
		return c, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTeacherConstraintUpdated,
			Title:     fmt.Sprintf("[%s] 교사 제약 조건 저장", c.TeacherName),
			Message:   fmt.Sprintf("%s (%s)", c.Reason, constraintTypeLabel(c.ConstraintType)),
			ActorName: actorName,
			Payload:   c,
		})
	}
	return c, nil
}

// BatchSaveConstraints는 부장회의(부장 전체) 또는 교과협의회(특정 교과 전체) 고정 공강을 일괄 등록합니다.
func (s *Service) BatchSaveConstraints(req protocol.BatchConstraintRequest, actorName string) ([]protocol.TeacherConstraint, error) {
	if req.DayOfWeek < 0 || req.DayOfWeek > 5 || req.Period < 0 || req.Period > 7 {
		return nil, errors.New("유효한 요일(0~5)과 교시(0~7)를 지정해 주세요")
	}
	ctype := normalizeConstraintType(req.ConstraintType)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = defaultConstraintReason(ctype)
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	var targets []protocol.Teacher
	switch req.TargetScope {
	case "DEPT_HEADS":
		for _, t := range teachers {
			if t.IsDepartmentHead && !t.IsParentHonorary && !protocol.IsSystemAdminAccount(t) {
				targets = append(targets, t)
			}
		}
	case "SUBJECT":
		subj := strings.TrimSpace(req.Subject)
		if subj == "" {
			return nil, errors.New("일괄 등록할 교과명을 입력해 주세요")
		}
		for _, t := range teachers {
			if !t.IsParentHonorary && !protocol.IsSystemAdminAccount(t) && strings.EqualFold(strings.TrimSpace(t.Subject), subj) {
				targets = append(targets, t)
			}
		}
	default:
		for _, t := range teachers {
			if !t.IsParentHonorary && !protocol.IsSystemAdminAccount(t) && slices.Contains(req.TeacherIDs, t.ID) {
				targets = append(targets, t)
			}
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("일괄 제약을 적용할 대상 교사가 없습니다")
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	saved := make([]protocol.TeacherConstraint, 0, len(targets))
	for _, t := range targets {
		saved = append(saved, protocol.TeacherConstraint{
			ID:             fmt.Sprintf("tc-%s-%d-%d", t.ID, req.DayOfWeek, req.Period),
			TeacherID:      t.ID,
			TeacherName:    t.Name,
			GroupTag:       strings.TrimSpace(req.GroupTag),
			DayOfWeek:      req.DayOfWeek,
			Period:         req.Period,
			ConstraintType: ctype,
			Reason:         reason,
			CreatedAt:      nowStr,
		})
	}
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		for _, c := range saved {
			if sErr := tx.SaveRecord("teacher_constraints", c.ID, c); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTeacherConstraintUpdated,
			Title:     fmt.Sprintf("고정 공강·제약 일괄 등록 (%d명)", len(saved)),
			Message:   reason,
			ActorName: actorName,
			Payload:   map[string]interface{}{"count": len(saved), "groupTag": req.GroupTag},
		})
	}
	return saved, nil
}

// DeleteConstraint는 등록된 교사 제약 조건 1건을 삭제합니다.
func (s *Service) DeleteConstraint(id, actorName string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("삭제할 제약 ID가 필요합니다")
	}
	if err := s.store.DeleteRecord("teacher_constraints", id); err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTeacherConstraintUpdated,
			Title:     "교사 제약 조건 삭제",
			Message:   "교사 제약 설정이 갱신되었습니다.",
			ActorName: actorName,
			Payload:   map[string]string{"deletedId": id},
		})
	}
	return nil
}

func normalizeConstraintType(ct protocol.ConstraintType) protocol.ConstraintType {
	switch ct {
	case protocol.ConstraintSoftAvoid, protocol.ConstraintMeetingFree, protocol.ConstraintHardAvoid:
		return ct
	default:
		return protocol.ConstraintHardAvoid
	}
}

func defaultConstraintReason(ct protocol.ConstraintType) string {
	switch ct {
	case protocol.ConstraintMeetingFree:
		return "회의·협의회 고정 공강"
	case protocol.ConstraintSoftAvoid:
		return "희망 공강(기피 교시)"
	default:
		return "배정 금지 교시"
	}
}

func constraintTypeLabel(ct protocol.ConstraintType) string {
	switch ct {
	case protocol.ConstraintMeetingFree:
		return "회의 공강"
	case protocol.ConstraintSoftAvoid:
		return "기피 교시"
	default:
		return "절대 금지"
	}
}

func constraintMatchesSlot(c protocol.TeacherConstraint, teacherID string, day, period int) bool {
	if c.TeacherID != teacherID {
		return false
	}
	if c.DayOfWeek != 0 && c.DayOfWeek != day {
		return false
	}
	if c.Period != 0 && c.Period != period {
		return false
	}
	return true
}

func isHardConstraint(ct protocol.ConstraintType) bool {
	return ct == protocol.ConstraintHardAvoid || ct == protocol.ConstraintMeetingFree
}

func hardConstraintReason(teacherID string, day, period int, constraints []protocol.TeacherConstraint, tMap map[string]protocol.Teacher) string {
	for _, c := range constraints {
		if isHardConstraint(c.ConstraintType) && constraintMatchesSlot(c, teacherID, day, period) {
			name := c.TeacherName
			if name == "" {
				name = teacherName(teacherID, tMap)
			}
			return fmt.Sprintf("%s 선생님 %s(%s)에 걸립니다", name, constraintTypeLabel(c.ConstraintType), c.Reason)
		}
	}
	return ""
}

func softConstraintReason(teacherID string, day, period int, constraints []protocol.TeacherConstraint, tMap map[string]protocol.Teacher) string {
	for _, c := range constraints {
		if c.ConstraintType == protocol.ConstraintSoftAvoid && constraintMatchesSlot(c, teacherID, day, period) {
			name := c.TeacherName
			if name == "" {
				name = teacherName(teacherID, tMap)
			}
			return fmt.Sprintf("%s 선생님 %s", name, c.Reason)
		}
	}
	return ""
}
