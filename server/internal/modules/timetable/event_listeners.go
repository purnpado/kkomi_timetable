package timetable

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

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
					if c.DayOfWeek == wd && slices.Contains(protocol.AllCellTeachers(c), tid) {
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
			if c.DayOfWeek == wd && slices.Contains(protocol.AllCellTeachers(c), tid) &&
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
