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
	return EvaluateMove(req, cells, tMap, events), nil
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
	req.Status = protocol.StatusStaging
	if req.CreatedAt == "" {
		req.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
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
			changed, uErr := validateAndApplyCellUpdateTx(tx, cells, tMap, *loaded)
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
	req protocol.TimetableChangeRequest,
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
		sim[srcIdx].TeacherID = req.PartnerID
		if sim[srcIdx].CoTeacherID == req.PartnerID {
			sim[srcIdx].CoTeacherID = ""
		}
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
		if reason := slotConflictReason(c, c.DayOfWeek, c.Period, "", map[string]bool{c.ID: true}, sim, tMap, nil); reason != "" {
			return false, fmt.Errorf("승인 시점 시간표 충돌: %s", reason)
		}
		for _, other := range sim {
			if other.ID != c.ID && other.ClassName == c.ClassName && other.DayOfWeek == c.DayOfWeek && other.Period == c.Period {
				return false, fmt.Errorf("승인 시점 충돌: %s반 %d교시에 이미 %s 수업이 있습니다", c.ClassName, c.Period, other.Subject)
			}
		}
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

func (s *Service) onExternalExamConfirmed(evt protocol.DomainEvent) {
	notice, ok := evt.Payload.(protocol.ExternalExamNotice)
	if !ok {
		return
	}
	prefix := fmt.Sprintf("ext-trip-%s-", notice.ID)
	existingReqs, _ := s.ListRequests()

	desired := make(map[string]protocol.TimetableChangeRequest)
	if parsedDate, err := time.Parse("2006-01-02", notice.ExamDate); err == nil {
		wd := int(parsedDate.Weekday()) // 0=일, 1=월 ... 5=금, 6=토
		if wd >= 1 && wd <= 5 && len(notice.ConfirmedIDs) > 0 {
			cells, _ := s.ListCells()
			teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
			tNameMap := make(map[string]string, len(teachers))
			for _, t := range teachers {
				tNameMap[t.ID] = t.Name
			}
			nowStr := time.Now().Format("2006-01-02 15:04:05")
			for _, tid := range notice.ConfirmedIDs {
				for _, c := range cells {
					if c.DayOfWeek == wd && (c.TeacherID == tid || c.CoTeacherID == tid) {
						id := fmt.Sprintf("%s%s-%s", prefix, tid, c.ID)
						desired[id] = protocol.TimetableChangeRequest{
							ID: id, RequestType: "EXTERNAL_TRIP", Scope: "DATE_ONLY",
							SourceDate: notice.ExamDate, TargetDate: notice.ExamDate,
							RequesterID: tid, RequesterName: tNameMap[tid],
							PartnerName: "보강교사 미배정", SourceCell: c, TargetCell: c,
							Reason:    fmt.Sprintf("[외부감독 자동이관] %s 파견 출장", notice.Title),
							Status:    protocol.StatusStaging,
							CreatedAt: nowStr,
						}
					}
				}
			}
		}
	}

	for _, rq := range existingReqs {
		if !strings.HasPrefix(rq.ID, prefix) {
			continue
		}
		if _, keep := desired[rq.ID]; !keep {
			if rq.Status == protocol.StatusStaging {
				_ = s.store.DeleteRecord("timetable_requests", rq.ID)
			}
		} else if rq.Status == protocol.StatusApproved {
			delete(desired, rq.ID)
		}
	}
	for _, rq := range desired {
		_ = s.store.SaveRecord("timetable_requests", rq.ID, rq)
	}
}

func (s *Service) onExternalExamUpdated(evt protocol.DomainEvent) {
	info, ok := evt.Payload.(map[string]string)
	if !ok {
		return
	}
	deletedID := strings.TrimSpace(info["deletedId"])
	if deletedID == "" {
		return
	}
	prefix := fmt.Sprintf("ext-trip-%s-", deletedID)
	reqs, _ := s.ListRequests()
	for _, rq := range reqs {
		if strings.HasPrefix(rq.ID, prefix) && rq.Status == protocol.StatusStaging {
			_ = s.store.DeleteRecord("timetable_requests", rq.ID)
		}
	}
}

func (s *Service) onSchoolEventPublished(evt protocol.DomainEvent) {
	ev, ok := evt.Payload.(protocol.SchoolEvent)
	if !ok || ev.Status != protocol.StatusApproved {
		return
	}
	// [학년/전교 수업블록]은 정규 timetable_cells를 오염시키지 않고 해당 주차 조회(buildEffectiveWeekCells)에서 동적 합성
	if ev.LinkType != protocol.EventLinkTeacherTrip {
		return
	}
	parsedDate, err := time.Parse("2006-01-02", ev.EventDate)
	if err != nil {
		return
	}
	wd := int(parsedDate.Weekday())
	if wd < 1 || wd > 5 {
		return
	}
	cells, _ := s.ListCells()
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tNameMap := make(map[string]string, len(teachers))
	for _, t := range teachers {
		tNameMap[t.ID] = t.Name
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05")
	for _, tid := range ev.LeadTeachers {
		for _, c := range cells {
			if c.DayOfWeek == wd && (c.TeacherID == tid || c.CoTeacherID == tid) &&
				(ev.StartPeriod == 0 || (c.Period >= ev.StartPeriod && c.Period <= ev.EndPeriod)) {
				req := protocol.TimetableChangeRequest{
					ID: fmt.Sprintf("evt-trip-%s-%s-%s", ev.ID, tid, c.ID),
					RequestType: "EVENT_TRIP", Scope: "DATE_ONLY",
					SourceDate: ev.EventDate, TargetDate: ev.EventDate,
					RequesterID: tid, RequesterName: tNameMap[tid],
					PartnerName: "보강교사 미배정", SourceCell: c, TargetCell: c,
					Reason:    fmt.Sprintf("[행사 자동이관] %s 인솔 출장", ev.Title),
					Status:    protocol.StatusStaging,
					CreatedAt: nowStr,
				}
				_ = s.store.SaveRecord("timetable_requests", req.ID, req)
			}
		}
	}
}

func (s *Service) onSchoolEventDeleted(evt protocol.DomainEvent) {
	ev, ok := evt.Payload.(protocol.SchoolEvent)
	if !ok || strings.TrimSpace(ev.ID) == "" {
		return
	}
	prefix := fmt.Sprintf("evt-trip-%s-", ev.ID)
	reqs, _ := s.ListRequests()
	for _, rq := range reqs {
		if strings.HasPrefix(rq.ID, prefix) && rq.Status == protocol.StatusStaging {
			_ = s.store.DeleteRecord("timetable_requests", rq.ID)
		}
	}
}

func compactIDs(ids ...string) []string {
	var out []string
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			out = append(out, id)
		}
	}
	return out
}
