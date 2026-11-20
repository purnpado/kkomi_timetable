package internal_exam

import (
	"fmt"
	"sort"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// ElectiveRoomItem은 고교학점제 선택과목 분반 또는 교과교실 기반 시험 고사실 항목입니다.
type ElectiveRoomItem struct {
	Label     string `json:"label"`
	Subject   string `json:"subject"`
	Grade     int    `json:"grade"`
	GroupName string `json:"groupName"`
}

// ExamRoomCatalog는 학급 고사실, 교과교실, 고교선택과목 고사실 및 별도고사실 목록입니다.
type ExamRoomCatalog struct {
	GradeClassRooms map[int][]string   `json:"gradeClassRooms"`
	SpecialRooms    []string           `json:"specialRooms"`
	ElectiveRooms   []ElectiveRoomItem `json:"electiveRooms"`
	IsolatedRooms   []string           `json:"isolatedRooms"`
}

// BuildRoomCatalog는 학교 기본설정·시수표·시간표에서 정규반, 교과교실, 고교선택과목 고사실 및 별도 고사실 목록을 수집합니다.
func (s *Service) BuildRoomCatalog() ExamRoomCatalog {
	cat := ExamRoomCatalog{
		GradeClassRooms: map[int][]string{},
		IsolatedRooms:   []string{"별도고사실(독감·감염병)", "특수학급고사실"},
	}
	cfgs, _ := database.LoadCollection[protocol.SchoolConfig](s.store, "school_configs")
	specialSet := make(map[string]bool)
	classSets := map[int]map[string]bool{1: {}, 2: {}, 3: {}}
	for _, sc := range cfgs {
		for g, cnt := range sc.GradeClassCounts {
			if classSets[g] == nil {
				classSets[g] = map[string]bool{}
			}
			for i := 1; i <= cnt; i++ {
				classSets[g][fmt.Sprintf("%d-%d", g, i)] = true
			}
		}
		for _, rm := range sc.SpecialRooms {
			if rm.Name != "" {
				specialSet[rm.Name] = true
			}
		}
	}

	electiveSeen := make(map[string]bool)
	cells, _ := database.LoadCollection[protocol.TimetableCell](s.store, "timetable_cells")
	for _, c := range cells {
		if c.Grade >= 1 && c.Grade <= 3 && c.ClassName != "" {
			classSets[c.Grade][c.ClassName] = true
		}
		if c.Room != "" && c.Room != c.ClassName+"교실" {
			specialSet[c.Room] = true
		}
		if c.ElectiveGroup != "" || (c.Room != "" && c.Room != c.ClassName+"교실") {
			roomHost := c.Room
			if roomHost == "" {
				roomHost = c.ClassName
			}
			label := fmt.Sprintf("%s(%s)", roomHost, c.Subject)
			if !electiveSeen[label] {
				electiveSeen[label] = true
				cat.ElectiveRooms = append(cat.ElectiveRooms, ElectiveRoomItem{
					Label: label, Subject: c.Subject, Grade: c.Grade, GroupName: c.ElectiveGroup,
				})
			}
		}
	}

	loads, _ := database.LoadCollection[protocol.TeachingLoad](s.store, "teaching_loads")
	for _, ld := range loads {
		if ld.DedicatedRoom != "" {
			specialSet[ld.DedicatedRoom] = true
		}
		if ld.SimultaneousGroup != "" || ld.DedicatedRoom != "" {
			host := ld.DedicatedRoom
			if host == "" {
				host = fmt.Sprintf("%d학년분반", ld.Grade)
			}
			label := fmt.Sprintf("%s(%s)", host, ld.Subject)
			if !electiveSeen[label] {
				electiveSeen[label] = true
				cat.ElectiveRooms = append(cat.ElectiveRooms, ElectiveRoomItem{
					Label: label, Subject: ld.Subject, Grade: ld.Grade, GroupName: ld.SimultaneousGroup,
				})
			}
		}
	}

	for g := 1; g <= 3; g++ {
		for cls := range classSets[g] {
			cat.GradeClassRooms[g] = append(cat.GradeClassRooms[g], cls)
		}
		sort.Strings(cat.GradeClassRooms[g])
	}
	for rm := range specialSet {
		cat.SpecialRooms = append(cat.SpecialRooms, rm)
	}
	sort.Strings(cat.SpecialRooms)
	sort.Slice(cat.ElectiveRooms, func(i, j int) bool {
		if cat.ElectiveRooms[i].Grade != cat.ElectiveRooms[j].Grade {
			return cat.ElectiveRooms[i].Grade < cat.ElectiveRooms[j].Grade
		}
		return cat.ElectiveRooms[i].Label < cat.ElectiveRooms[j].Label
	})
	return cat
}
