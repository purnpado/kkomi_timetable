package internal_exam

import (
	"fmt"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// TeacherExamPolicyUpdate는 교사별 교내 시험감독 제외(특수반·보건실 등) 및
// 연간 누적 감독횟수 설정/초기화 요청 구조체입니다.
type TeacherExamPolicyUpdate struct {
	TeacherID                string `json:"teacherId"`
	InternalExamExempt       bool   `json:"internalExamExempt"`
	InternalExamExemptReason string `json:"internalExamExemptReason"`
	AnnualMainCount          int    `json:"annualMainCount"`
	AnnualSubCount           int    `json:"annualSubCount"`
	AnnualCorridorCount      int    `json:"annualCorridorCount"`
	ResetAllBaseCounts       bool   `json:"resetAllBaseCounts"`
	CommitCurrentToBase      bool   `json:"commitCurrentToBase"`
}

// UpdateTeacherExamPolicy는 특수반·보건실 제외 설정, 기실시 연간 시수 수정, 전체 0회 초기화
// 또는 이번 시험 배정의 연간 누적 확정 반영을 수행합니다.
func (s *Service) UpdateTeacherExamPolicy(req TeacherExamPolicyUpdate) ([]protocol.Teacher, error) {
	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return nil, err
	}
	if req.ResetAllBaseCounts {
		err := s.store.WithTx(func(tx *database.TxStore) error {
			txTeachers, tErr := database.LoadCollectionTx[protocol.Teacher](tx, "teachers")
			if tErr != nil {
				return tErr
			}
			for i := range txTeachers {
				txTeachers[i].AnnualMainCount = 0
				txTeachers[i].AnnualSubCount = 0
				txTeachers[i].AnnualCorridorCount = 0
				txTeachers[i].InternalExamCount = 0
				if sErr := tx.SaveRecord("teachers", txTeachers[i].ID, txTeachers[i]); sErr != nil {
					return sErr
				}
			}
			cfgs, _ := database.LoadCollectionTx[protocol.ExamConfig](tx, "exam_configs")
			if len(cfgs) > 0 && cfgs[0].CountsCommitted {
				cfgs[0].CountsCommitted = false
				_ = tx.SaveRecord("exam_configs", cfgs[0].ID, cfgs[0])
			}
			teachers = txTeachers
			return nil
		})
		if err != nil {
			return nil, err
		}
		return teachers, nil
	}
	if req.CommitCurrentToBase {
		err := s.store.WithTx(func(tx *database.TxStore) error {
			cfgs, _ := database.LoadCollectionTx[protocol.ExamConfig](tx, "exam_configs")
			if len(cfgs) > 0 && cfgs[0].CountsCommitted {
				return fmt.Errorf("이번 시험(%s)의 감독 시수는 이미 연간 누적에 확정 반영되었습니다", cfgs[0].Title)
			}
			slots, sErr := database.LoadCollectionTx[protocol.ExamSlot](tx, "exam_slots")
			if sErr != nil {
				return sErr
			}
			if len(slots) == 0 {
				return fmt.Errorf("연간 누적에 반영할 감독 배정 슬롯이 없습니다")
			}
			curMap := make(map[string]*roleLoad)
			for _, sl := range slots {
				if curMap[sl.TeacherID] == nil {
					curMap[sl.TeacherID] = &roleLoad{}
				}
				curMap[sl.TeacherID].add(sl.SlotRole, 1)
			}
			txTeachers, tErr := database.LoadCollectionTx[protocol.Teacher](tx, "teachers")
			if tErr != nil {
				return tErr
			}
			for i := range txTeachers {
				if cur := curMap[txTeachers[i].ID]; cur != nil {
					bm, bs, bc, _ := EffectiveBaseCounts(txTeachers[i])
					txTeachers[i].AnnualMainCount = bm + cur.Main
					txTeachers[i].AnnualSubCount = bs + cur.Sub
					txTeachers[i].AnnualCorridorCount = bc + cur.Corridor
					txTeachers[i].InternalExamCount = txTeachers[i].AnnualMainCount + txTeachers[i].AnnualSubCount + txTeachers[i].AnnualCorridorCount
					if saveErr := tx.SaveRecord("teachers", txTeachers[i].ID, txTeachers[i]); saveErr != nil {
						return saveErr
					}
				}
			}
			if len(cfgs) > 0 {
				cfgs[0].CountsCommitted = true
				if saveErr := tx.SaveRecord("exam_configs", cfgs[0].ID, cfgs[0]); saveErr != nil {
					return saveErr
				}
			}
			teachers = txTeachers
			return nil
		})
		if err != nil {
			return nil, err
		}
		if s.bus != nil {
			s.bus.Publish(protocol.DomainEvent{
				Type:    protocol.EventExamScheduleUpdated,
				Title:   "교내 시험감독 연간 누적 시수 반영 완료",
				Message: "이번 시험 감독 시수가 전 교원 연간 누적 횟수에 확정 합산되었습니다.",
			})
		}
		return teachers, nil
	}
	for i := range teachers {
		if teachers[i].ID == req.TeacherID {
			teachers[i].InternalExamExempt = req.InternalExamExempt
			teachers[i].InternalExamExemptReason = strings.TrimSpace(req.InternalExamExemptReason)
			teachers[i].AnnualMainCount = max(0, req.AnnualMainCount)
			teachers[i].AnnualSubCount = max(0, req.AnnualSubCount)
			teachers[i].AnnualCorridorCount = max(0, req.AnnualCorridorCount)
			teachers[i].InternalExamCount = teachers[i].AnnualMainCount + teachers[i].AnnualSubCount + teachers[i].AnnualCorridorCount
			if err := s.store.SaveRecord("teachers", teachers[i].ID, teachers[i]); err != nil {
				return nil, err
			}
			return teachers, nil
		}
	}
	return nil, fmt.Errorf("대상 교사(%s)를 찾을 수 없습니다", req.TeacherID)
}
