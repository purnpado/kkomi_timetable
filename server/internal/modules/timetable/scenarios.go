package timetable

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// CalculateScenarioMetrics는 시간표 셀 묶음의 충돌·제약 위반·연강·공강일 지표를 산출합니다.
func CalculateScenarioMetrics(
	cells []protocol.TimetableCell,
	teachers []protocol.Teacher,
	constraints []protocol.TeacherConstraint,
) protocol.ScenarioMetrics {
	return CalculateScenarioMetricsWithConfig(cells, teachers, constraints, protocol.SchoolConfig{})
}

// CalculateScenarioMetricsWithConfig는 학교 교과교실 설정 및 복수 코티칭을 반영해 시나리오 품질 지표를 산출합니다.
func CalculateScenarioMetricsWithConfig(
	cells []protocol.TimetableCell,
	teachers []protocol.Teacher,
	constraints []protocol.TeacherConstraint,
	cfg protocol.SchoolConfig,
) protocol.ScenarioMetrics {
	tMap := make(map[string]protocol.Teacher, len(teachers))
	for _, t := range teachers {
		tMap[t.ID] = t
	}

	m := protocol.ScenarioMetrics{TotalCells: len(cells)}
	type roomSlotKey struct {
		room   string
		day    int
		period int
	}
	roomCountMap := make(map[roomSlotKey]int)
	if protocol.IsSubjectClassroomEnabled(cfg) {
		for _, c := range cells {
			if c.Room != "" && c.Room != c.ClassName+"교실" {
				roomCountMap[roomSlotKey{room: c.Room, day: c.DayOfWeek, period: c.Period}]++
			}
		}
		for rk, cnt := range roomCountMap {
			maxConc := protocol.RoomMaxConcurrent(cfg, rk.room)
			if cnt > maxConc {
				m.ConflictCount += cnt - maxConc
			}
		}
	}

	// 1. 동시간대(학급·교사) 중복 및 순회 비출강·제약 위반 집계
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		cellTeachers := protocol.AllCellTeachers(c)
		for _, tid := range cellTeachers {
			if t, ok := tMap[tid]; ok && len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, c.DayOfWeek) {
				m.ConflictCount++
			}
		}

		hardHit := false
		for _, tid := range cellTeachers {
			if hardConstraintReason(tid, c.DayOfWeek, c.Period, constraints, tMap) != "" {
				hardHit = true
				break
			}
		}
		if hardHit {
			m.HardAvoidViolated++
		} else {
			for _, tid := range cellTeachers {
				if softConstraintReason(tid, c.DayOfWeek, c.Period, constraints, tMap) != "" {
					m.SoftAvoidViolated++
					break
				}
			}
		}

		for j := i + 1; j < len(cells); j++ {
			o := cells[j]
			if c.DayOfWeek != o.DayOfWeek || c.Period != o.Period {
				continue
			}
			if c.ClassName == o.ClassName || hasTeacherOverlap(c, o) {
				m.ConflictCount++
			}
		}
	}

	// 2. 교사별 3연강/4연강+ 및 주 1일 이상 전일 공강 확보 교사 수 집계
	teacherSlots := make(map[string]map[int]map[int]bool)
	for _, c := range cells {
		for _, tid := range protocol.AllCellTeachers(c) {
			if teacherSlots[tid] == nil {
				teacherSlots[tid] = make(map[int]map[int]bool)
			}
			if teacherSlots[tid][c.DayOfWeek] == nil {
				teacherSlots[tid][c.DayOfWeek] = make(map[int]bool)
			}
			teacherSlots[tid][c.DayOfWeek][c.Period] = true
		}
	}

	for _, t := range teachers {
		if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
			continue
		}
		days := teacherSlots[t.ID]
		totalT := 0
		activeDays := 0
		for d := 1; d <= 5; d++ {
			if len(days[d]) > 0 {
				activeDays++
				totalT += len(days[d])
			}
			streak := 0
			for p := 1; p <= 7; p++ {
				if days[d][p] {
					streak++
				} else {
					if streak == 3 {
						m.Consecutive3Count++
					} else if streak >= 4 {
						m.Consecutive4Plus++
					}
					streak = 0
				}
			}
			if streak == 3 {
				m.Consecutive3Count++
			} else if streak >= 4 {
				m.Consecutive4Plus++
			}
		}
		if totalT > 0 && activeDays < 5 {
			m.FreeDayTeachers++
		}
	}

	score := 100 - m.ConflictCount*20 - m.HardAvoidViolated*15 - m.Consecutive4Plus*10 - m.SoftAvoidViolated*3 - m.Consecutive3Count
	if score < 0 {
		score = 0
	}
	m.Score = score
	return m
}

// CurrentMetrics는 현재 정규 시간표의 품질 비교 지표를 계산해 반환합니다.
func (s *Service) CurrentMetrics() (protocol.ScenarioMetrics, error) {
	cells, err := s.ListCells()
	if err != nil {
		return protocol.ScenarioMetrics{}, err
	}
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	constraints, _ := s.ListConstraints()
	cfg, _ := s.GetSchoolConfig()
	return CalculateScenarioMetricsWithConfig(cells, teachers, constraints, cfg), nil
}

// ListScenarios는 저장된 시간표 비교 시나리오(A/B/C안 등) 목록을 반환합니다.
func (s *Service) ListScenarios() ([]protocol.TimetableScenario, error) {
	list, err := database.LoadCollection[protocol.TimetableScenario](s.store, "timetable_scenarios")
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].SlotCode != list[j].SlotCode {
			return list[i].SlotCode < list[j].SlotCode
		}
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return list, nil
}

// SaveScenario는 현재 정규 시간표를 지정 슬롯(A/B/C안 등) 스냅샷으로 저장하고 품질 지표를 산출합니다.
func (s *Service) SaveScenario(slotCode, name, description, actorName string) (protocol.TimetableScenario, error) {
	cells, err := s.ListCells()
	if err != nil {
		return protocol.TimetableScenario{}, err
	}
	if len(cells) == 0 {
		return protocol.TimetableScenario{}, errors.New("저장할 정규 시간표 셀이 비어 있습니다")
	}
	teachers, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	constraints, _ := s.ListConstraints()
	cfg, _ := s.GetSchoolConfig()

	code := strings.ToUpper(strings.TrimSpace(slotCode))
	if code == "" {
		code = "CUSTOM"
	}
	title := strings.TrimSpace(name)
	if title == "" {
		title = fmt.Sprintf("%s안 시간표 스냅샷", code)
	}
	id := fmt.Sprintf("scenario-%d", time.Now().UnixNano())
	if code == "A" || code == "B" || code == "C" || code == "D" {
		id = "scenario-" + code
	}

	sc := protocol.TimetableScenario{
		ID:          id,
		SlotCode:    code,
		Name:        title,
		Description: strings.TrimSpace(description),
		Cells:       cells,
		Metrics:     CalculateScenarioMetricsWithConfig(cells, teachers, constraints, cfg),
		CreatedBy:   actorName,
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := s.store.SaveRecord("timetable_scenarios", sc.ID, sc); err != nil {
		return sc, err
	}
	return sc, nil
}

// ApplyScenario는 선택한 시나리오(A/B/C안)를 Undo 이력을 남기며 정규 마스터 시간표에 즉시 채택 적용합니다.
func (s *Service) ApplyScenario(scenarioID, actorName string) (protocol.TimetableScenario, error) {
	if strings.TrimSpace(scenarioID) == "" {
		return protocol.TimetableScenario{}, errors.New("적용할 시나리오 ID가 필요합니다")
	}
	var sc *protocol.TimetableScenario
	err := s.store.WithTx(func(tx *database.TxStore) error {
		loaded, lErr := database.LoadRecordTx[protocol.TimetableScenario](tx, "timetable_scenarios", scenarioID)
		if lErr != nil || loaded == nil {
			return fmt.Errorf("시나리오(%s)를 찾을 수 없습니다", scenarioID)
		}
		beforeCells, _ := database.LoadCollectionTx[protocol.TimetableCell](tx, "timetable_cells")
		if hErr := recordHistoryTx(tx, fmt.Sprintf("시나리오 채택 (%s)", loaded.Name), actorName, beforeCells, loaded.Cells); hErr != nil {
			return hErr
		}
		if cErr := tx.ClearCollections("timetable_cells"); cErr != nil {
			return cErr
		}
		for _, c := range loaded.Cells {
			if sErr := tx.SaveRecord("timetable_cells", c.ID, c); sErr != nil {
				return sErr
			}
		}
		sc = loaded
		return nil
	})
	if err != nil {
		return protocol.TimetableScenario{}, err
	}
	if s.bus != nil {
		s.bus.Publish(protocol.DomainEvent{
			Type:      protocol.EventTimetableCellsUpdated,
			Title:     fmt.Sprintf("📋 시나리오 '%s' 정규 시간표 반영", sc.Name),
			Message:   fmt.Sprintf("종합 점수 %d점 (%d개 셀) 안이 정규 시간표로 채택되었습니다.", sc.Metrics.Score, len(sc.Cells)),
			ActorName: actorName,
			Payload:   map[string]interface{}{"scenarioId": sc.ID, "score": sc.Metrics.Score},
		})
	}
	return *sc, nil
}

// DeleteScenario는 저장된 비교 시나리오를 삭제합니다.
func (s *Service) DeleteScenario(scenarioID string) error {
	if strings.TrimSpace(scenarioID) == "" {
		return errors.New("삭제할 시나리오 ID가 필요합니다")
	}
	return s.store.DeleteRecord("timetable_scenarios", scenarioID)
}
