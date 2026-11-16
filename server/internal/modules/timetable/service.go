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
		// 외부 시험감독 파견 확정 시 평일이면 시간표 담당교사 결보강 대기열로 자동 이관
		bus.Subscribe(protocol.EventExternalExamConfirmed, svc.onExternalExamConfirmed)
		// 학교 행사 확정 시 수업 블록 또는 인솔교사 결보강 대기열 자동 연동
		bus.Subscribe(protocol.EventSchoolEventPublished, svc.onSchoolEventPublished)
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
	cells, err := s.ListCells()
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

// CommitRequest는 시간표 담당교사가 대기열 항목을 [최종 선택(서버 반영)]할 때 마스터 시간표를 갱신합니다.
func (s *Service) CommitRequest(requestID, reviewerName string, approve bool, rejectReason string) (*protocol.TimetableChangeRequest, error) {
	reqs, err := s.ListRequests()
	if err != nil {
		return nil, err
	}
	var target *protocol.TimetableChangeRequest
	for i := range reqs {
		if reqs[i].ID == requestID {
			target = &reqs[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("요청(%s)을 찾을 수 없습니다", requestID)
	}

	target.ReviewedBy = reviewerName
	target.CommittedAt = time.Now().Format("2006-01-02 15:04:05")

	if !approve {
		target.Status = protocol.StatusRejected
		target.RejectReason = rejectReason
		if err := s.store.SaveRecord("timetable_requests", target.ID, *target); err != nil {
			return nil, err
		}
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

	// 승인: 마스터 시간표 셀 반영
	cells, err := s.ListCells()
	if err == nil {
		s.applyCellUpdate(cells, *target)
	}
	target.Status = protocol.StatusApproved
	if err := s.store.SaveRecord("timetable_requests", target.ID, *target); err != nil {
		return nil, err
	}

	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableApproved,
			Title:     fmt.Sprintf("[%s반 %d교시] 시간표 변경 확정 반영", target.SourceCell.ClassName, target.SourceCell.Period),
			Message:   fmt.Sprintf("%s(%s) → %s(%s) 변경이 마스터 시간표에 반영되었습니다.", target.SourceCell.Subject, target.RequesterName, target.TargetCell.Subject, target.PartnerName),
			TargetIDs: compactIDs(target.RequesterID, target.PartnerID),
			ActorName: reviewerName,
			Payload:   *target,
		})
	}
	return target, nil
}

func (s *Service) applyCellUpdate(cells []protocol.TimetableCell, req protocol.TimetableChangeRequest) {
	// 삼각/사각 다단계 연쇄 교환 또는 단일 이동 step 목록이 있는 경우 전체 단계를 일괄 반영
	if len(req.ChainSteps) > 0 {
		cellMap := make(map[string]protocol.TimetableCell, len(cells))
		for _, c := range cells {
			cellMap[c.ID] = c
		}
		for _, step := range req.ChainSteps {
			if c, ok := cellMap[step.CellID]; ok {
				c.DayOfWeek = step.ToDay
				c.Period = step.ToPeriod
				_ = s.store.SaveRecord("timetable_cells", c.ID, c)
			}
		}
		return
	}
	if req.RequestType == "SWAP" && req.TargetCell.ID != "" {
		// 맞교환: 두 셀의 요일/교시를 상호 교환 저장
		for i := range cells {
			switch cells[i].ID {
			case req.SourceCell.ID:
				cells[i].DayOfWeek = req.TargetCell.DayOfWeek
				cells[i].Period = req.TargetCell.Period
				_ = s.store.SaveRecord("timetable_cells", cells[i].ID, cells[i])
			case req.TargetCell.ID:
				cells[i].DayOfWeek = req.SourceCell.DayOfWeek
				cells[i].Period = req.SourceCell.Period
				_ = s.store.SaveRecord("timetable_cells", cells[i].ID, cells[i])
			}
		}
		return
	}
	// 빈 교시 이동 (MOVE) 또는 보강/대타
	if req.SourceCell.ID != "" && req.TargetCell.ID == "" && req.TargetCell.DayOfWeek > 0 && req.TargetCell.Period > 0 && req.RequestType == "MOVE" {
		for i := range cells {
			if cells[i].ID == req.SourceCell.ID {
				cells[i].DayOfWeek = req.TargetCell.DayOfWeek
				cells[i].Period = req.TargetCell.Period
				_ = s.store.SaveRecord("timetable_cells", cells[i].ID, cells[i])
				break
			}
		}
		return
	}
	if req.SourceCell.ID != "" && req.PartnerID != "" {
		for i := range cells {
			if cells[i].ID == req.SourceCell.ID {
				cells[i].TeacherID = req.PartnerID
				if req.TargetCell.Subject != "" {
					cells[i].Subject = req.TargetCell.Subject
				}
				_ = s.store.SaveRecord("timetable_cells", cells[i].ID, cells[i])
				break
			}
		}
	}
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
	parsedDate, err := time.Parse("2006-01-02", notice.ExamDate)
	if err != nil {
		return
	}
	wd := int(parsedDate.Weekday()) // 0=일, 1=월 ... 5=금, 6=토
	if wd < 1 || wd > 5 {
		return // 주말 시험은 결보강 이관 불필요
	}

	cells, _ := s.ListCells()
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tNameMap := make(map[string]string, len(teachers))
	for _, t := range teachers {
		tNameMap[t.ID] = t.Name
	}

	for _, tid := range notice.ConfirmedIDs {
		for _, c := range cells {
			if c.DayOfWeek == wd && c.TeacherID == tid {
				req := protocol.TimetableChangeRequest{
					ID:            fmt.Sprintf("ext-trip-%s-%s", notice.ID, c.ID),
					RequestType:   "EXTERNAL_TRIP",
					TargetDate:    notice.ExamDate,
					RequesterID:   tid,
					RequesterName: tNameMap[tid],
					PartnerName:   "보강교사 미배정",
					SourceCell:    c,
					TargetCell:    c,
					Reason:        fmt.Sprintf("[외부감독 자동이관] %s 파견 출장", notice.Title),
					Status:        protocol.StatusStaging,
					CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
				}
				_ = s.store.SaveRecord("timetable_requests", req.ID, req)
			}
		}
	}
}

func (s *Service) onSchoolEventPublished(evt protocol.DomainEvent) {
	ev, ok := evt.Payload.(protocol.SchoolEvent)
	if !ok || ev.Status != protocol.StatusApproved {
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

	switch ev.LinkType {
	case protocol.EventLinkClassBlock:
		for _, c := range cells {
			if c.DayOfWeek == wd && c.Period >= ev.StartPeriod && c.Period <= ev.EndPeriod && sliceContainsInt(ev.TargetGrades, c.Grade) {
				c.BlockedBy = ev.Title
				_ = s.store.SaveRecord("timetable_cells", c.ID, c)
			}
		}
	case protocol.EventLinkTeacherTrip:
		for _, tid := range ev.LeadTeachers {
			for _, c := range cells {
				if c.DayOfWeek == wd && c.TeacherID == tid && (ev.StartPeriod == 0 || (c.Period >= ev.StartPeriod && c.Period <= ev.EndPeriod)) {
					req := protocol.TimetableChangeRequest{
						ID:            fmt.Sprintf("evt-trip-%s-%s", ev.ID, c.ID),
						RequestType:   "EVENT_TRIP",
						TargetDate:    ev.EventDate,
						RequesterID:   tid,
						RequesterName: tNameMap[tid],
						PartnerName:   "보강교사 미배정",
						SourceCell:    c,
						TargetCell:    c,
						Reason:        fmt.Sprintf("[행사 자동이관] %s 인솔 출장", ev.Title),
						Status:        protocol.StatusStaging,
						CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
					}
					_ = s.store.SaveRecord("timetable_requests", req.ID, req)
				}
			}
		}
	}
}

func sliceContainsInt(arr []int, target int) bool {
	for _, v := range arr {
		if v == target {
			return true
		}
	}
	return false
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
