package timetable

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

type resolvedDebtRecord struct {
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
}

// GetSemesterLedgerConfig는 학기 시수 원장 기준(총 수업 주수, 최소 확보율 경고치)을 반환합니다.
func (s *Service) GetSemesterLedgerConfig() (protocol.SemesterLedgerConfig, error) {
	loaded, err := database.LoadRecord[protocol.SemesterLedgerConfig](s.store, "semester_ledger_config", "default")
	if err == nil && loaded != nil && loaded.TotalWeeks > 0 {
		return *loaded, nil
	}
	return protocol.SemesterLedgerConfig{
		ID:             "default",
		SemesterName:   "2026학년도 1학기",
		TotalWeeks:     17,
		MinRatioPct:    95,
		IncludeCurrent: true,
	}, nil
}

// SaveSemesterLedgerConfig는 학기 시수 원장 기준을 저장합니다.
func (s *Service) SaveSemesterLedgerConfig(cfg protocol.SemesterLedgerConfig) (protocol.SemesterLedgerConfig, error) {
	cfg.ID = "default"
	if strings.TrimSpace(cfg.SemesterName) == "" {
		cfg.SemesterName = "2026학년도 1학기"
	}
	if cfg.TotalWeeks < 1 {
		cfg.TotalWeeks = 17
	}
	if cfg.MinRatioPct <= 0 || cfg.MinRatioPct > 100 {
		cfg.MinRatioPct = 95
	}
	if err := s.store.SaveRecord("semester_ledger_config", cfg.ID, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// SetMakeupDebtResolved는 행사·결강으로 발생한 미보충 수업 부채의 보충 완료 여부를 저장합니다.
func (s *Service) SetMakeupDebtResolved(debtID string, resolved bool) error {
	if strings.TrimSpace(debtID) == "" {
		return fmt.Errorf("부채 항목 ID가 필요합니다")
	}
	if !resolved {
		return s.store.DeleteRecord("semester_debt_resolved", debtID)
	}
	return s.store.SaveRecord("semester_debt_resolved", debtID, resolvedDebtRecord{ID: debtID, Resolved: true})
}

// CalculateSemesterLedger는 정규 시간표 기준 학기 목표 시수, 행사/결강 결손 시수,
// 보강/대강 누적 변동 및 미보충 부채 잔고를 산출합니다.
func (s *Service) CalculateSemesterLedger() (protocol.SemesterLedgerReport, error) {
	cfg, _ := s.GetSemesterLedgerConfig()
	cells, err := s.ListCells()
	if err != nil {
		return protocol.SemesterLedgerReport{}, err
	}
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachers))
	for _, t := range teachers {
		tMap[t.ID] = t
	}
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")
	reqs, _ := s.ListRequests()
	resolvedList, _ := database.LoadCollection[resolvedDebtRecord](s.store, "semester_debt_resolved")
	resolvedMap := make(map[string]bool, len(resolvedList))
	for _, r := range resolvedList {
		if r.Resolved {
			resolvedMap[r.ID] = true
		}
	}

	type rowKey struct {
		className string
		subject   string
	}
	rowAcc := make(map[rowKey]*protocol.SemesterLedgerRow)
	for _, c := range cells {
		rk := rowKey{className: c.ClassName, subject: c.Subject}
		row, exists := rowAcc[rk]
		if !exists {
			row = &protocol.SemesterLedgerRow{
				ClassName:   c.ClassName,
				Grade:       parseGrade(c.ClassName, c.Grade),
				Subject:     c.Subject,
				TeacherID:   c.TeacherID,
				TeacherName: teacherName(c.TeacherID, tMap),
			}
			rowAcc[rk] = row
		}
		row.WeeklyHours++
	}
	for _, row := range rowAcc {
		row.TargetHours = row.WeeklyHours * cfg.TotalWeeks
	}

	balMap := make(map[string]*protocol.TeacherMakeupBalance)
	ensureBal := func(tid string) *protocol.TeacherMakeupBalance {
		if tid == "" {
			return nil
		}
		b, ok := balMap[tid]
		if !ok {
			b = &protocol.TeacherMakeupBalance{
				TeacherID:   tid,
				TeacherName: teacherName(tid, tMap),
			}
			balMap[tid] = b
		}
		return b
	}

	var debts []protocol.MakeupDebtItem

	// 1. 확정 학교 행사(CLASS_BLOCK) 및 공휴일/휴업일(HOLIDAY) 처리 (다일 연속 행사 지원)
	holidayDeductedHours := 0
	for _, ev := range events {
		if ev.Status != protocol.StatusApproved ||
			(ev.LinkType != protocol.EventLinkClassBlock && ev.LinkType != protocol.EventLinkHoliday) {
			continue
		}
		dates := protocol.EventDates(ev)
		multiDay := len(dates) > 1
		for _, dateStr := range dates {
			parsed, pErr := time.Parse("2006-01-02", dateStr)
			if pErr != nil {
				continue
			}
			wd := int(parsed.Weekday())
			if wd < 1 || wd > 5 {
				continue
			}
			for _, c := range cells {
				if c.DayOfWeek != wd || !matchesEventTarget(ev, c.Grade, c.ClassName) {
					continue
				}
				if ev.StartPeriod != 0 && (c.Period < ev.StartPeriod || c.Period > ev.EndPeriod) {
					continue
				}
				rk := rowKey{className: c.ClassName, subject: c.Subject}
				if ev.LinkType == protocol.EventLinkHoliday {
					// 공휴일/재량휴업일은 학기 시수 원장에서 자동 차감하고 교사 미보충 부채는 생성하지 않음
					if row, ok := rowAcc[rk]; ok {
						row.BlockedHours++
					}
					holidayDeductedHours++
					continue
				}
				debtID := fmt.Sprintf("debt-evt-%s-%s", ev.ID, c.ID)
				if multiDay {
					debtID = fmt.Sprintf("debt-evt-%s-%s-%s", ev.ID, dateStr, c.ID)
				}
				isResolved := resolvedMap[debtID]
				status := "UNRESOLVED"
				if isResolved {
					status = "RESOLVED"
				}
				debts = append(debts, protocol.MakeupDebtItem{
					ID:          debtID,
					CellID:      c.ID,
					Date:        dateStr,
					DayOfWeek:   c.DayOfWeek,
					Period:      c.Period,
					ClassName:   c.ClassName,
					Subject:     c.Subject,
					TeacherID:   c.TeacherID,
					TeacherName: teacherName(c.TeacherID, tMap),
					Reason:      fmt.Sprintf("[행사 결손] %s", ev.Title),
					Status:      status,
				})
				if row, ok := rowAcc[rk]; ok {
					row.BlockedHours++
					if isResolved {
						row.AddedHours++
					}
				}
				if !isResolved {
					if b := ensureBal(c.TeacherID); b != nil {
						b.PendingDebts++
					}
				}
			}
		}
	}

	// 2. 확정된 결보강·외부감독·인솔출장 내역 반영
	if cfg.IncludeCurrent {
		for _, rq := range reqs {
			if rq.Status != protocol.StatusApproved {
				continue
			}
			if isSubstitutionType(rq.RequestType) {
				if b := ensureBal(rq.RequesterID); b != nil {
					b.MissedHours++
				}
				if rq.PartnerID != "" {
					if b := ensureBal(rq.PartnerID); b != nil {
						b.CoveredHours++
					}
					if rq.TargetCell.Subject != "" && rq.TargetCell.Subject != rq.SourceCell.Subject {
						if rOld, ok := rowAcc[rowKey{rq.SourceCell.ClassName, rq.SourceCell.Subject}]; ok {
							rOld.BlockedHours++
						}
						rNew, exists := rowAcc[rowKey{rq.SourceCell.ClassName, rq.TargetCell.Subject}]
						if !exists {
							rNew = &protocol.SemesterLedgerRow{
								ClassName:   rq.SourceCell.ClassName,
								Grade:       parseGrade(rq.SourceCell.ClassName, rq.SourceCell.Grade),
								Subject:     rq.TargetCell.Subject,
								TeacherID:   rq.PartnerID,
								TeacherName: teacherName(rq.PartnerID, tMap),
							}
							rowAcc[rowKey{rq.SourceCell.ClassName, rq.TargetCell.Subject}] = rNew
						}
						rNew.AddedHours++
					}
				} else {
					debtID := fmt.Sprintf("debt-req-%s", rq.ID)
					isResolved := resolvedMap[debtID]
					status := "UNRESOLVED"
					if isResolved {
						status = "RESOLVED"
					}
					debts = append(debts, protocol.MakeupDebtItem{
						ID:          debtID,
						CellID:      rq.SourceCell.ID,
						Date:        rq.TargetDate,
						DayOfWeek:   rq.SourceCell.DayOfWeek,
						Period:      rq.SourceCell.Period,
						ClassName:   rq.SourceCell.ClassName,
						Subject:     rq.SourceCell.Subject,
						TeacherID:   rq.RequesterID,
						TeacherName: rq.RequesterName,
						Reason:      fmt.Sprintf("[미보강 결강] %s", rq.Reason),
						Status:      status,
					})
					if rOld, ok := rowAcc[rowKey{rq.SourceCell.ClassName, rq.SourceCell.Subject}]; ok {
						rOld.BlockedHours++
						if isResolved {
							rOld.AddedHours++
						}
					}
					if !isResolved {
						if b := ensureBal(rq.RequesterID); b != nil {
							b.PendingDebts++
						}
					}
				}
			}
		}
	}

	var rows []protocol.SemesterLedgerRow
	summary := map[string]int{
		"totalTargetHours":     0,
		"totalActualHours":     0,
		"totalDeficitHours":    0,
		"shortRowCount":        0,
		"unresolvedDebtCount":  0,
		"holidayDeductedHours": holidayDeductedHours,
	}
	for _, r := range rowAcc {
		r.ActualHours = r.TargetHours - r.BlockedHours + r.AddedHours
		if r.ActualHours < 0 {
			r.ActualHours = 0
		}
		if r.TargetHours > 0 {
			r.DeficitHours = r.TargetHours - r.ActualHours
			if r.DeficitHours < 0 {
				r.DeficitHours = 0
			}
			r.AchievedPct = (r.ActualHours * 100) / r.TargetHours
		} else {
			r.AchievedPct = 100
		}
		switch {
		case r.TargetHours == 0 || r.AchievedPct >= 100:
			r.Status = "충족"
		case r.AchievedPct >= cfg.MinRatioPct:
			r.Status = "주의"
		default:
			r.Status = "미달"
			summary["shortRowCount"]++
		}
		summary["totalTargetHours"] += r.TargetHours
		summary["totalActualHours"] += r.ActualHours
		summary["totalDeficitHours"] += r.DeficitHours
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AchievedPct != rows[j].AchievedPct {
			return rows[i].AchievedPct < rows[j].AchievedPct
		}
		if rows[i].ClassName != rows[j].ClassName {
			return rows[i].ClassName < rows[j].ClassName
		}
		return rows[i].Subject < rows[j].Subject
	})

	for _, d := range debts {
		if d.Status != "RESOLVED" {
			summary["unresolvedDebtCount"]++
		}
	}
	sort.Slice(debts, func(i, j int) bool {
		if debts[i].Status != debts[j].Status {
			return debts[i].Status > debts[j].Status // UNRESOLVED 우선
		}
		if debts[i].Date != debts[j].Date {
			return debts[i].Date < debts[j].Date
		}
		return debts[i].ClassName < debts[j].ClassName
	})

	var balances []protocol.TeacherMakeupBalance
	for _, b := range balMap {
		b.NetBalance = b.CoveredHours - b.MissedHours
		balances = append(balances, *b)
	}
	sort.Slice(balances, func(i, j int) bool {
		if balances[i].NetBalance != balances[j].NetBalance {
			return balances[i].NetBalance < balances[j].NetBalance
		}
		return balances[i].TeacherName < balances[j].TeacherName
	})

	return protocol.SemesterLedgerReport{
		Config:          cfg,
		Summary:         summary,
		Rows:            rows,
		Debts:           debts,
		TeacherBalances: balances,
	}, nil
}
