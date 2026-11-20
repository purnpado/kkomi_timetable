package internal_exam

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/database"
)

// buildUnavailableTeachersForDate는 시험 당일 순회교사 비출강일, 확정된 외부시험 파견,
// 확정된 행사 인솔 출장을 조회해 교사 ID별 제외 사유 맵을 반환합니다.
func buildUnavailableTeachersForDate(store *database.Store, dateStr string) map[string]string {
	res := make(map[string]string)
	trimmed := strings.TrimSpace(dateStr)
	if trimmed == "" || store == nil {
		return res
	}

	if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
		wd := int(parsed.Weekday())
		if wd >= 1 && wd <= 5 {
			teachers, _ := database.LoadCollection[protocol.Teacher](store, "teachers")
			for _, t := range teachers {
				if len(t.ItinerantDays) > 0 && !slices.Contains(t.ItinerantDays, wd) {
					res[t.ID] = "순회교사 비출강일"
				}
			}
		}
	}

	extNotices, _ := database.LoadCollection[protocol.ExternalExamNotice](store, "external_exams")
	for _, ex := range extNotices {
		if ex.IsConfirmed && ex.ExamDate == trimmed {
			for _, tid := range ex.ConfirmedIDs {
				if res[tid] == "" {
					res[tid] = fmt.Sprintf("외부시험 파견(%s)", ex.Title)
				}
			}
		}
	}

	events, _ := database.LoadCollection[protocol.SchoolEvent](store, "school_events")
	for _, ev := range events {
		if ev.Status == protocol.StatusApproved && ev.LinkType == protocol.EventLinkTeacherTrip && ev.EventDate == trimmed {
			for _, tid := range ev.LeadTeachers {
				if res[tid] == "" {
					res[tid] = fmt.Sprintf("학사행사 출장(%s)", ev.Title)
				}
			}
		}
	}
	return res
}

// validateSubstituteTeacher는 당일 돌발 결원 긴급 대타 교체 시 교사 자격·중복·담임·과목·부재 여부를 검증합니다.
func validateSubstituteTeacher(
	target protocol.ExamSlot,
	newT protocol.Teacher,
	allSlots []protocol.ExamSlot,
	unavail map[string]string,
) error {
	if newT.IsParentHonorary || protocol.IsSystemAdminAccount(newT) {
		return fmt.Errorf("%s 계정은 교내 시험감독 대타로 배정할 수 없습니다", newT.Name)
	}
	if exempt, reason := IsEffectiveExamExempt(newT); exempt {
		return fmt.Errorf("%s 선생님은 감독 제외 대상(%s)입니다", newT.Name, reason)
	}
	if rsn := unavail[newT.ID]; rsn != "" {
		return fmt.Errorf("%s 선생님은 시험일(%s)에 배정 불가 상태입니다: %s", newT.Name, target.ExamDate, rsn)
	}
	for _, sl := range allSlots {
		if sl.ID == target.ID || sl.Period != target.Period {
			continue
		}
		if target.ExamDate == "" || sl.ExamDate == "" || sl.ExamDate == target.ExamDate {
			if sl.TeacherID == newT.ID {
				return fmt.Errorf("%s 선생님은 동일 교시(%d교시)에 이미 '%s' 감독이 배정되어 있습니다", newT.Name, sl.Period, sl.RoomName)
			}
		}
	}
	roomPrefix := extractRoomBaseClass(target.RoomName)
	if target.SlotRole != "CORRIDOR" && roomPrefix != "" && strings.EqualFold(strings.TrimSpace(newT.HomeroomClass), roomPrefix) {
		return fmt.Errorf("담임교사(%s)는 본인 학급(%s) 고사장에 배정할 수 없습니다", newT.Name, roomPrefix)
	}
	if target.SlotRole != "CORRIDOR" && roomOrSpecMatchesTeacherSubject(target.RoomName, target.Subject, newT.Subject) {
		return fmt.Errorf("시험 과목(%s) 담당교사(%s)는 고사장 감독으로 배정할 수 없습니다", target.Subject, newT.Name)
	}
	return nil
}
