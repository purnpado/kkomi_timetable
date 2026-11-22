import React, { useMemo, useState } from "react";
import { CheckCircle2, ChevronLeft, ChevronRight, Edit3, PlusCircle, Trash2, XCircle } from "lucide-react";
import {
  HOLIDAY_KIND_LABELS,
  LINK_META,
  buildMonthMatrix,
  buildWeekDates,
  eventCoversDate,
  formatEventDateRange,
  formatEventPeriods,
  formatEventTargets,
  formatLocalYMD,
  formatTeacherNames,
  parseLocalYMD,
} from "./calendarUtils";

function StatusBadge({ status }) {
  const cls = status === "APPROVED" ? "badge-green" : status === "REJECTED" ? "badge-red" : "badge-amber";
  const text = status === "APPROVED" ? "확정 반영" : status === "REJECTED" ? "반려" : "승인 대기";
  return <span className={`badge ${cls}`}>{text}</span>;
}

function EventActionButtons({ ev, user, isEventAdmin, onCommit, onEdit, onDelete }) {
  const canEdit = isEventAdmin || (ev.status === "STAGING" && ev.proposedBy === user?.name);
  return (
    <div className="no-print" style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
      {isEventAdmin && ev.status === "STAGING" && (
        <>
          <button type="button" className="btn btn-sm btn-green" onClick={() => onCommit(ev.id, true)}><CheckCircle2 size={13} /> 확정</button>
          <button type="button" className="btn btn-sm btn-danger" onClick={() => onCommit(ev.id, false)}><XCircle size={13} /> 반려</button>
        </>
      )}
      {canEdit && <button type="button" className="btn btn-sm" onClick={() => onEdit(ev)}><Edit3 size={13} /> 수정</button>}
      {isEventAdmin && <button type="button" className="btn btn-sm btn-danger" onClick={() => onDelete(ev)}><Trash2 size={13} /> 삭제</button>}
    </div>
  );
}

// MonthlyCalendarGrid는 월간 학사일정 달력과 선택한 일자의 상세 행사 패널을 표시합니다.
export function MonthlyCalendarGrid({ events, teachers, user, isEventAdmin, onCommit, onEdit, onDelete, onQuickAddDate }) {
  const todayStr = formatLocalYMD(new Date());
  const [cursorYM, setCursorYM] = useState(() => {
    const now = new Date();
    return { year: now.getFullYear(), month: now.getMonth() + 1 };
  });
  const [selectedDate, setSelectedDate] = useState(todayStr);

  const weeks = useMemo(() => buildMonthMatrix(cursorYM.year, cursorYM.month), [cursorYM]);
  const shiftMonth = (delta) => {
    setCursorYM((prev) => {
      const d = new Date(prev.year, prev.month - 1 + delta, 1);
      return { year: d.getFullYear(), month: d.getMonth() + 1 };
    });
  };
  const selectedEvents = useMemo(
    () => (events || []).filter((ev) => eventCoversDate(ev, selectedDate)),
    [events, selectedDate]
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12, flexWrap: "wrap", gap: 8 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <button type="button" className="btn btn-sm" onClick={() => shiftMonth(-1)}><ChevronLeft size={14} /></button>
            <span style={{ fontWeight: 800, fontSize: 16, fontVariantNumeric: "tabular-nums" }}>{cursorYM.year}년 {cursorYM.month}월</span>
            <button type="button" className="btn btn-sm" onClick={() => shiftMonth(1)}><ChevronRight size={14} /></button>
            <button type="button" className="btn btn-sm" onClick={() => { const n = new Date(); setCursorYM({ year: n.getFullYear(), month: n.getMonth() + 1 }); setSelectedDate(todayStr); }}>오늘</button>
          </div>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            <span className="badge badge-red">휴업일</span>
            <span className="badge badge-blue">수업블록</span>
            <span className="badge badge-amber">출장·시종</span>
            <span className="badge badge-gray">단순안내</span>
          </div>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "repeat(7, minmax(0, 1fr))", gap: 6 }}>
          {["일", "월", "화", "수", "목", "금", "토"].map((wd, i) => (
            <div key={wd} style={{ textAlign: "center", fontWeight: 700, fontSize: 12, padding: "4px 0", color: i === 0 ? "#dc2626" : i === 6 ? "#2563eb" : "#475569" }}>{wd}</div>
          ))}
          {weeks.flat().map((cell) => {
            const dayEvents = (events || []).filter((ev) => eventCoversDate(ev, cell.dateStr));
            const hasHoliday = dayEvents.some((ev) => ev.linkType === "HOLIDAY" && ev.status === "APPROVED");
            const isSelected = cell.dateStr === selectedDate;
            const isToday = cell.dateStr === todayStr;
            return (
              <button
                key={cell.dateStr}
                type="button"
                onClick={() => setSelectedDate(cell.dateStr)}
                style={{
                  minHeight: 88,
                  padding: 6,
                  borderRadius: 10,
                  border: isSelected ? "2px solid var(--primary)" : "1px solid var(--border)",
                  background: hasHoliday ? "#fff1f2" : cell.inMonth ? "#fff" : "#f8fafc",
                  opacity: cell.inMonth ? 1 : 0.55,
                  textAlign: "left",
                  display: "flex",
                  flexDirection: "column",
                  gap: 4,
                  cursor: "pointer",
                  fontFamily: "inherit",
                }}
              >
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                  <span style={{ fontWeight: 700, fontSize: 12.5, fontVariantNumeric: "tabular-nums", color: hasHoliday || cell.weekday === 0 ? "#dc2626" : cell.weekday === 6 ? "#2563eb" : "var(--text)" }}>
                    {cell.day}
                  </span>
                  {isToday && <span className="badge badge-blue">오늘</span>}
                </div>
                {dayEvents.slice(0, 3).map((ev) => {
                  const meta = LINK_META[ev.linkType] || LINK_META.SIMPLE_NOTICE;
                  return (
                    <span key={ev.id} className={`badge ${meta.badgeClass}`} style={{ justifyContent: "flex-start", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: "100%", opacity: ev.status === "REJECTED" ? 0.45 : 1 }}>
                      {ev.title}
                    </span>
                  );
                })}
                {dayEvents.length > 3 && <span className="badge badge-gray">+{dayEvents.length - 3}건</span>}
              </button>
            );
          })}
        </div>
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <h3 style={{ margin: 0, fontSize: 15, fontVariantNumeric: "tabular-nums" }}>{selectedDate} 일정</h3>
            <span className="badge badge-blue">{selectedEvents.length}건</span>
          </div>
          <button type="button" className="btn btn-sm btn-primary no-print" onClick={() => onQuickAddDate(selectedDate)}><PlusCircle size={13} /> 이 날짜 행사 등록</button>
        </div>
        {selectedEvents.length === 0 ? (
          <div style={{ fontSize: 13, color: "#64748b" }}>선택한 일자에 등록된 행사가 없습니다.</div>
        ) : (
          <table className="table">
            <thead><tr><th>시간/기간</th><th>부서</th><th>행사명 · 장소</th><th>대상 · 인솔</th><th>연동</th><th>상태</th><th className="no-print">관리</th></tr></thead>
            <tbody>
              {selectedEvents.map((ev) => {
                const meta = LINK_META[ev.linkType] || LINK_META.SIMPLE_NOTICE;
                return (
                  <tr key={ev.id}>
                    <td><div>{formatEventPeriods(ev)}</div><div style={{ fontSize: 11.5, color: "#64748b" }}>{formatEventDateRange(ev)}</div></td>
                    <td>{ev.department || "-"}</td>
                    <td>
                      <b>{ev.title}</b>
                      {ev.holidayKind && <span className="badge badge-red" style={{ marginLeft: 6 }}>{HOLIDAY_KIND_LABELS[ev.holidayKind] || ev.holidayKind}</span>}
                      {ev.location && <div style={{ fontSize: 12, color: "#475569" }}>장소: {ev.location}</div>}
                    </td>
                    <td><div>{formatEventTargets(ev)}</div><div style={{ fontSize: 12, color: "#475569" }}>{formatTeacherNames(ev, teachers)}</div></td>
                    <td><span className={`badge ${meta.badgeClass}`}>{meta.label}</span></td>
                    <td><StatusBadge status={ev.status} /></td>
                    <td><EventActionButtons ev={ev} user={user} isEventAdmin={isEventAdmin} onCommit={onCommit} onEdit={onEdit} onDelete={onDelete} /></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

// WeeklyBriefingTable은 교무회의·공문용 월~금 주간업무표 뷰를 제공합니다.
export function WeeklyBriefingTable({ events, teachers, user, isEventAdmin, onCommit, onEdit, onDelete }) {
  const [anchorDate, setAnchorDate] = useState(() => formatLocalYMD(new Date()));
  const weekDays = useMemo(() => buildWeekDates(anchorDate), [anchorDate]);
  const shiftWeek = (days) => {
    const d = parseLocalYMD(anchorDate);
    d.setDate(d.getDate() + days);
    setAnchorDate(formatLocalYMD(d));
  };

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12, flexWrap: "wrap", gap: 8 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>주간업무표 (월~금)</h3>
          <span className="badge badge-blue" style={{ fontVariantNumeric: "tabular-nums" }}>{weekDays[0]?.dateStr} ~ {weekDays[4]?.dateStr}</span>
        </div>
        <div className="no-print" style={{ display: "flex", gap: 6 }}>
          <button type="button" className="btn btn-sm" onClick={() => shiftWeek(-7)}><ChevronLeft size={14} /> 이전 주</button>
          <button type="button" className="btn btn-sm" onClick={() => setAnchorDate(formatLocalYMD(new Date()))}>이번 주</button>
          <button type="button" className="btn btn-sm" onClick={() => shiftWeek(7)}>다음 주 <ChevronRight size={14} /></button>
        </div>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 10 }}>
        {weekDays.map((wd) => {
          const dayEvents = (events || []).filter((ev) => eventCoversDate(ev, wd.dateStr));
          const hasHoliday = dayEvents.some((ev) => ev.linkType === "HOLIDAY" && ev.status === "APPROVED");
          return (
            <div key={wd.dateStr} style={{ border: "1px solid var(--border)", borderRadius: 10, padding: 10, background: hasHoliday ? "#fff1f2" : "var(--surface-alt)", display: "flex", flexDirection: "column", gap: 8 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", borderBottom: "1px solid var(--border)", paddingBottom: 6 }}>
                <b style={{ fontSize: 13.5, fontVariantNumeric: "tabular-nums" }}>{wd.shortDate} ({wd.weekdayLabel})</b>
                <span className="badge badge-gray">{dayEvents.length}건</span>
              </div>
              {dayEvents.length === 0 ? (
                <div style={{ fontSize: 12.5, color: "#64748b" }}>일정 없음</div>
              ) : (
                dayEvents.map((ev) => {
                  const meta = LINK_META[ev.linkType] || LINK_META.SIMPLE_NOTICE;
                  return (
                    <div key={ev.id} style={{ background: "#fff", border: "1px solid var(--border)", borderRadius: 8, padding: 8, display: "flex", flexDirection: "column", gap: 4, fontSize: 12 }}>
                      <div style={{ display: "flex", justifyContent: "space-between", gap: 4, flexWrap: "wrap" }}>
                        <span className={`badge ${meta.badgeClass}`}>{meta.shortLabel}</span>
                        <StatusBadge status={ev.status} />
                      </div>
                      <b style={{ fontSize: 13 }}>{ev.title}</b>
                      <div style={{ color: "#475569" }}>{formatEventPeriods(ev)} · {ev.department || "부서미정"}</div>
                      {ev.location && <div style={{ color: "#475569" }}>장소: {ev.location}</div>}
                      <div style={{ color: "#475569" }}>대상: {formatEventTargets(ev)}</div>
                      <div style={{ color: "#475569" }}>인솔: {formatTeacherNames(ev, teachers)}</div>
                      <EventActionButtons ev={ev} user={user} isEventAdmin={isEventAdmin} onCommit={onCommit} onEdit={onEdit} onDelete={onDelete} />
                    </div>
                  );
                })
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// StagingQueueTable은 승인 대기 및 확정된 전체 행사 취합 목록을 관리합니다.
export function StagingQueueTable({ events, teachers, user, isEventAdmin, onCommit, onEdit, onDelete }) {
  const [onlyPending, setOnlyPending] = useState(false);
  const rows = useMemo(() => {
    const base = onlyPending ? (events || []).filter((e) => e.status === "STAGING") : [...(events || [])];
    return base.sort((a, b) => (a.status === "STAGING") !== (b.status === "STAGING") ? (a.status === "STAGING" ? -1 : 1) : (a.eventDate || "").localeCompare(b.eventDate || ""));
  }, [events, onlyPending]);

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>취합 대기함 및 전체 학사일정</h3>
          <span className="badge badge-blue">{rows.length}건</span>
        </div>
        <button type="button" className={`btn btn-sm ${onlyPending ? "btn-primary" : ""}`} onClick={() => setOnlyPending((v) => !v)}>
          {onlyPending ? "전체 보기" : "승인 대기만 보기"}
        </button>
      </div>
      <table className="table">
        <thead><tr><th>일자/교시</th><th>부서</th><th>행사명 · 장소</th><th>대상 · 인솔</th><th>연동</th><th>상태</th><th className="no-print">관리</th></tr></thead>
        <tbody>
          {rows.map((ev) => {
            const meta = LINK_META[ev.linkType] || LINK_META.SIMPLE_NOTICE;
            return (
              <tr key={ev.id}>
                <td><div>{formatEventDateRange(ev)}</div><div style={{ fontSize: 12, color: "#475569" }}>{formatEventPeriods(ev)}</div></td>
                <td>{ev.department}</td>
                <td><b>{ev.title}</b>{ev.location && <div style={{ fontSize: 12, color: "#475569" }}>장소: {ev.location}</div>}</td>
                <td><div>{formatEventTargets(ev)}</div><div style={{ fontSize: 12, color: "#475569" }}>{formatTeacherNames(ev, teachers)}</div></td>
                <td><span className={`badge ${meta.badgeClass}`}>{meta.label}</span></td>
                <td><StatusBadge status={ev.status} /></td>
                <td><EventActionButtons ev={ev} user={user} isEventAdmin={isEventAdmin} onCommit={onCommit} onEdit={onEdit} onDelete={onDelete} /></td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
