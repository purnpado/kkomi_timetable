package timetable

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ApplyCreativeActivities는 창체(자치·동아리·진로 등) 슬롯을 학년별 또는 전학년 동시·고정 교시로 일괄 편성합니다.
func (s *Service) ApplyCreativeActivities(slots []protocol.CreativeActivitySlot, actorName string) ([]protocol.TimetableCell, error) {
	if locked, wf := s.isWorkflowLocked(); locked {
		return nil, fmt.Errorf("정규 시간표가 '%s'에 의해 최종 확정(잠금)되어 창체 고정 편성을 변경할 수 없습니다", wf.LockedBy)
	}
	cfg, err := s.GetSchoolConfig()
	if err != nil {
		return nil, err
	}
	for i := range slots {
		if slots[i].DayOfWeek < 1 || slots[i].DayOfWeek > 5 || slots[i].Period < 1 || slots[i].Period > 7 {
			return nil, fmt.Errorf("창체 슬롯(%s)의 요일·교시 범위가 올바르지 않습니다", slots[i].Title)
		}
		if strings.TrimSpace(slots[i].Title) == "" {
			slots[i].Title = "창체"
		}
		if slots[i].ID == "" {
			slots[i].ID = fmt.Sprintf("ca-%d-%d-%d", slots[i].Grade, slots[i].DayOfWeek, slots[i].Period)
		}
	}
	cfg.CreativeActivities = slots

	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	homeroomMap := make(map[string]string)
	fallbackTID := ""
	for _, t := range teachers {
		if protocol.IsSystemAdminAccount(t) || t.IsParentHonorary {
			continue
		}
		if fallbackTID == "" && protocol.HasRegularClasses(t) {
			fallbackTID = t.ID
		}
		if t.HomeroomClass != "" {
			homeroomMap[strings.TrimSpace(t.HomeroomClass)] = t.ID
		}
	}
	if fallbackTID == "" {
		return nil, errors.New("창체 담당 교사로 배정할 수 있는 활성 교사가 없습니다")
	}

	beforeCells, _ := s.ListCells()
	cellMap := make(map[string]protocol.TimetableCell, len(beforeCells))
	for _, c := range beforeCells {
		cellMap[c.ID] = c
	}

	var applied []protocol.TimetableCell
	for _, slot := range slots {
		grades := []int{slot.Grade}
		if slot.Grade <= 0 {
			grades = []int{1, 2, 3}
		}
		for _, g := range grades {
			cnt := cfg.GradeClassCounts[g]
			for cIdx := 1; cIdx <= cnt; cIdx++ {
				cls := fmt.Sprintf("%d-%d", g, cIdx)
				tid := homeroomMap[cls]
				if tid == "" {
					tid = fallbackTID
				}
				cid := fmt.Sprintf("c-%d-%d-%s", slot.DayOfWeek, slot.Period, cls)
				elGroup := ""
				if slot.Simultaneous {
					elGroup = fmt.Sprintf("창체-G%d-%d-%d", slot.Grade, slot.DayOfWeek, slot.Period)
				}
				cell := protocol.TimetableCell{
					ID:            cid,
					DayOfWeek:     slot.DayOfWeek,
					Period:        slot.Period,
					ClassName:     cls,
					Grade:         g,
					Subject:       strings.TrimSpace(slot.Title),
					SubjectCode:   "창체",
					TeacherID:     tid,
					Room:          cls + "교실",
					ElectiveGroup: elGroup,
					IsLocked:      slot.Locked,
				}
				cellMap[cid] = cell
				applied = append(applied, cell)
			}
		}
	}

	afterCells := make([]protocol.TimetableCell, 0, len(cellMap))
	for _, c := range cellMap {
		afterCells = append(afterCells, c)
	}
	if err := s.store.WithTx(func(tx *database.TxStore) error {
		if hErr := recordHistoryTx(tx, "창체 동시·고정 편성 적용", actorName, beforeCells, afterCells); hErr != nil {
			return hErr
		}
		if sErr := tx.SaveRecord("school_configs", cfg.ID, cfg); sErr != nil {
			return sErr
		}
		for _, c := range applied {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableCellsUpdated,
			Title:     "창체 동시·고정 교시 편성 반영",
			Message:   fmt.Sprintf("창체 고정 슬롯 %d종(%d개 학급 셀)이 반영되었습니다.", len(slots), len(applied)),
			ActorName: actorName,
		})
	}
	return applied, nil
}

// GetWorkflowState는 현재 시간표 편성 단계(취합 중 → 가편성 검토 → 최종 확정 잠금) 상태를 반환합니다.
func (s *Service) GetWorkflowState() (protocol.TimetableWorkflowState, error) {
	loaded, err := database.LoadRecord[protocol.TimetableWorkflowState](s.store, "timetable_workflow", "default")
	if err == nil && loaded != nil && loaded.Phase != "" {
		return *loaded, nil
	}
	return protocol.TimetableWorkflowState{
		ID:    "default",
		Phase: protocol.WorkflowCollecting,
	}, nil
}

func (s *Service) isWorkflowLocked() (bool, protocol.TimetableWorkflowState) {
	wf, err := s.GetWorkflowState()
	if err == nil && wf.Phase == protocol.WorkflowLocked {
		return true, wf
	}
	return false, wf
}

func isWorkflowLockedTx(tx *database.TxStore) (bool, protocol.TimetableWorkflowState) {
	loaded, err := database.LoadRecordTx[protocol.TimetableWorkflowState](tx, "timetable_workflow", "default")
	if err == nil && loaded != nil && loaded.Phase == protocol.WorkflowLocked {
		return true, *loaded
	}
	return false, protocol.TimetableWorkflowState{}
}

// SetWorkflowPhase는 시간표 편성 단계를 전환(취합 → 가편성 검토 → 최종 확정 잠금/해제)합니다.
func (s *Service) SetWorkflowPhase(phase, actorName, note string) (protocol.TimetableWorkflowState, error) {
	phase = strings.ToUpper(strings.TrimSpace(phase))
	if phase != protocol.WorkflowCollecting && phase != protocol.WorkflowDraftReview && phase != protocol.WorkflowLocked {
		return protocol.TimetableWorkflowState{}, errors.New("유효하지 않은 편성 단계입니다 (COLLECTING, DRAFT_REVIEW, LOCKED)")
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	wf := protocol.TimetableWorkflowState{
		ID:        "default",
		Phase:     phase,
		UpdatedBy: actorName,
		UpdatedAt: now,
		Note:      strings.TrimSpace(note),
	}
	if phase == protocol.WorkflowLocked {
		wf.LockedBy = actorName
		wf.LockedAt = now
	}
	if err := s.store.SaveRecord("timetable_workflow", "default", wf); err != nil {
		return wf, err
	}
	return wf, nil
}

// ListDeptSubmissions는 교과별 배당 시수 제출 내역을 최신순으로 반환합니다.
func (s *Service) ListDeptSubmissions() ([]protocol.DeptLoadSubmission, error) {
	list, err := database.LoadCollection[protocol.DeptLoadSubmission](s.store, "dept_load_submissions")
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].SubmittedAt > list[j].SubmittedAt })
	return list, nil
}

// SubmitDeptLoads는 교과부장(또는 담당교사)이 해당 교과의 시수표 작성을 완료해 취합 제출합니다.
func (s *Service) SubmitDeptLoads(sub protocol.DeptLoadSubmission) (protocol.DeptLoadSubmission, error) {
	sub.Department = strings.TrimSpace(sub.Department)
	if sub.Department == "" {
		return sub, errors.New("교과(부서)명은 필수입니다")
	}
	if sub.ID == "" {
		sub.ID = fmt.Sprintf("dsub-%d", time.Now().UnixNano())
	}
	if sub.LoadCount == 0 && sub.TotalHours == 0 {
		loads, _ := s.ListTeachingLoads()
		for _, ld := range loads {
			if strings.Contains(ld.Subject, sub.Department) || sub.Department == "전체교과" {
				sub.LoadCount++
				sub.TotalHours += ld.TotalHours
			}
		}
	}
	sub.Status = "SUBMITTED"
	sub.SubmittedAt = time.Now().Format("2006-01-02 15:04:05")
	if err := s.store.SaveRecord("dept_load_submissions", sub.ID, sub); err != nil {
		return sub, err
	}
	return sub, nil
}

// ReviewDeptSubmission은 일과계가 교과별 시수 제출 내역을 승인(취합 완료)하거나 보완 요청합니다.
func (s *Service) ReviewDeptSubmission(id, status, reviewerName, reviewNote string) (protocol.DeptLoadSubmission, error) {
	loaded, err := database.LoadRecord[protocol.DeptLoadSubmission](s.store, "dept_load_submissions", id)
	if err != nil || loaded == nil {
		return protocol.DeptLoadSubmission{}, fmt.Errorf("교과 시수 제출 내역(%s)을 찾을 수 없습니다", id)
	}
	st := strings.ToUpper(strings.TrimSpace(status))
	if st != "APPROVED" && st != "REVISION_REQUESTED" {
		return *loaded, errors.New("검토 상태는 APPROVED 또는 REVISION_REQUESTED 여야 합니다")
	}
	loaded.Status = st
	loaded.ReviewedBy = reviewerName
	loaded.ReviewedAt = time.Now().Format("2006-01-02 15:04:05")
	loaded.ReviewNote = strings.TrimSpace(reviewNote)
	if err := s.store.SaveRecord("dept_load_submissions", loaded.ID, *loaded); err != nil {
		return *loaded, err
	}
	return *loaded, nil
}

// ListDraftFeedbacks는 가편성 시간표에 대한 교사 피드백·건의 목록을 반환합니다.
func (s *Service) ListDraftFeedbacks() ([]protocol.DraftFeedback, error) {
	list, err := database.LoadCollection[protocol.DraftFeedback](s.store, "draft_feedbacks")
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
	return list, nil
}

// SubmitDraftFeedback은 교사가 가편성 기간 중 시간표 수정 요청 의견을 제출합니다.
func (s *Service) SubmitDraftFeedback(fb protocol.DraftFeedback) (protocol.DraftFeedback, error) {
	fb.Message = strings.TrimSpace(fb.Message)
	if fb.Message == "" {
		return fb, errors.New("피드백 내용을 입력해 주세요")
	}
	if fb.ID == "" {
		fb.ID = fmt.Sprintf("dfb-%d", time.Now().UnixNano())
	}
	if fb.Category == "" {
		fb.Category = "기타"
	}
	fb.Status = "OPEN"
	fb.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	if err := s.store.SaveRecord("draft_feedbacks", fb.ID, fb); err != nil {
		return fb, err
	}
	return fb, nil
}

// ResolveDraftFeedback은 일과계가 가편성 피드백을 반영 완료 처리합니다.
func (s *Service) ResolveDraftFeedback(id, resolverName, resolution string) (protocol.DraftFeedback, error) {
	loaded, err := database.LoadRecord[protocol.DraftFeedback](s.store, "draft_feedbacks", id)
	if err != nil || loaded == nil {
		return protocol.DraftFeedback{}, fmt.Errorf("가편성 피드백(%s)을 찾을 수 없습니다", id)
	}
	loaded.Status = "RESOLVED"
	loaded.Resolution = strings.TrimSpace(resolution)
	loaded.ResolvedBy = resolverName
	loaded.ResolvedAt = time.Now().Format("2006-01-02 15:04:05")
	if err := s.store.SaveRecord("draft_feedbacks", loaded.ID, *loaded); err != nil {
		return *loaded, err
	}
	return *loaded, nil
}
