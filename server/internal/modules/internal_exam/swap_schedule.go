package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// SwapSlots는 자동 배정 후 담당교사가 두 시험감독 슬롯의 교사를 검증 거쳐 수동 맞교환합니다.
func (s *Service) SwapSlots(slotID1, slotID2, actorName string) ([]protocol.ExamSlot, error) {
	if strings.TrimSpace(slotID1) == "" || strings.TrimSpace(slotID2) == "" || slotID1 == slotID2 {
		return nil, fmt.Errorf("맞교환할 서로 다른 두 감독 슬롯이 필요합니다")
	}
	var updated []protocol.ExamSlot
	err := s.store.WithTx(func(tx *database.TxStore) error {
		slots, sErr := database.LoadCollectionTx[protocol.ExamSlot](tx, "exam_slots")
		if sErr != nil {
			return sErr
		}
		teachers, tErr := database.LoadCollectionTx[protocol.Teacher](tx, "teachers")
		if tErr != nil {
			return tErr
		}
		tMap := make(map[string]protocol.Teacher, len(teachers))
		for _, t := range teachers {
			tMap[t.ID] = t
		}

		idx1, idx2 := -1, -1
		for i := range slots {
			if slots[i].ID == slotID1 {
				idx1 = i
			} else if slots[i].ID == slotID2 {
				idx2 = i
			}
		}
		if idx1 < 0 || idx2 < 0 {
			return fmt.Errorf("지정한 감독 슬롯을 찾을 수 없습니다")
		}
		s1, s2 := slots[idx1], slots[idx2]
		if s1.SlotRole == "HONORARY" || s2.SlotRole == "HONORARY" {
			return fmt.Errorf("학부모 명예감독 슬롯은 일반 교사 맞교환 대상이 아닙니다")
		}
		if s1.TeacherID == s2.TeacherID {
			return fmt.Errorf("동일 교사가 배정된 슬롯끼리는 맞교환할 수 없습니다")
		}
		t1, ok1 := tMap[s1.TeacherID]
		t2, ok2 := tMap[s2.TeacherID]
		if !ok1 || !ok2 {
			return fmt.Errorf("슬롯 교사 정보를 찾을 수 없습니다")
		}

		unavailMap := buildUnavailableByDateTx(tx, []string{s1.ExamDate, s2.ExamDate})
		unavail1 := unavailMap[strings.TrimSpace(s1.ExamDate)]
		unavail2 := unavailMap[strings.TrimSpace(s2.ExamDate)]
		// 맞교환을 가상 반영해 두 교사가 서로의 슬롯 조건(중복·담임·과목·연강절대금지·부재일)을 만족하는지 검증
		sim := make([]protocol.ExamSlot, len(slots))
		copy(sim, slots)
		sim[idx1].TeacherID, sim[idx1].TeacherName = t2.ID, t2.Name
		sim[idx2].TeacherID, sim[idx2].TeacherName = t1.ID, t1.Name

		if vErr := validateSubstituteTeacher(s1, t2, sim, unavail1); vErr != nil {
			return fmt.Errorf("1번 슬롯 교체 불가: %w", vErr)
		}
		if vErr := validateSubstituteTeacher(s2, t1, sim, unavail2); vErr != nil {
			return fmt.Errorf("2번 슬롯 교체 불가: %w", vErr)
		}

		sim[idx1].Note = fmt.Sprintf("수동 맞교환 (기존: %s)", t1.Name)
		sim[idx2].Note = fmt.Sprintf("수동 맞교환 (기존: %s)", t2.Name)
		if err := tx.SaveRecord("exam_slots", sim[idx1].ID, sim[idx1]); err != nil {
			return err
		}
		if err := tx.SaveRecord("exam_slots", sim[idx2].ID, sim[idx2]); err != nil {
			return err
		}
		updated = sim
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExamScheduleUpdated,
			Title:     "교내 시험감독 수동 맞교환 반영",
			Message:   "감독 배정표 내 두 교사의 고사장 감독이 맞교환되었습니다.",
			ActorName: actorName,
		})
	}
	sortSlotsOrder(updated)
	return updated, nil
}

// PersonalSchedule은 교사 개인의 교내 시험감독 시간표를 봉인 규칙을 준수해 반환합니다.
func (s *Service) PersonalSchedule(teacherID string, isExamAdmin bool) (protocol.PersonalExamSchedule, error) {
	slots, cfg, err := s.ListSlotsForViewer(isExamAdmin)
	if err != nil {
		return protocol.PersonalExamSchedule{}, err
	}
	teacherName := teacherID
	if t, tErr := database.LoadRecord[protocol.Teacher](s.store, "teachers", teacherID); tErr == nil && t != nil {
		teacherName = t.Name
	}
	var mySlots []protocol.ExamSlot
	for _, sl := range slots {
		if sl.TeacherID == teacherID {
			mySlots = append(mySlots, sl)
		}
	}
	sort.Slice(mySlots, func(i, j int) bool {
		if mySlots[i].ExamDate != mySlots[j].ExamDate {
			return mySlots[i].ExamDate < mySlots[j].ExamDate
		}
		return mySlots[i].Period < mySlots[j].Period
	})
	return protocol.PersonalExamSchedule{
		TeacherID:   teacherID,
		TeacherName: teacherName,
		ExamTitle:   cfg.Title,
		IsSealed:    cfg.IsSealed,
		Slots:       mySlots,
	}, nil
}
