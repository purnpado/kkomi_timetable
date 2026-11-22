package timetable

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ExploreSwapOptions는 선택한 수업 셀에 대해 단순 이동·1:1 맞교환·삼각·사각 연쇄·다주간·블록 통째 이동 등
// 가능한 모든 교체 경우의 수를 전수 탐색해 품질 점수순으로 정렬 반환합니다.
func (s *Service) ExploreSwapOptions(req protocol.ExploreSwapsRequest) (protocol.ExploreSwapsResponse, error) {
	if strings.TrimSpace(req.SourceCellID) == "" {
		return protocol.ExploreSwapsResponse{}, errors.New("탐색할 원본 수업 셀 ID가 필요합니다")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 80
	}
	if req.MaxCrossWeeks <= 0 {
		req.MaxCrossWeeks = 4
	}
	scope := strings.TrimSpace(req.Scope)
	var baseMon string
	var cells []protocol.TimetableCell
	var err error
	if req.WeekStart != "" && !strings.EqualFold(req.WeekStart, "BASE") {
		baseMon = NormalizeMonday(req.WeekStart)
		cells, err = s.ListWeekCells(baseMon)
	} else {
		cells, err = s.ListCells()
	}
	if err != nil {
		return protocol.ExploreSwapsResponse{}, err
	}

	var src *protocol.TimetableCell
	for i := range cells {
		if cells[i].ID == req.SourceCellID {
			src = &cells[i]
			break
		}
	}
	if src == nil {
		return protocol.ExploreSwapsResponse{}, errors.New("원본 수업 셀을 찾을 수 없습니다")
	}

	teachersList, _ := database.LoadCollection[protocol.Teacher](s.store, "teachers")
	tMap := make(map[string]protocol.Teacher, len(teachersList))
	for _, t := range teachersList {
		tMap[t.ID] = t
	}
	events, _ := database.LoadCollection[protocol.SchoolEvent](s.store, "school_events")
	constraints, _ := s.ListConstraints()
	cfg, _ := s.GetSchoolConfig()

	resp := protocol.ExploreSwapsResponse{
		SourceCell: *src,
		Scope:      scope,
		Counts:     map[string]int{"MOVE": 0, "SWAP": 0, "TRIANGLE": 0, "QUAD": 0, "CROSS_WEEK": 0, "BLOCK_MOVE": 0},
	}
	if src.IsLocked || src.BlockedBy != "" {
		return resp, nil
	}

	var all []protocol.SwapOption
	seenSig := make(map[string]bool)
	addOpt := func(opt protocol.SwapOption) {
		sig := buildOptionSignature(opt)
		if seenSig[sig] {
			return
		}
		seenSig[sig] = true
		opt.OptionID = fmt.Sprintf("opt-%s-%d", strings.ToLower(opt.OptionType), len(all)+1)
		opt.Scope = scope
		opt.Score = scoreSwapOption(opt, cells)
		resp.Counts[opt.OptionType]++
		all = append(all, opt)
	}

	// 1. 고교학점제 선택블록 / 2연강 블록 통째 이동 탐색
	if req.IncludeBlocks || src.ElectiveGroup != "" || src.BlockGroupID != "" {
		for d := 1; d <= 5; d++ {
			for p := 1; p <= 7; p++ {
				if d == src.DayOfWeek && p == src.Period {
					continue
				}
				evRes := evaluateWholeBlockMove(*src, protocol.EvaluateMoveRequest{
					SourceCellID: src.ID, TargetDay: d, TargetPeriod: p, TargetDate: baseMon, MoveWholeBlock: true,
				}, cells, tMap, events, constraints, cfg)
				if evRes.Signal == "GREEN" && len(evRes.ChainSteps) > 0 {
					var warns []string
					for _, st := range evRes.ChainSteps {
						for _, sc := range cells {
							if sc.ID == st.CellID {
								if n := softNotesForSlot(sc, st.ToDay, st.ToPeriod, constraints, tMap); n != "" {
									warns = append(warns, n)
								}
							}
						}
					}
					addOpt(protocol.SwapOption{
						OptionType:     "BLOCK_MOVE",
						Title:          fmt.Sprintf("블록 통째 이동 (%s %d교시, %d건)", dayNames[d], p, len(evRes.ChainSteps)),
						Description:    evRes.Reason,
						Complexity:     len(evRes.ChainSteps),
						SoftWarnings:   warns,
						Steps:          evRes.ChainSteps,
						BlockCellCount: len(evRes.ChainSteps),
					})
				}
			}
		}
	}

	// 선택과목·연강 단독 이동이 아닌 일반 수업 전수 탐색 (MOVE, SWAP, TRIANGLE, QUAD)
	if src.ElectiveGroup == "" && src.BlockGroupID == "" {
		exploreSingleClassSwaps(src, baseMon, cells, tMap, events, constraints, cfg, addOpt)
	}

	// 2. 다주간(+1 ~ MaxCrossWeeks) 맞교환 탐색
	if req.IncludeCrossWk && src.ElectiveGroup == "" && src.BlockGroupID == "" {
		curMon := baseMon
		if curMon == "" {
			curMon = NormalizeMonday("")
		}
		srcDate := src.LessonDate
		if srcDate == "" {
			srcDate = DateForWeekday(curMon, src.DayOfWeek)
		}
		reqList, _ := s.ListRequests()
		srcWkCells := buildEffectiveWeekCells(curMon, cells, tMap, reqList, events)
		var effSrc protocol.TimetableCell
		for _, wc := range srcWkCells {
			if wc.ID == src.ID {
				effSrc = wc
				break
			}
		}
		if effSrc.ID == "" {
			effSrc = *src
			effSrc.LessonDate = srcDate
		}
		for w := 1; w <= req.MaxCrossWeeks; w++ {
			tMon := AddWeeks(curMon, w)
			tWkCells := buildEffectiveWeekCells(tMon, cells, tMap, reqList, events)
			cands := findCrossWeekForCell(effSrc, srcWkCells, tWkCells, tMap, events, constraints, false)
			for _, cw := range cands {
				addOpt(protocol.SwapOption{
					OptionType:  "CROSS_WEEK",
					Title:       fmt.Sprintf("+%d주(%s) %s %d교시 맞교환 (%s)", w, shortMD(cw.TargetDate), dayNames[cw.TargetCell.DayOfWeek], cw.TargetCell.Period, cw.TargetCell.Subject),
					Description: cw.Reason,
					Complexity:  len(cw.ChainSteps),
					WeekDelta:   w,
					SourceDate:  srcDate,
					TargetDate:  cw.TargetDate,
					PartnerID:   cw.TargetCell.TeacherID,
					PartnerName: teacherName(cw.TargetCell.TeacherID, tMap),
					Steps:       cw.ChainSteps,
				})
			}
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		if all[i].Complexity != all[j].Complexity {
			return all[i].Complexity < all[j].Complexity
		}
		if all[i].WeekDelta != all[j].WeekDelta {
			return all[i].WeekDelta < all[j].WeekDelta
		}
		return all[i].Title < all[j].Title
	})
	resp.TotalCount = len(all)
	if len(all) > req.MaxResults {
		all = all[:req.MaxResults]
	}
	resp.Options = all
	return resp, nil
}

func exploreSingleClassSwaps(
	src *protocol.TimetableCell,
	weekMon string,
	cells []protocol.TimetableCell,
	tMap map[string]protocol.Teacher,
	events []protocol.SchoolEvent,
	constraints []protocol.TeacherConstraint,
	cfg protocol.SchoolConfig,
	addOpt func(protocol.SwapOption),
) {
	grade := parseGrade(src.ClassName, src.Grade)
	slotOcc := make(map[int]protocol.TimetableCell)
	for _, c := range cells {
		if c.ClassName == src.ClassName {
			slotOcc[c.DayOfWeek*10+c.Period] = c
		}
	}
	maxPForDay := func(d int) int {
		if mp := cfg.DailyPeriodLimits[grade][d]; mp > 0 {
			return mp
		}
		return 7
	}

	// (1) 빈 교시 단순 이동 & (2) 빈 교시 경유 2단계 이동
	for d := 1; d <= 5; d++ {
		for p := 1; p <= maxPForDay(d); p++ {
			if _, occ := slotOcc[d*10+p]; occ {
				continue
			}
			if slotConflictReasonWithConfig(*src, d, p, weekMon, map[string]bool{src.ID: true}, cells, tMap, events, constraints, cfg) == "" {
				var warns []string
				if n := softNotesForSlot(*src, d, p, constraints, tMap); n != "" {
					warns = append(warns, n)
				}
				addOpt(protocol.SwapOption{
					OptionType:   "MOVE",
					Title:        fmt.Sprintf("빈 교시 이동 (%s %d교시)", dayNames[d], p),
					Description:  fmt.Sprintf("%s %d교시 빈 칸으로 %s(%s) 수업 즉시 이동", dayNames[d], p, src.Subject, teacherName(src.TeacherID, tMap)),
					Complexity:   1,
					SoftWarnings: warns,
					Steps:        []protocol.ChainSwapStep{buildChainStep(1, *src, src.DayOfWeek, src.Period, d, p, tMap)},
				})
			} else if _, _, steps := findEmptyTargetChainWithConfig(src, d, p, weekMon, cells, tMap, events, constraints, cfg); len(steps) > 0 {
				addOpt(protocol.SwapOption{
					OptionType:  "TRIANGLE",
					Title:       fmt.Sprintf("2단계 빈칸 연쇄 (%s %d교시)", dayNames[d], p),
					Description: fmt.Sprintf("기존 수업을 빈 %s %d교시로 옮긴 뒤 연쇄 이동", dayNames[d], p),
					Complexity:  len(steps),
					Steps:       steps,
				})
			}
		}
	}

	// (3) 1:1 맞교환 + 3단계 삼각 + 4단계 사각 연쇄 전수 탐색
	classCells := sameClassCandidates(src.ClassName, cells, src.ID)
	// 사전 필터: 각 셀 c가 (src.DayOfWeek, src.Period)로 올 수 있는지 여부 및 c -> d 후보 사전 구축
	for _, tgt := range classCells {
		ign2 := map[string]bool{src.ID: true, tgt.ID: true}
		if slotConflictReasonWithConfig(*src, tgt.DayOfWeek, tgt.Period, weekMon, ign2, cells, tMap, events, constraints, cfg) == "" &&
			slotConflictReasonWithConfig(tgt, src.DayOfWeek, src.Period, weekMon, ign2, cells, tMap, events, constraints, cfg) == "" {
			var warns []string
			if n := softNotesForSlot(*src, tgt.DayOfWeek, tgt.Period, constraints, tMap); n != "" {
				warns = append(warns, n)
			}
			if n := softNotesForSlot(tgt, src.DayOfWeek, src.Period, constraints, tMap); n != "" {
				warns = append(warns, n)
			}
			addOpt(protocol.SwapOption{
				OptionType:   "SWAP",
				Title:        fmt.Sprintf("1:1 맞교환 (%s %d교시 %s · %s)", dayNames[tgt.DayOfWeek], tgt.Period, tgt.Subject, teacherName(tgt.TeacherID, tMap)),
				Description:  fmt.Sprintf("%s(%s) ↔ %s(%s)", src.Subject, teacherName(src.TeacherID, tMap), tgt.Subject, teacherName(tgt.TeacherID, tMap)),
				Complexity:   2,
				PartnerID:    tgt.TeacherID,
				PartnerName:  teacherName(tgt.TeacherID, tMap),
				SoftWarnings: warns,
				Steps: []protocol.ChainSwapStep{
					buildChainStep(1, *src, src.DayOfWeek, src.Period, tgt.DayOfWeek, tgt.Period, tMap),
					buildChainStep(2, tgt, tgt.DayOfWeek, tgt.Period, src.DayOfWeek, src.Period, tMap),
				},
			})
		}

		// 3단계 삼각 순환 (src -> mid -> tgt -> src)
		for _, mid := range classCells {
			if mid.ID == tgt.ID {
				continue
			}
			ign3 := map[string]bool{src.ID: true, tgt.ID: true, mid.ID: true}
			if slotConflictReasonWithConfig(*src, mid.DayOfWeek, mid.Period, weekMon, ign3, cells, tMap, events, constraints, cfg) == "" &&
				slotConflictReasonWithConfig(mid, tgt.DayOfWeek, tgt.Period, weekMon, ign3, cells, tMap, events, constraints, cfg) == "" &&
				slotConflictReasonWithConfig(tgt, src.DayOfWeek, src.Period, weekMon, ign3, cells, tMap, events, constraints, cfg) == "" {
				addOpt(protocol.SwapOption{
					OptionType:  "TRIANGLE",
					Title:       fmt.Sprintf("삼각 교환 (%s → %s → %s)", dayNames[mid.DayOfWeek], dayNames[tgt.DayOfWeek], dayNames[src.DayOfWeek]),
					Description: fmt.Sprintf("%s→%s→%s 3단계 순환", src.Subject, mid.Subject, tgt.Subject),
					Complexity:  3,
					PartnerID:   tgt.TeacherID,
					PartnerName: teacherName(tgt.TeacherID, tMap),
					Steps: []protocol.ChainSwapStep{
						buildChainStep(1, *src, src.DayOfWeek, src.Period, mid.DayOfWeek, mid.Period, tMap),
						buildChainStep(2, mid, mid.DayOfWeek, mid.Period, tgt.DayOfWeek, tgt.Period, tMap),
						buildChainStep(3, tgt, tgt.DayOfWeek, tgt.Period, src.DayOfWeek, src.Period, tMap),
					},
				})
			}
		}
	}

	// 4단계 사각 순환 탐색 (폭발 방지를 위해 최대 24건까지 대표 사각 경로 수집)
	quadFound := 0
	for _, m1 := range classCells {
		if quadFound >= 24 {
			break
		}
		ignS := map[string]bool{src.ID: true, m1.ID: true}
		if slotConflictReasonWithConfig(*src, m1.DayOfWeek, m1.Period, weekMon, ignS, cells, tMap, events, constraints, cfg) != "" {
			continue
		}
		for _, tgt := range classCells {
			if quadFound >= 24 || tgt.ID == m1.ID {
				continue
			}
			for _, m2 := range classCells {
				if m2.ID == m1.ID || m2.ID == tgt.ID {
					continue
				}
				ign4 := map[string]bool{src.ID: true, m1.ID: true, m2.ID: true, tgt.ID: true}
				if slotConflictReasonWithConfig(*src, m1.DayOfWeek, m1.Period, weekMon, ign4, cells, tMap, events, constraints, cfg) == "" &&
					slotConflictReasonWithConfig(m1, m2.DayOfWeek, m2.Period, weekMon, ign4, cells, tMap, events, constraints, cfg) == "" &&
					slotConflictReasonWithConfig(m2, tgt.DayOfWeek, tgt.Period, weekMon, ign4, cells, tMap, events, constraints, cfg) == "" &&
					slotConflictReasonWithConfig(tgt, src.DayOfWeek, src.Period, weekMon, ign4, cells, tMap, events, constraints, cfg) == "" {
					addOpt(protocol.SwapOption{
						OptionType:  "QUAD",
						Title:       fmt.Sprintf("사각 연쇄 (%s → %s → %s → %s)", src.Subject, m1.Subject, m2.Subject, tgt.Subject),
						Description: fmt.Sprintf("4단계 순환 (%s %d교시 경유)", dayNames[tgt.DayOfWeek], tgt.Period),
						Complexity:  4,
						PartnerID:   tgt.TeacherID,
						PartnerName: teacherName(tgt.TeacherID, tMap),
						Steps: []protocol.ChainSwapStep{
							buildChainStep(1, *src, src.DayOfWeek, src.Period, m1.DayOfWeek, m1.Period, tMap),
							buildChainStep(2, m1, m1.DayOfWeek, m1.Period, m2.DayOfWeek, m2.Period, tMap),
							buildChainStep(3, m2, m2.DayOfWeek, m2.Period, tgt.DayOfWeek, tgt.Period, tMap),
							buildChainStep(4, tgt, tgt.DayOfWeek, tgt.Period, src.DayOfWeek, src.Period, tMap),
						},
					})
					quadFound++
					break
				}
			}
		}
	}
}

func buildOptionSignature(opt protocol.SwapOption) string {
	var b strings.Builder
	b.WriteString(opt.OptionType)
	b.WriteString("|")
	b.WriteString(opt.TargetDate)
	for _, st := range opt.Steps {
		fmt.Fprintf(&b, "|%s:%d-%d", st.CellID, st.ToDay, st.ToPeriod)
	}
	return b.String()
}

func scoreSwapOption(opt protocol.SwapOption, cells []protocol.TimetableCell) int {
	score := 100
	switch opt.OptionType {
	case "MOVE":
		score -= 0
	case "SWAP":
		score -= 5
	case "BLOCK_MOVE":
		score -= 6
		if opt.Complexity > len(cells) {
			score -= 4
		}
	case "CROSS_WEEK":
		score -= 10 + opt.WeekDelta*4
	case "TRIANGLE":
		score -= 14
	case "QUAD":
		score -= 24
	}
	score -= len(opt.SoftWarnings) * 18
	if score < 1 {
		score = 1
	}
	return score
}
