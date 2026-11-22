package timetable

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 정규·일일 시간표 조회, 수업 교환/결보강 대기열(STAGING),
// 담당교사 최종 선택(Commit) 반영 및 나이스(NEIS) 입력 도우미를 담당합니다.
type Service struct {
	store *database.Store
	bus   *eventbus.Bus
}

// NewService는 시간표 모듈을 초기화하고 타 부서 도메인 이벤트 구독을 연결합니다.
func NewService(store *database.Store, bus *eventbus.Bus) *Service {
	svc := &Service{store: store, bus: bus}
	if bus != nil {
		// 외부 시험감독 파견 확정 시 평일이면 결보강 대기열 동기화 (제외된 교사 정리 포함)
		bus.Subscribe(protocol.EventExternalExamConfirmed, svc.onExternalExamConfirmed)
		// 외부 시험 공고 삭제/변경 시 미승인 결보강 대기열 정리
		bus.Subscribe(protocol.EventExternalExamUpdated, svc.onExternalExamUpdated)
		// 학교 행사 확정 시 인솔교사 결보강 대기열 자동 연동 (단일 일자 수업블록은 주차 조회에서 동적 합성)
		bus.Subscribe(protocol.EventSchoolEventPublished, svc.onSchoolEventPublished)
		// 학교 행사 삭제 시 연동된 미승인 결보강 대기열 자동 정리
		bus.Subscribe(protocol.EventSchoolEventDeleted, svc.onSchoolEventDeleted)
	}
	return svc
}

// ListCells는 전체 정규/일일 시간표 셀 목록을 반환합니다.
func (s *Service) ListCells() ([]protocol.TimetableCell, error) {
	return database.LoadCollection[protocol.TimetableCell](s.store, "timetable_cells")
}

// ListRequests는 결보강 및 수업 교환 요청 목록을 최신순으로 반환합니다.
func (s *Service) ListRequests() ([]protocol.TimetableChangeRequest, error) {
	reqs, err := database.LoadCollection[protocol.TimetableChangeRequest](s.store, "timetable_requests")
	if err != nil {
		return nil, err
	}
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].CreatedAt > reqs[j].CreatedAt
	})
	return reqs, nil
}

// EvaluateMove는 Canvas 드래그 앤 드롭 신호등 판정을 수행합니다.
func (s *Service) EvaluateMove(req protocol.EvaluateMoveRequest) (protocol.MoveEvaluationResult, error) {
	var (
		cells []protocol.TimetableCell
		err   error
	)
	if strings.TrimSpace(req.TargetDate) != "" {
		cells, err = s.ListWeekCells(NormalizeMonday(req.TargetDate))
	} else {
		cells, err = s.ListCells()
	}
	if err != nil {
		return protocol.MoveEvaluationResult{}, err
	}
	teachersList, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return protocol.MoveEvaluationResult{}, err
	}
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")
	constraints, _ := s.ListConstraints()
	cfg, _ := s.GetSchoolConfig()
	return EvaluateMoveWithConfig(req, cells, tMap, events, constraints, cfg), nil
}

// SubmitRequest는 교사의 수업 교환 또는 결보강 신청을 대기열(STAGING)에 등록합니다.
// 마스터 DB 시간표는 담당교사가 CommitRequest(최종 선택)를 실행할 때까지 변경되지 않습니다.
func (s *Service) SubmitRequest(req protocol.TimetableChangeRequest) (protocol.TimetableChangeRequest, error) {
	if req.ID == "" {
		req.ID = fmt.Sprintf("treq-%d", time.Now().UnixNano())
	}
	if req.TargetDate == "" {
		req.TargetDate = time.Now().Format("2006-01-02")
	}
	if req.Scope == "DATE_ONLY" && req.SourceDate == "" {
		req.SourceDate = req.TargetDate
	}
	req.Status = protocol.StatusStaging
	if req.CreatedAt == "" {
		req.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if req.PartnerAgreed && req.PartnerAgreedAt == "" {
		req.PartnerAgreedAt = req.CreatedAt
	}
	if err := s.store.SaveRecord("timetable_requests", req.ID, req); err != nil {
		return req, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableRequested,
			Title:     fmt.Sprintf("[%s] 수업 변경 승인 대기 등록", req.RequesterName),
			Message:   fmt.Sprintf("%s %s반 %d교시 (%s)", req.TargetDate, req.SourceCell.ClassName, req.SourceCell.Period, req.Reason),
			ActorID:   req.RequesterID,
			ActorName: req.RequesterName,
			Payload:   req,
		})
	}
	return req, nil
}

// AgreeRequest는 수업 교환·보강 대상(상대) 교사가 사전 합의 동의 여부와 메모를 기록합니다.
func (s *Service) AgreeRequest(requestID, actorID, actorName string, agreed bool, note string, isAdmin bool) (*protocol.TimetableChangeRequest, error) {
	var updated *protocol.TimetableChangeRequest
	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.TimetableChangeRequest](tx, "timetable_requests", requestID)
		if lErr != nil || loaded == nil {
			return fmt.Errorf("요청(%s)을 찾을 수 없습니다", requestID)
		}
		if loaded.Status != protocol.StatusStaging {
			return fmt.Errorf("승인 대기 상태인 요청만 동의 상태를 변경할 수 있습니다")
		}
		if !isAdmin && loaded.PartnerID != "" && actorID != loaded.PartnerID {
			return fmt.Errorf("지정된 상대 교사(%s) 또는 관리자만 합의 동의 처리를 할 수 있습니다", loaded.PartnerName)
		}
		loaded.PartnerAgreed = agreed
		loaded.AgreementNote = strings.TrimSpace(note)
		if agreed {
			loaded.PartnerAgreedAt = time.Now().Format("2006-01-02 15:04:05")
			if loaded.AgreementNote == "" && actorName != "" {
				loaded.AgreementNote = fmt.Sprintf("%s 사전 합의 완료", actorName)
			}
		} else {
			loaded.PartnerAgreedAt = ""
		}
		if sErr := tx.SaveRecord("timetable_requests", loaded.ID, *loaded); sErr != nil {
			return sErr
		}
		updated = loaded
		return nil
	})
	return updated, err
}

// CommitRequest는 시간표 담당교사가 대기열 항목을 [최종 선택(서버 반영)]할 때 원자적 트랜잭션으로 마스터 시간표를 갱신합니다.
func (s *Service) CommitRequest(requestID, reviewerName string, approve bool, rejectReason string) (*protocol.TimetableChangeRequest, error) {
	var target *protocol.TimetableChangeRequest
	cellsChanged := false

	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.TimetableChangeRequest](tx, "timetable_requests", requestID)
		if lErr != nil || loaded == nil {
			return fmt.Errorf("요청(%s)을 찾을 수 없습니다", requestID)
		}
		if loaded.Status != protocol.StatusStaging {
			return fmt.Errorf("이미 처리된 요청입니다 (현재 상태: %s)", loaded.Status)
		}
		loaded.ReviewedBy = reviewerName
		loaded.CommittedAt = time.Now().Format("2006-01-02 15:04:05")

		if !approve {
			loaded.Status = protocol.StatusRejected
			loaded.RejectReason = rejectReason
			if sErr := tx.SaveRecord("timetable_requests", loaded.ID, *loaded); sErr != nil {
				return sErr
			}
			target = loaded
			return nil
		}

		if loaded.RequestType != "CROSS_WEEK_SWAP" && loaded.Scope != "DATE_ONLY" {
			if locked, wf := isWorkflowLockedTx(tx); locked {
				return fmt.Errorf("정규 시간표가 '%s'에 의해 최종 확정(잠금)되어 영구 수정할 수 없습니다. 일회성(주차 한정) 변경을 이용해 주세요", wf.LockedBy)
			}
			cells, cErr := database.LoadCollectionTx[protocol.TimetableCell](tx, "timetable_cells")
			if cErr != nil {
				return cErr
			}
			teachers, tErr := database.LoadCollectionTx[protocol.Teacher](tx, "teachers")
			if tErr != nil {
				return tErr
			}
			tMap := make(map[string]protocol.Teacher, len(teachers))
			for _, t := range teachers {
				tMap[t.ID] = t
			}
			constraints, _ := database.LoadCollectionTx[protocol.TeacherConstraint](tx, "teacher_constraints")
			cfg := loadSchoolConfigTx(tx)
			changed, uErr := validateAndApplyCellUpdateTx(tx, cells, tMap, constraints, cfg, *loaded, reviewerName)
			if uErr != nil {
				return uErr
			}
			cellsChanged = changed
		}

		if loaded.PartnerID != "" && isSubstitutionType(loaded.RequestType) {
			if pt, pErr := database.LoadRecordTx[protocol.Teacher](tx, "teachers", loaded.PartnerID); pErr == nil && pt != nil {
				pt.SubstitutionCount++
				if sErr := tx.SaveRecord("teachers", pt.ID, *pt); sErr != nil {
					return sErr
				}
			}
		}

		loaded.Status = protocol.StatusApproved
		if sErr := tx.SaveRecord("timetable_requests", loaded.ID, *loaded); sErr != nil {
			return sErr
		}
		target = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}

	if !approve {
		if s.bus != nil {
			s.bus.Publish(protocol.DomainEvent{
				Type:      protocol.EventTimetableRejected,
				Title:     "수업 변경 신청 반려",
				Message:   fmt.Sprintf("%s반 %d교시 요청이 반려되었습니다. (사유: %s)", target.SourceCell.ClassName, target.SourceCell.Period, rejectReason),
				TargetIDs: compactIDs(target.RequesterID, target.PartnerID),
				ActorName: reviewerName,
				Payload:   *target,
			})
		}
		return target, nil
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableApproved,
			Title:     fmt.Sprintf("[%s반 %d교시] 시간표 변경 확정 반영", target.SourceCell.ClassName, target.SourceCell.Period),
			Message:   fmt.Sprintf("%s(%s) → %s(%s) 변경이 반영되었습니다.", target.SourceCell.Subject, target.RequesterName, target.TargetCell.Subject, target.PartnerName),
			TargetIDs: compactIDs(target.RequesterID, target.PartnerID),
			ActorName: reviewerName,
			Payload:   *target,
		})
		if cellsChanged {
			s.bus.Publish(protocol.DomainEvent{
				Type:      protocol.EventTimetableCellsUpdated,
				Title:     "정규 시간표 변경 반영",
				Message:   "승인된 시간표 변경이 정규 시간표에 반영되었습니다.",
				ActorName: reviewerName,
				Payload:   map[string]string{"requestId": target.ID},
			})
		}
	}
	return target, nil
}

func isSubstitutionType(reqType string) bool {
	return reqType == "SUBSTITUTE" || reqType == "ABSENCE_SUBSTITUTE" || reqType == "EXTERNAL_TRIP" || reqType == "EVENT_TRIP"
}

func validateAndApplyCellUpdateTx(
	tx *database.TxStore,
	cells []protocol.TimetableCell,
	tMap map[string]protocol.Teacher,
	constraints []protocol.TeacherConstraint,
	cfg protocol.SchoolConfig,
	req protocol.TimetableChangeRequest,
	actorName string,
) (bool, error) {
	sim := make([]protocol.TimetableCell, len(cells))
	copy(sim, cells)
	indexByID := make(map[string]int, len(sim))
	for i, c := range sim {
		indexByID[c.ID] = i
	}

	var touchedIDs []string
	markTouched := func(id string) {
		for _, existing := range touchedIDs {
			if existing == id {
				return
			}
		}
		touchedIDs = append(touchedIDs, id)
	}

	switch {
	case len(req.ChainSteps) > 0:
		for _, step := range req.ChainSteps {
			idx, ok := indexByID[step.CellID]
			if !ok {
				return false, fmt.Errorf("연쇄 교환 대상 셀(%s)을 찾을 수 없습니다", step.CellID)
			}
			if sim[idx].IsLocked {
				return false, fmt.Errorf("이동금지(고정) 수업(%s)은 변경할 수 없습니다", sim[idx].ClassName)
			}
			sim[idx].DayOfWeek = step.ToDay
			sim[idx].Period = step.ToPeriod
			markTouched(sim[idx].ID)
		}
	case req.RequestType == "SWAP" && req.TargetCell.ID != "":
		srcIdx, okS := indexByID[req.SourceCell.ID]
		tgtIdx, okT := indexByID[req.TargetCell.ID]
		if !okS || !okT {
			return false, fmt.Errorf("맞교환 대상 셀을 찾을 수 없습니다")
		}
		if sim[srcIdx].IsLocked || sim[tgtIdx].IsLocked {
			return false, fmt.Errorf("이동금지(고정) 수업은 맞교환할 수 없습니다")
		}
		sim[srcIdx].DayOfWeek, sim[tgtIdx].DayOfWeek = sim[tgtIdx].DayOfWeek, sim[srcIdx].DayOfWeek
		sim[srcIdx].Period, sim[tgtIdx].Period = sim[tgtIdx].Period, sim[srcIdx].Period
		markTouched(sim[srcIdx].ID)
		markTouched(sim[tgtIdx].ID)
	case req.SourceCell.ID != "" && req.TargetCell.ID == "" && req.TargetCell.DayOfWeek > 0 && req.TargetCell.Period > 0 && req.RequestType == "MOVE":
		srcIdx, ok := indexByID[req.SourceCell.ID]
		if !ok {
			return false, fmt.Errorf("이동 대상 셀(%s)을 찾을 수 없습니다", req.SourceCell.ID)
		}
		if sim[srcIdx].IsLocked {
			return false, fmt.Errorf("이동금지(고정) 수업은 이동할 수 없습니다")
		}
		sim[srcIdx].DayOfWeek = req.TargetCell.DayOfWeek
		sim[srcIdx].Period = req.TargetCell.Period
		markTouched(sim[srcIdx].ID)
	case req.SourceCell.ID != "" && req.PartnerID != "":
		srcIdx, ok := indexByID[req.SourceCell.ID]
		if !ok {
			return false, fmt.Errorf("대타 대상 셀(%s)을 찾을 수 없습니다", req.SourceCell.ID)
		}
		replaceSlotTeacher(&sim[srcIdx], req.RequesterID, req.PartnerID)
		if req.TargetCell.Subject != "" {
			sim[srcIdx].Subject = req.TargetCell.Subject
		}
		markTouched(sim[srcIdx].ID)
	}

	if len(touchedIDs) == 0 {
		return false, nil
	}
	for _, tid := range touchedIDs {
		idx := indexByID[tid]
		c := sim[idx]
		if reason := slotConflictReasonWithConfig(c, c.DayOfWeek, c.Period, "", map[string]bool{c.ID: true}, sim, tMap, nil, constraints, cfg); reason != "" {
			return false, fmt.Errorf("승인 시점 시간표 충돌: %s", reason)
		}
		for _, other := range sim {
			if other.ID != c.ID && other.ClassName == c.ClassName && other.DayOfWeek == c.DayOfWeek && other.Period == c.Period {
				return false, fmt.Errorf("승인 시점 충돌: %s반 %d교시에 이미 %s 수업이 있습니다", c.ClassName, c.Period, other.Subject)
			}
		}
	}
	if hErr := recordHistoryTx(tx, fmt.Sprintf("수업 변경 확정 (%s)", req.SourceCell.ClassName), actorName, cells, sim); hErr != nil {
		return false, hErr
	}
	for _, tid := range touchedIDs {
		c := sim[indexByID[tid]]
		if err := tx.SaveRecord("timetable_cells", c.ID, c); err != nil {
			return false, err
		}
	}
	return true, nil
}

// NEISHelperList는 확정(APPROVED)된 일일 변경 내역을 나이스(NEIS) 입력 순서대로 정렬해 반환합니다.
func (s *Service) NEISHelperList() ([]protocol.TimetableChangeRequest, error) {
	reqs, err := s.ListRequests()
	if err != nil {
		return nil, err
	}
	var approved []protocol.TimetableChangeRequest
	for _, r := range reqs {
		if r.Status == protocol.StatusApproved {
			approved = append(approved, r)
		}
	}
	sort.Slice(approved, func(i, j int) bool {
		if approved[i].TargetDate != approved[j].TargetDate {
			return approved[i].TargetDate < approved[j].TargetDate
		}
		if approved[i].SourceCell.ClassName != approved[j].SourceCell.ClassName {
			return approved[i].SourceCell.ClassName < approved[j].SourceCell.ClassName
		}
		return approved[i].SourceCell.Period < approved[j].SourceCell.Period
	})
	return approved, nil
}
