package internal_exam

import (
	"fmt"
	"sort"
	"strings"

	"kkomi-timetable/pkg/protocol"
)

// IsEffectiveExamExempt는 교사가 특수반 전담 또는 학교가 명시 지정한 감독 제외 대상인지 판정합니다.
// 참고: 별도 고사실(독감·감염병)은 전담 제외 보직이 아니라 개설 시 일반 교사가 순환 감독합니다.
func IsEffectiveExamExempt(t protocol.Teacher) (bool, string) {
	if t.InternalExamExempt {
		if strings.TrimSpace(t.InternalExamExemptReason) != "" {
			return true, strings.TrimSpace(t.InternalExamExemptReason)
		}
		return true, "특수보직·학교 지정 교내 감독 제외"
	}
	if strings.EqualFold(strings.TrimSpace(t.InternalExamExemptReason), "제외해제") {
		return false, ""
	}
	if protocol.IsSpecialEducationTeacher(t) {
		return true, "특수반(개별고사장 전담)"
	}
	joined := strings.Join([]string{t.Position, t.Subject, t.InternalExamExemptReason}, " ")
	for _, kw := range []string{"특수반", "특수학급", "특수교육", "특수"} {
		if strings.Contains(joined, kw) {
			return true, "특수반(개별고사장 전담)"
		}
	}
	return false, ""
}

func isIsolatedExamRoom(roomLabel string) bool {
	for _, kw := range []string{"별도고사실", "별도 고사실", "별독고사실", "별독 고사실", "특수학급고사실", "특수학급 고사실", "특수반고사실", "특수반 고사실", "특수고사실", "특수 고사실", "독감", "감염병"} {
		if strings.Contains(roomLabel, kw) {
			return true
		}
	}
	return false
}

func isSpecialClassExamRoom(roomLabel string) bool {
	for _, kw := range []string{"특수학급고사실", "특수학급 고사실", "특수반고사실", "특수반 고사실", "특수고사실", "특수 고사실", "특수학급"} {
		if strings.Contains(roomLabel, kw) {
			return true
		}
	}
	return false
}

func isSpecialClassSupervisor(t protocol.Teacher) bool {
	if t.IsParentHonorary || protocol.IsSystemAdminAccount(t) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(t.InternalExamExemptReason), "제외해제") {
		return false
	}
	return protocol.IsSpecialEducationTeacher(t) || strings.Contains(t.InternalExamExemptReason, "특수")
}

// pickSpecialTeacherForRoom은 특수학급고사실 개설 시 가용한 특수교사를 최우선 선발합니다.
// 1인 특수교사 학교에서도 전 교시 특수학급 전담 배정이 끊기지 않도록 연강/일일상한은 소프트 가중치로만 반영합니다.
func pickSpecialTeacherForRoom(
	specialTeachers []protocol.Teacher,
	load map[string]*roleLoad,
	ctx rolePickCtx,
) *protocol.Teacher {
	pk := fmt.Sprintf("%s#%d", ctx.examDate, ctx.period)
	used := ctx.usage.usedPerPeriod[pk]
	var candidates []protocol.Teacher
	for _, t := range specialTeachers {
		if used[t.ID] || isUnavailableOnDate(ctx.unavailByDate, ctx.examDate, t.ID) {
			continue
		}
		candidates = append(candidates, t)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		pi, pj := ctx.penalty(candidates[i].ID, "", candidates[i]), ctx.penalty(candidates[j].ID, "", candidates[j])
		if pi != pj {
			return pi < pj
		}
		li, lj := load[candidates[i].ID], load[candidates[j].ID]
		if li != nil && lj != nil && li.Total != lj.Total {
			return li.Total < lj.Total
		}
		di, dj := ctx.usage.dailyCount[ctx.examDate][candidates[i].ID], ctx.usage.dailyCount[ctx.examDate][candidates[j].ID]
		if di != dj {
			return di < dj
		}
		return candidates[i].ID < candidates[j].ID
	})
	picked := candidates[0]
	return &picked
}
