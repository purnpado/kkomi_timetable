package external_exam

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
	"kkomi-timetable/server/internal/eventbus"
)

// Service는 대외 시험감독(수능·모평·검정고시·임용 등) 공고, 자발적 지원 및
// 학교별 맞춤형 다단계 차출 순번 산정기를 담당합니다. (계좌/수당 기능 없음)
type Service struct {
	store *database.Store
	bus   *eventbus.Bus
}

// NewService는 외부 시험감독 모듈 서비스를 생성합니다.
func NewService(store *database.Store, bus *eventbus.Bus) *Service {
	return &Service{store: store, bus: bus}
}

// ListNotices는 외부 시험 공고 목록을 반환합니다.
func (s *Service) ListNotices() ([]protocol.ExternalExamNotice, error) {
	return database.LoadCollection[protocol.ExternalExamNotice](s.store, "external_exams")
}

// CreateNotice는 신규 대외 시험 공고를 검증 후 등록합니다.
func (s *Service) CreateNotice(notice protocol.ExternalExamNotice) (protocol.ExternalExamNotice, error) {
	notice.Title = strings.TrimSpace(notice.Title)
	notice.ExamDate = strings.TrimSpace(notice.ExamDate)
	if notice.Title == "" {
		return notice, fmt.Errorf("외부 시험 공고 제목을 입력해 주세요")
	}
	if _, err := time.Parse("2006-01-02", notice.ExamDate); err != nil {
		return notice, fmt.Errorf("시험 일자(YYYY-MM-DD) 형식이 올바르지 않습니다")
	}
	if notice.RequiredCount <= 0 {
		notice.RequiredCount = 1
	}
	if notice.ID == "" {
		notice.ID = fmt.Sprintf("ext-%d", time.Now().UnixNano())
	}
	cfg, _ := s.GetSchoolConfig()
	if strings.TrimSpace(notice.ExamCategory) == "" && len(cfg.Categories) > 0 {
		notice.ExamCategory = cfg.Categories[0].Name
	}
	inheritPreset := notice.PresetMode == "" && len(notice.PriorityOrder) == 0
	if notice.PresetMode == "" {
		notice.PresetMode = cfg.DefaultPresetMode
	}
	if len(notice.PriorityOrder) == 0 {
		if len(cfg.DefaultPriorityOrder) > 0 {
			notice.PriorityOrder = append([]protocol.ExternalExamCriteria(nil), cfg.DefaultPriorityOrder...)
		} else {
			notice.PriorityOrder = PresetOrder(notice.PresetMode)
		}
	}
	if notice.ScoringMode == "" {
		notice.ScoringMode = cfg.DefaultScoringMode
	}
	if notice.TieBreaker == "" {
		notice.TieBreaker = cfg.DefaultTieBreaker
	}
	if notice.VolunteerOverflowMode == "" {
		notice.VolunteerOverflowMode = cfg.DefaultVolunteerOverflow
	}
	if inheritPreset && !notice.TransferGraceYear {
		notice.TransferGraceYear = cfg.DefaultTransferGraceYear
	}
	if inheritPreset && !notice.ExcludeExempt {
		notice.ExcludeExempt = cfg.DefaultExcludeExempt
	}
	if len(notice.CategoryExcludeRules) == 0 {
		notice.CategoryExcludeRules = defaultExcludeRulesForCategory(cfg, notice.ExamCategory)
	}
	if err := s.store.SaveRecord("external_exams", notice.ID, notice); err != nil {
		return notice, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventExternalExamUpdated,
			Title:   fmt.Sprintf("[%s] 외부 시험감독 공고 등록", notice.Title),
			Message: fmt.Sprintf("시험일: %s · 차출 인원: %d명", notice.ExamDate, notice.RequiredCount),
			Payload: notice,
		})
	}
	return notice, nil
}

// UpdateNotice는 기존 외부 시험 공고의 기본 정보 및 차출 기준을 수정합니다.
func (s *Service) UpdateNotice(notice protocol.ExternalExamNotice) (*protocol.ExternalExamNotice, error) {
	if strings.TrimSpace(notice.ID) == "" {
		return nil, fmt.Errorf("수정할 공고 ID가 필요합니다")
	}
	existing, err := database.LoadRecord[protocol.ExternalExamNotice](s.store, "external_exams", notice.ID)
	if err != nil || existing == nil {
		return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", notice.ID)
	}
	if strings.TrimSpace(notice.Title) != "" {
		existing.Title = strings.TrimSpace(notice.Title)
	}
	if strings.TrimSpace(notice.ExamCategory) != "" {
		existing.ExamCategory = strings.TrimSpace(notice.ExamCategory)
	}
	if strings.TrimSpace(notice.ExamDate) != "" {
		if _, pErr := time.Parse("2006-01-02", strings.TrimSpace(notice.ExamDate)); pErr != nil {
			return nil, fmt.Errorf("시험 일자(YYYY-MM-DD) 형식이 올바르지 않습니다")
		}
		existing.ExamDate = strings.TrimSpace(notice.ExamDate)
	}
	if strings.TrimSpace(notice.Location) != "" {
		existing.Location = strings.TrimSpace(notice.Location)
	}
	if notice.RequiredCount > 0 {
		existing.RequiredCount = notice.RequiredCount
	}
	if notice.PresetMode != "" {
		existing.PresetMode = notice.PresetMode
	}
	if notice.ScoringMode != "" {
		existing.ScoringMode = notice.ScoringMode
	}
	if len(notice.PriorityOrder) > 0 {
		existing.PriorityOrder = notice.PriorityOrder
	}
	existing.ExcludeExempt = notice.ExcludeExempt
	existing.TransferGraceYear = notice.TransferGraceYear
	if notice.CategoryExcludeRules != nil {
		existing.CategoryExcludeRules = notice.CategoryExcludeRules
	}
	if notice.TieBreaker != "" {
		existing.TieBreaker = notice.TieBreaker
	}
	if notice.LotterySeed != "" {
		existing.LotterySeed = notice.LotterySeed
	}
	if notice.VolunteerOverflowMode != "" {
		existing.VolunteerOverflowMode = notice.VolunteerOverflowMode
	}
	if err := s.store.SaveRecord("external_exams", existing.ID, *existing); err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:    protocol.EventExternalExamUpdated,
			Title:   fmt.Sprintf("[%s] 외부 시험 공고 수정", existing.Title),
			Message: fmt.Sprintf("시험일: %s · 차출 인원: %d명", existing.ExamDate, existing.RequiredCount),
			Payload: *existing,
		})
	}
	return existing, nil
}

// DeleteNotice는 외부 시험 공고를 삭제하고 시간표 대기열의 미승인 출장 이관을 정리합니다.
func (s *Service) DeleteNotice(noticeID, actorName string) error {
	if strings.TrimSpace(noticeID) == "" {
		return fmt.Errorf("삭제할 공고 ID가 필요합니다")
	}
	existing, err := database.LoadRecord[protocol.ExternalExamNotice](s.store, "external_exams", noticeID)
	if err != nil || existing == nil {
		return fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
	}
	if err := s.store.DeleteRecord("external_exams", noticeID); err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExternalExamUpdated,
			Title:     fmt.Sprintf("[%s] 외부 시험 공고 삭제", existing.Title),
			Message:   "외부 시험 공고와 미승인 결보강 대기 항목이 정리되었습니다.",
			ActorName: actorName,
			Payload:   map[string]string{"deletedId": noticeID},
		})
	}
	return nil
}

// ToggleVolunteer는 교사의 자발적 외부감독 지원 또는 지원 취소를 처리합니다.
func (s *Service) ToggleVolunteer(noticeID, teacherID string) (*protocol.ExternalExamNotice, error) {
	teacher, tErr := database.LoadRecord[protocol.Teacher](s.store, "teachers", teacherID)
	if tErr == nil && teacher != nil && (protocol.IsSystemAdminAccount(*teacher) || teacher.IsParentHonorary) {
		return nil, fmt.Errorf("시스템 관리자 및 명예감독 계정은 외부 시험감독에 지원할 수 없습니다")
	}
	notices, err := s.ListNotices()
	if err != nil {
		return nil, err
	}
	for i := range notices {
		if notices[i].ID == noticeID {
			if notices[i].IsConfirmed {
				return nil, fmt.Errorf("이미 파견 명단이 확정된 공고는 지원 상태를 변경할 수 없습니다")
			}
			if slices.Contains(notices[i].VolunteerIDs, teacherID) {
				filtered := make([]string, 0, len(notices[i].VolunteerIDs))
				for _, id := range notices[i].VolunteerIDs {
					if id != teacherID {
						filtered = append(filtered, id)
					}
				}
				notices[i].VolunteerIDs = filtered
			} else {
				notices[i].VolunteerIDs = append(notices[i].VolunteerIDs, teacherID)
			}
			if err := s.store.SaveRecord("external_exams", notices[i].ID, notices[i]); err != nil {
				return nil, err
			}
			if s.bus != nil {
				s.bus.Publish(protocol.DomainEvent{
					Type:    protocol.EventExternalExamUpdated,
					Title:   fmt.Sprintf("[%s] 자발적 지원 현황 변경", notices[i].Title),
					Message: fmt.Sprintf("현재 지원자 %d명", len(notices[i].VolunteerIDs)),
					Payload: notices[i],
				})
			}
			return &notices[i], nil
		}
	}
	return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
}

// RuleUpdateOptions는 외부 시험 차출 규칙 상세 옵션입니다.
type RuleUpdateOptions struct {
	PresetMode            string
	ScoringMode           string
	PriorityOrder         []protocol.ExternalExamCriteria
	ExcludeExempt         bool
	TransferGraceYear     bool
	CategoryExcludeRules  []string
	TieBreaker            string
	LotterySeed           string
	VolunteerOverflowMode string
}

// UpdateRules는 담당교사가 학교 내규에 맞게 차출 기준 프리셋 또는 커스텀 우선순위 순서를 변경할 때 호출됩니다.
func (s *Service) UpdateRules(noticeID, presetMode string, priorityOrder []protocol.ExternalExamCriteria, excludeExempt bool) (*protocol.ExternalExamNotice, error) {
	return s.UpdateRulesWithOptions(noticeID, RuleUpdateOptions{
		PresetMode:    presetMode,
		PriorityOrder: priorityOrder,
		ExcludeExempt: excludeExempt,
	})
}

// UpdateRulesWithOptions는 점수제·별도 트랙·전입 유예·종목 제외·동점·초과 선발 기준까지 확장 업데이트합니다.
func (s *Service) UpdateRulesWithOptions(noticeID string, opts RuleUpdateOptions) (*protocol.ExternalExamNotice, error) {
	notices, err := s.ListNotices()
	if err != nil {
		return nil, err
	}
	for i := range notices {
		if notices[i].ID == noticeID {
			if opts.PresetMode != "" {
				notices[i].PresetMode = opts.PresetMode
			}
			if opts.PresetMode != "CUSTOM" && len(opts.PriorityOrder) == 0 {
				notices[i].PriorityOrder = PresetOrder(notices[i].PresetMode)
			} else if len(opts.PriorityOrder) > 0 {
				notices[i].PriorityOrder = opts.PriorityOrder
			}
			notices[i].ExcludeExempt = opts.ExcludeExempt
			if opts.ScoringMode != "" {
				notices[i].ScoringMode = opts.ScoringMode
			}
			notices[i].TransferGraceYear = opts.TransferGraceYear
			if opts.CategoryExcludeRules != nil {
				notices[i].CategoryExcludeRules = opts.CategoryExcludeRules
			}
			if opts.TieBreaker != "" {
				notices[i].TieBreaker = opts.TieBreaker
			}
			if opts.LotterySeed != "" {
				notices[i].LotterySeed = opts.LotterySeed
			}
			if opts.VolunteerOverflowMode != "" {
				notices[i].VolunteerOverflowMode = opts.VolunteerOverflowMode
			}
			if err := s.store.SaveRecord("external_exams", notices[i].ID, notices[i]); err != nil {
				return nil, err
			}
			return &notices[i], nil
		}
	}
	return nil, fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
}

// ConfirmDispatch는 외부 시험감독 담당교사가 [최종 선택(파견 명단 확정)]을 실행할 때
// 이전 확정 명단과 Diff를 계산해 누적 횟수/최근 파견일을 정확히 보정하고, 평일 시험일 때만 시간표 이관을 연동합니다.
func (s *Service) ConfirmDispatch(noticeID string, selectedIDs []string, actorName string) (*protocol.ExternalExamNotice, error) {
	seen := make(map[string]bool, len(selectedIDs))
	deduped := make([]string, 0, len(selectedIDs))
	for _, rawID := range selectedIDs {
		id := strings.TrimSpace(rawID)
		if id != "" && !seen[id] {
			seen[id] = true
			deduped = append(deduped, id)
		}
	}

	var notice *protocol.ExternalExamNotice
	isWeekday := false
	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.ExternalExamNotice](tx, "external_exams", noticeID)
		if lErr != nil || loaded == nil {
			return fmt.Errorf("외부 시험 공고(%s)를 찾을 수 없습니다", noticeID)
		}
		allNotices, _ := database.LoadCollectionTx[protocol.ExternalExamNotice](tx, "external_exams")
		txTeachers, tErr := database.LoadCollectionTx[protocol.Teacher](tx, "teachers")
		if tErr != nil {
			return tErr
		}
		tMap := make(map[string]protocol.Teacher, len(txTeachers))
		for _, t := range txTeachers {
			tMap[t.ID] = t
		}
		for _, sid := range deduped {
			t, ok := tMap[sid]
			if !ok {
				return fmt.Errorf("선택한 교사(%s)를 찾을 수 없습니다", sid)
			}
			if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
				return fmt.Errorf("%s 계정은 외부 시험감독 파견 대상이 아닙니다", t.Name)
			}
		}

		prevSet := make(map[string]bool)
		if loaded.IsConfirmed {
			for _, id := range loaded.ConfirmedIDs {
				prevSet[id] = true
			}
		}
		nextSet := make(map[string]bool, len(deduped))
		for _, id := range deduped {
			nextSet[id] = true
		}

		if parsedDate, pErr := time.Parse("2006-01-02", loaded.ExamDate); pErr == nil {
			wd := int(parsedDate.Weekday())
			isWeekday = wd >= 1 && wd <= 5
		}

		for i := range txTeachers {
			tid := txTeachers[i].ID
			wasSel := prevSet[tid]
			nowSel := nextSet[tid]
			if !wasSel && nowSel {
				txTeachers[i].ExternalExamCount++
				if loaded.ExamDate > txTeachers[i].LastExternalDispatch {
					txTeachers[i].LastExternalDispatch = loaded.ExamDate
				}
				if sErr := tx.SaveRecord("teachers", tid, txTeachers[i]); sErr != nil {
					return sErr
				}
			} else if wasSel && !nowSel {
				if txTeachers[i].ExternalExamCount > 0 {
					txTeachers[i].ExternalExamCount--
				}
				if txTeachers[i].LastExternalDispatch == loaded.ExamDate {
					maxDate := ""
					for _, other := range allNotices {
						if other.ID != loaded.ID && other.IsConfirmed && slices.Contains(other.ConfirmedIDs, tid) {
							if other.ExamDate > maxDate {
								maxDate = other.ExamDate
							}
						}
					}
					txTeachers[i].LastExternalDispatch = maxDate
				}
				if sErr := tx.SaveRecord("teachers", tid, txTeachers[i]); sErr != nil {
					return sErr
				}
			}
		}

		loaded.ConfirmedIDs = deduped
		loaded.IsConfirmed = true
		loaded.LinkedToTimetable = isWeekday
		if sErr := tx.SaveRecord("external_exams", loaded.ID, *loaded); sErr != nil {
			return sErr
		}
		notice = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}

	if s.bus != nil {
		msg := fmt.Sprintf("파견 명단 %d명 확정이 완료되었습니다.", len(deduped))
		if isWeekday {
			msg = fmt.Sprintf("파견 교사의 %s 평일 수업이 시간표 결보강 대기열과 동기화되었습니다.", notice.ExamDate)
		}
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventExternalExamConfirmed,
			Title:     fmt.Sprintf("[%s] 외부 시험감독 파견 명단 확정 (%d명)", notice.Title, len(deduped)),
			Message:   msg,
			Urgent:    true,
			TargetIDs: deduped,
			ActorName: actorName,
			Payload:   *notice,
		})
	}
	return notice, nil
}
