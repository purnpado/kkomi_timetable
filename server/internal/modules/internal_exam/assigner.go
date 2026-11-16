package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// ExamPeriodSpec은 지필고사 교시별 시험 과목, 과목별 시험시간 및 고사실 정의입니다.
type ExamPeriodSpec struct {
	Period          int      `json:"period"`
	Grade           int      `json:"grade"`
	Subject         string   `json:"subject"`
	StartTime       string   `json:"startTime"`       // 예: "08:50"
	EndTime         string   `json:"endTime"`         // 예: "09:35"
	DurationMinutes int      `json:"durationMinutes"` // 예: 45
	ClassRooms      []string `json:"classRooms"`      // 예: ["1-1", "1-2"]
}

// AssignSlots는 핵심 배제 규칙을 적용해 지필고사 감독 배정표(정/부감독, 복도감독, 학부모 명예감독)를 생성합니다.
// 1) 담임교사 자기 반 감독 원천 제외 (상피제에 따른 교사 자녀 학년 배제는 미적용)
// 2) 해당 교시 시험 과목 담당교사는 고사장 입실(정/부감독) 100% 배제
// 3) SubjectTeacherPolicy == CORRIDOR_FIRST(기본값)이면 담당과목 교사를 해당 학년 층 복도감독으로 최우선 배치
// 4) 학부모 명예감독 On 시 자녀 학년/반(ParentOfStudentClass) 배제
func AssignSlots(cfg protocol.ExamConfig, specs []ExamPeriodSpec, teachers []protocol.Teacher) []protocol.ExamSlot {
	var slots []protocol.ExamSlot
	loadCount := make(map[string]int, len(teachers))
	for _, t := range teachers {
		loadCount[t.ID] = t.InternalExamCount
	}

	// 정규 교사 / 학부모 명예감독 분리
	var regularTeachers []protocol.Teacher
	var honoraryParents []protocol.Teacher
	for _, t := range teachers {
		if t.IsParentHonorary {
			honoraryParents = append(honoraryParents, t)
		} else {
			regularTeachers = append(regularTeachers, t)
		}
	}

	for _, spec := range specs {
		usedInPeriod := make(map[string]bool)

		// 1단계: 해당 교시 시험 과목 담당교사 처리 (CORRIDOR_FIRST면 해당 학년 층 복도감독 최우선 배정)
		corridorAssigned := false
		if cfg.SubjectTeacherPolicy == "" || cfg.SubjectTeacherPolicy == protocol.SubjectModeCorridorFirst {
			for _, t := range regularTeachers {
				if strings.EqualFold(strings.TrimSpace(t.Subject), strings.TrimSpace(spec.Subject)) && !usedInPeriod[t.ID] {
					usedInPeriod[t.ID] = true
					loadCount[t.ID]++
					slots = append(slots, protocol.ExamSlot{
						ID:              fmt.Sprintf("slot-%s-p%d-g%d-corr-%s", cfg.ID, spec.Period, spec.Grade, t.ID),
						ExamID:          cfg.ID,
						ExamDate:        cfg.ExamDate,
						Period:          spec.Period,
						StartTime:       spec.StartTime,
						EndTime:         spec.EndTime,
						DurationMinutes: spec.DurationMinutes,
						Grade:           spec.Grade,
						Subject:         spec.Subject,
						SlotRole:        "CORRIDOR",
						RoomName:        fmt.Sprintf("%d학년 층 복도감독 (%s 과목 질의응답)", spec.Grade, spec.Subject),
						TeacherID:       t.ID,
						TeacherName:     t.Name,
						Note:            "담당과목 시험 교시 복도감독 우선배치",
					})
					corridorAssigned = true
					break // 학년별 핵심 복도감독 1인 우선 배치 후 나머지 과목교사도 고사실 배제 유지
				}
			}
		}

		// 만약 담당 과목 교사가 없어 복도감독이 비었거나, 추가 복도감독 필요 시 공강 교사 중 1인 배치
		if !corridorAssigned {
			if cand := pickBestRegularTeacher(regularTeachers, usedInPeriod, loadCount, "", spec.Subject, true); cand != nil {
				usedInPeriod[cand.ID] = true
				loadCount[cand.ID]++
				slots = append(slots, protocol.ExamSlot{
					ID:              fmt.Sprintf("slot-%s-p%d-g%d-corr-%s", cfg.ID, spec.Period, spec.Grade, cand.ID),
					ExamID:          cfg.ID,
					ExamDate:        cfg.ExamDate,
					Period:          spec.Period,
					StartTime:       spec.StartTime,
					EndTime:         spec.EndTime,
					DurationMinutes: spec.DurationMinutes,
					Grade:           spec.Grade,
					Subject:         spec.Subject,
					SlotRole:        "CORRIDOR",
					RoomName:        fmt.Sprintf("%d학년 층 복도감독", spec.Grade),
					TeacherID:       cand.ID,
					TeacherName:     cand.Name,
					Note:            "복도감독 배치",
				})
			}
		}

		// 2단계: 각 학급 고사장별 정감독(MAIN) 및 선택적 부감독(SUB) / 학부모 명예감독(HONORARY) 배정
		for _, room := range spec.ClassRooms {
			// 정감독 배정 (담임 자기반 제외 + 해당 교시 시험과목 담당교사 제외)
			if mainT := pickBestRegularTeacher(regularTeachers, usedInPeriod, loadCount, room, spec.Subject, false); mainT != nil {
				usedInPeriod[mainT.ID] = true
				loadCount[mainT.ID]++
				slots = append(slots, protocol.ExamSlot{
					ID:              fmt.Sprintf("slot-%s-p%d-%s-main", cfg.ID, spec.Period, room),
					ExamID:          cfg.ID,
					ExamDate:        cfg.ExamDate,
					Period:          spec.Period,
					StartTime:       spec.StartTime,
					EndTime:         spec.EndTime,
					DurationMinutes: spec.DurationMinutes,
					Grade:           spec.Grade,
					Subject:         spec.Subject,
					SlotRole:        "MAIN",
					RoomName:        room + " 고사장",
					TeacherID:       mainT.ID,
					TeacherName:     mainT.Name,
				})
			}

			// 부감독 또는 학부모 명예감독 배정 (DeputyMode 또는 HonoraryParentOn)
			if cfg.HonoraryParentOn {
				if parent := pickHonoraryParent(honoraryParents, usedInPeriod, room, spec.Grade); parent != nil {
					usedInPeriod[parent.ID] = true
					slots = append(slots, protocol.ExamSlot{
						ID:              fmt.Sprintf("slot-%s-p%d-%s-hon", cfg.ID, spec.Period, room),
						ExamID:          cfg.ID,
						ExamDate:        cfg.ExamDate,
						Period:          spec.Period,
						StartTime:       spec.StartTime,
						EndTime:         spec.EndTime,
						DurationMinutes: spec.DurationMinutes,
						Grade:           spec.Grade,
						Subject:         spec.Subject,
						SlotRole:        "HONORARY",
						RoomName:        room + " 고사장",
						TeacherID:       parent.ID,
						TeacherName:     parent.Name,
						Note:            "학부모 명예감독 (자녀 학년/반 자동 배제)",
					})
					continue
				}
			}
			if cfg.DeputyMode {
				if subT := pickBestRegularTeacher(regularTeachers, usedInPeriod, loadCount, room, spec.Subject, false); subT != nil {
					usedInPeriod[subT.ID] = true
					loadCount[subT.ID]++
					slots = append(slots, protocol.ExamSlot{
						ID:              fmt.Sprintf("slot-%s-p%d-%s-sub", cfg.ID, spec.Period, room),
						ExamID:          cfg.ID,
						ExamDate:        cfg.ExamDate,
						Period:          spec.Period,
						StartTime:       spec.StartTime,
						EndTime:         spec.EndTime,
						DurationMinutes: spec.DurationMinutes,
						Grade:           spec.Grade,
						Subject:         spec.Subject,
						SlotRole:        "SUB",
						RoomName:        room + " 고사장",
						TeacherID:       subT.ID,
						TeacherName:     subT.Name,
					})
				}
			}
		}
	}

	return slots
}

func pickBestRegularTeacher(
	teachers []protocol.Teacher,
	used map[string]bool,
	load map[string]int,
	forbiddenHomeroom string,
	forbiddenSubject string,
	allowSubject bool,
) *protocol.Teacher {
	var candidates []protocol.Teacher
	for _, t := range teachers {
		if used[t.ID] {
			continue
		}
		// 핵심 배제 1: 담임교사 자기 반 감독 원천 제외
		if forbiddenHomeroom != "" && strings.EqualFold(strings.TrimSpace(t.HomeroomClass), strings.TrimSpace(forbiddenHomeroom)) {
			continue
		}
		// 핵심 배제 2: 담당 과목 시험 교시 고사장 입실 배제
		if !allowSubject && strings.EqualFold(strings.TrimSpace(t.Subject), strings.TrimSpace(forbiddenSubject)) {
			continue
		}
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if load[candidates[i].ID] != load[candidates[j].ID] {
			return load[candidates[i].ID] < load[candidates[j].ID]
		}
		// 비담임/평교사 우선 정렬로 부담 균형
		if (candidates[i].HomeroomClass == "") != (candidates[j].HomeroomClass == "") {
			return candidates[i].HomeroomClass == ""
		}
		return candidates[i].ID < candidates[j].ID
	})
	picked := candidates[0]
	return &picked
}

func pickHonoraryParent(parents []protocol.Teacher, used map[string]bool, roomClass string, grade int) *protocol.Teacher {
	gradePrefix := fmt.Sprintf("%d-", grade)
	for i := range parents {
		p := parents[i]
		if used[p.ID] {
			continue
		}
		childClass := strings.TrimSpace(p.ParentOfStudentClass)
		// 학부모 자녀가 속한 반 및 동일 학년 배제
		if childClass != "" && (strings.EqualFold(childClass, roomClass) || strings.HasPrefix(childClass, gradePrefix)) {
			continue
		}
		return &p
	}
	return nil
}
