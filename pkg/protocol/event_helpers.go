package protocol

import (
	"strings"
	"time"
)

// EventConflict는 행사 등록·수정 시 동일 장소 또는 동일 학년·교시 중복 경고를 표현합니다.
type EventConflict struct {
	Type       string `json:"type"`       // LOCATION, GRADE_PERIOD
	Date       string `json:"date"`       // 충돌 발생 일자 (YYYY-MM-DD)
	EventID    string `json:"eventId"`    // 기존 충돌 행사 ID
	EventTitle string `json:"eventTitle"` // 기존 충돌 행사명
	Message    string `json:"message"`
}

// BellPresetDef는 단축수업·오전행사 시종시간 변경 원클릭 프리셋 정의입니다.
type BellPresetDef struct {
	Key         string       `json:"key"`         // SHORT_40, SHORT_45, MORNING_EVENT
	Name        string       `json:"name"`        // 표시 명칭
	Description string       `json:"description"` // 요약 설명
	Periods     []BellPeriod `json:"periods"`
}

// EventEndDate는 다일 연속 행사의 종료 일자를 반환합니다. 미지정 또는 역전 시 시작 일자를 반환합니다.
func EventEndDate(ev SchoolEvent) string {
	start := strings.TrimSpace(ev.EventDate)
	end := strings.TrimSpace(ev.EndDate)
	if end == "" || end < start {
		return start
	}
	return end
}

// EventDates는 시작 일자부터 종료 일자까지(최대 60일) YYYY-MM-DD 일자 목록을 반환합니다.
func EventDates(ev SchoolEvent) []string {
	start := strings.TrimSpace(ev.EventDate)
	if start == "" {
		return nil
	}
	end := EventEndDate(ev)
	st, err1 := time.Parse("2006-01-02", start)
	et, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil || et.Before(st) {
		return []string{start}
	}
	var out []string
	for cur := st; !cur.After(et) && len(out) < 60; cur = cur.AddDate(0, 0, 1) {
		out = append(out, cur.Format("2006-01-02"))
	}
	return out
}

// EventCoversDate는 지정 일자가 행사의 시작~종료 구간에 포함되는지 판정합니다.
func EventCoversDate(ev SchoolEvent, dateStr string) bool {
	target := strings.TrimSpace(dateStr)
	start := strings.TrimSpace(ev.EventDate)
	if target == "" || start == "" {
		return false
	}
	end := EventEndDate(ev)
	return target >= start && target <= end
}

// EventOverlapsRange는 행사 구간이 [startDate, endDate] 범위와 하루 이상 겹치는지 확인합니다.
func EventOverlapsRange(ev SchoolEvent, startDate, endDate string) bool {
	start := strings.TrimSpace(ev.EventDate)
	sRange := strings.TrimSpace(startDate)
	eRange := strings.TrimSpace(endDate)
	if start == "" || sRange == "" || eRange == "" {
		return false
	}
	end := EventEndDate(ev)
	return start <= eRange && end >= sRange
}

// EventBlocksClasses는 확정된 수업블록 또는 공휴일/휴업일 행사 여부를 반환합니다.
func EventBlocksClasses(ev SchoolEvent) bool {
	return ev.Status == StatusApproved &&
		(ev.LinkType == EventLinkClassBlock || ev.LinkType == EventLinkHoliday)
}
