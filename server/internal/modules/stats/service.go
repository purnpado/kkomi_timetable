package stats

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// Service는 월별·학기별·학년도별 결보강·수업교환·내외부 시험감독·행사 실적을 통합 집계합니다.
type Service struct {
	store *database.Store
}

// NewService는 통합 통계 정산 서비스를 생성합니다.
func NewService(store *database.Store) *Service {
	return &Service{store: store}
}

func lastDayOfMonth(year, month int) string {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// ResolvePeriodRange는 조회 모드(MONTH, SEMESTER_1, SEMESTER_2, YEAR, CUSTOM, ALL)에 따른 실제 날짜 범위와 한글 라벨을 계산합니다.
func ResolvePeriodRange(mode string, year, month int, startDate, endDate string) (resolvedMode, label, start, end string) {
	now := time.Now()
	if year <= 0 {
		year = now.Year()
	}
	if month < 1 || month > 12 {
		month = int(now.Month())
	}
	resolvedMode = strings.ToUpper(strings.TrimSpace(mode))
	switch resolvedMode {
	case "MONTH":
		start = fmt.Sprintf("%04d-%02d-01", year, month)
		end = lastDayOfMonth(year, month)
		label = fmt.Sprintf("%d년 %d월 월간 정산", year, month)
	case "SEMESTER_1":
		start = fmt.Sprintf("%04d-03-01", year)
		end = fmt.Sprintf("%04d-08-31", year)
		label = fmt.Sprintf("%d학년도 1학기 (%04d.03~08)", year, year)
	case "SEMESTER_2":
		start = fmt.Sprintf("%04d-09-01", year)
		end = lastDayOfMonth(year+1, 2)
		label = fmt.Sprintf("%d학년도 2학기 (%04d.09~%04d.02)", year, year, year+1)
	case "YEAR":
		start = fmt.Sprintf("%04d-03-01", year)
		end = lastDayOfMonth(year+1, 2)
		label = fmt.Sprintf("%d학년도 전체 (%04d.03~%04d.02)", year, year, year+1)
	case "CUSTOM":
		start = strings.TrimSpace(startDate)
		end = strings.TrimSpace(endDate)
		if start == "" && end == "" {
			return "ALL", "전체 누적 기간", "", ""
		}
		if start == "" {
			start = end
		} else if end == "" {
			end = start
		}
		if start > end {
			start, end = end, start
		}
		label = fmt.Sprintf("기간 지정 (%s ~ %s)", start, end)
	default:
		resolvedMode = "ALL"
		label = "전체 누적 기간"
	}
	return resolvedMode, label, start, end
}

func inPeriodDate(dateStr, start, end string) bool {
	d := strings.TrimSpace(dateStr)
	if len(d) >= 10 {
		d = d[:10]
	}
	if start == "" && end == "" {
		return true
	}
	if d == "" {
		return false
	}
	if start != "" && d < start {
		return false
	}
	if end != "" && d > end {
		return false
	}
	return true
}

func monthKeyOf(dateStr string) string {
	d := strings.TrimSpace(dateStr)
	if len(d) >= 7 {
		return d[:7]
	}
	return ""
}

func isSubstitutionKind(reqType string) bool {
	return reqType == "SUBSTITUTE" || reqType == "ABSENCE_SUBSTITUTE" || reqType == "EXTERNAL_TRIP" || reqType == "EVENT_TRIP"
}

// BuildPeriodReport는 지정 기간 내 확정된 결보강·수업교환, 교내·외부 시험감독 및 학사행사를 집계한 보고서를 반환합니다.
func (s *Service) BuildPeriodReport(mode string, year, month int, startDate, endDate string) (protocol.PeriodStatsReport, error) {
	schoolName := "꼬미중·고등학교"
	if cfgs, err := database.LoadCollection[protocol.SchoolConfig](s.store, "school_configs"); err == nil {
		for _, c := range cfgs {
			if c.ID == "default" && strings.TrimSpace(c.SchoolName) != "" {
				schoolName = strings.TrimSpace(c.SchoolName)
				break
			}
		}
	}

	rMode, rLabel, start, end := ResolvePeriodRange(mode, year, month, startDate, endDate)
	report := protocol.PeriodStatsReport{
		PeriodMode:  rMode,
		PeriodLabel: rLabel,
		StartDate:   start,
		EndDate:     end,
		SchoolName:  schoolName,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	teachers, err := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	if err != nil {
		return report, err
	}
	statMap := make(map[string]*protocol.TeacherPeriodStat, len(teachers))
	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			continue
		}
		statMap[t.ID] = &protocol.TeacherPeriodStat{
			TeacherID:     t.ID,
			TeacherName:   t.Name,
			Subject:       t.Subject,
			Position:      t.Position,
			HomeroomClass: t.HomeroomClass,
		}
	}

	trendMap := make(map[string]*protocol.MonthlyTrendRow)
	ensureTrend := func(dateStr string) *protocol.MonthlyTrendRow {
		ym := monthKeyOf(dateStr)
		if ym == "" {
			return nil
		}
		if trendMap[ym] == nil {
			trendMap[ym] = &protocol.MonthlyTrendRow{Month: ym}
		}
		return trendMap[ym]
	}

	// 1. 확정(APPROVED)된 수업 교환 및 결보강 집계
	reqs, _ := database.LoadCollection[protocol.TimetableChangeRequest](s.store, "timetable_requests")
	for _, rq := range reqs {
		if rq.Status != protocol.StatusApproved {
			continue
		}
		effDate := strings.TrimSpace(rq.TargetDate)
		if effDate == "" && len(rq.CommittedAt) >= 10 {
			effDate = rq.CommittedAt[:10]
		}
		if !inPeriodDate(effDate, start, end) {
			continue
		}
		report.ApprovedChanges = append(report.ApprovedChanges, rq)
		report.TotalChanges++
		tr := ensureTrend(effDate)
		if tr != nil {
			tr.TimetableChanges++
		}
		if isSubstitutionKind(rq.RequestType) {
			report.TotalSubstitutes++
			if tr != nil {
				tr.SubstituteSessions++
			}
			origID := rq.SourceCell.TeacherID
			if origID == "" {
				origID = rq.RequesterID
			}
			if st, ok := statMap[origID]; ok {
				st.SubstituteTakenCount++
			}
			if rq.PartnerID != "" && rq.PartnerID != origID {
				if st, ok := statMap[rq.PartnerID]; ok {
					st.SubstituteGivenCount++
				}
			}
		} else {
			seen := make(map[string]bool)
			for _, id := range []string{rq.SourceCell.TeacherID, rq.PartnerID} {
				if id != "" && !seen[id] {
					seen[id] = true
					if st, ok := statMap[id]; ok {
						st.SwapCount++
					}
				}
			}
			for _, step := range rq.ChainSteps {
				if step.TeacherID != "" && !seen[step.TeacherID] {
					seen[step.TeacherID] = true
					if st, ok := statMap[step.TeacherID]; ok {
						st.SwapCount++
					}
				}
			}
			if len(seen) == 0 && rq.RequesterID != "" {
				if st, ok := statMap[rq.RequesterID]; ok {
					st.SwapCount++
				}
			}
		}
	}

	// 2. 교내 지필고사 감독 실적 집계
	examSlots, _ := database.LoadCollection[protocol.ExamSlot](s.store, "exam_slots")
	for _, sl := range examSlots {
		if !inPeriodDate(sl.ExamDate, start, end) {
			continue
		}
		if st, ok := statMap[sl.TeacherID]; ok {
			switch sl.SlotRole {
			case "MAIN":
				st.InternalMainCount++
			case "SUB":
				st.InternalSubCount++
			case "CORRIDOR":
				st.InternalCorrCount++
			}
			st.InternalTotal++
			report.TotalInternal++
			if tr := ensureTrend(sl.ExamDate); tr != nil {
				tr.InternalExamSlots++
			}
		}
	}

	// 3. 외부 시험감독 확정 파견 실적 집계
	extNotices, _ := database.LoadCollection[protocol.ExternalExamNotice](s.store, "external_exams")
	for _, ex := range extNotices {
		if !ex.IsConfirmed || !inPeriodDate(ex.ExamDate, start, end) {
			continue
		}
		tr := ensureTrend(ex.ExamDate)
		for _, tid := range ex.ConfirmedIDs {
			if st, ok := statMap[tid]; ok {
				st.ExternalExamCount++
			}
			report.TotalExternal++
			if tr != nil {
				tr.ExternalDispatches++
			}
		}
	}

	// 4. 확정 학사행사 인솔 실적 집계
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")
	for _, ev := range events {
		if ev.Status != protocol.StatusApproved || !inPeriodDate(ev.EventDate, start, end) {
			continue
		}
		report.ApprovedEvents = append(report.ApprovedEvents, ev)
		report.TotalEvents++
		if tr := ensureTrend(ev.EventDate); tr != nil {
			tr.ApprovedEvents++
		}
		seenLead := make(map[string]bool)
		for _, tid := range ev.LeadTeachers {
			if tid == "" || seenLead[tid] {
				continue
			}
			seenLead[tid] = true
			if st, ok := statMap[tid]; ok {
				st.EventLeadCount++
			}
		}
	}

	for _, st := range statMap {
		st.TotalActivity = st.SwapCount + st.SubstituteGivenCount + st.InternalTotal + st.ExternalExamCount + st.EventLeadCount
		report.TeacherStats = append(report.TeacherStats, *st)
	}
	sort.Slice(report.TeacherStats, func(i, j int) bool {
		if report.TeacherStats[i].TotalActivity != report.TeacherStats[j].TotalActivity {
			return report.TeacherStats[i].TotalActivity > report.TeacherStats[j].TotalActivity
		}
		return report.TeacherStats[i].TeacherName < report.TeacherStats[j].TeacherName
	})

	for _, tr := range trendMap {
		report.MonthlyTrend = append(report.MonthlyTrend, *tr)
	}
	sort.Slice(report.MonthlyTrend, func(i, j int) bool {
		return report.MonthlyTrend[i].Month < report.MonthlyTrend[j].Month
	})
	sort.Slice(report.ApprovedChanges, func(i, j int) bool {
		return report.ApprovedChanges[i].TargetDate < report.ApprovedChanges[j].TargetDate
	})
	sort.Slice(report.ApprovedEvents, func(i, j int) bool {
		return report.ApprovedEvents[i].EventDate < report.ApprovedEvents[j].EventDate
	})
	return report, nil
}
