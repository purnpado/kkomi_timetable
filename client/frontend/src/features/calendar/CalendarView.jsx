import React, { useEffect, useMemo, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { CalendarDays, CalendarRange, FileSpreadsheet, FileText, Inbox, PlusCircle, Printer } from "lucide-react";
import { apiFetch, hasRole, printDesktopWindow } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import CalendarEventForm from "./CalendarEventForm";
import { MonthlyCalendarGrid, StagingQueueTable, WeeklyBriefingTable } from "./CalendarViewsGrid";
import { DEFAULT_BELL_PRESETS, formatLocalYMD } from "./calendarUtils";

// CalendarView는 월간 달력·주간업무표·취합 대기함 3종 뷰 전환과 다일·휴업일·시종 프리셋·반복 일정 관리를 제공합니다.
export default function CalendarView({ serverUrl, user, events, teachers, onRefresh, onNotify }) {
  const [viewMode, setViewMode] = useState("month");
  const [departmentFilter, setDepartmentFilter] = useState("ALL");
  const [showForm, setShowForm] = useState(false);
  const [editingEvent, setEditingEvent] = useState(null);
  const [quickDate, setQuickDate] = useState(() => formatLocalYMD(new Date()));
  const [bellPresets, setBellPresets] = useState(DEFAULT_BELL_PRESETS);
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleting, setDeleting] = useState(false);

  const isEventAdmin = hasRole(user, "EVENT_ADMIN");

  useEffect(() => {
    let active = true;
    apiFetch(serverUrl, "/api/calendar/bell-presets", user.id)
      .then((list) => {
        if (active && Array.isArray(list) && list.length > 0) setBellPresets(list);
      })
      .catch(() => {});
    return () => { active = false; };
  }, [serverUrl, user.id]);

  const departments = useMemo(() => {
    const set = new Set((events || []).map((ev) => String(ev.department || "").trim()).filter(Boolean));
    return ["ALL", ...Array.from(set).sort()];
  }, [events]);

  const filteredEvents = useMemo(() => {
    if (departmentFilter === "ALL") return events || [];
    return (events || []).filter((ev) => String(ev.department || "").trim() === departmentFilter);
  }, [events, departmentFilter]);

  const counts = useMemo(() => {
    const list = filteredEvents;
    return {
      total: list.length,
      staging: list.filter((e) => e.status === "STAGING").length,
      approved: list.filter((e) => e.status === "APPROVED").length,
      holiday: list.filter((e) => e.linkType === "HOLIDAY").length,
    };
  }, [filteredEvents]);

  const handleCommit = async (eventId, approve) => {
    try {
      await apiFetch(serverUrl, "/api/calendar/commit", user.id, {
        method: "POST",
        body: JSON.stringify({ eventId, approve }),
      });
      onNotify(approve ? "행사 최종 확정 반영" : "행사 반려", approve ? "시간표·시종·시수 원장 연동이 완료되었습니다." : "행사 신청이 반려되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("처리 실패", err.message, true);
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await apiFetch(serverUrl, `/api/calendar/events?id=${encodeURIComponent(deleteTarget.id)}`, user.id, {
        method: "DELETE",
      });
      onNotify("행사 삭제 완료", `'${deleteTarget.title}' 일정이 삭제되었습니다.`);
      setDeleteTarget(null);
      onRefresh();
    } catch (err) {
      onNotify("행사 삭제 실패", err.message, true);
    } finally {
      setDeleting(false);
    }
  };

  const openCreateForm = (dateStr) => {
    setEditingEvent(null);
    setQuickDate(dateStr || formatLocalYMD(new Date()));
    setShowForm(true);
  };

  const openEditForm = (ev) => {
    setEditingEvent(ev);
    setShowForm(true);
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <h2 style={{ margin: 0, fontSize: 17 }}>주중·월중 행사 취합 및 학사일정 연동</h2>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap", fontVariantNumeric: "tabular-nums" }}>
              <span className="badge badge-gray">전체 {counts.total}건</span>
              <span className="badge badge-amber">승인 대기 {counts.staging}건</span>
              <span className="badge badge-green">확정 반영 {counts.approved}건</span>
              <span className="badge badge-red">공휴·휴업 {counts.holiday}건</span>
            </div>
          </div>
          <div className="no-print" style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <button type="button" className="btn btn-sm" onClick={() => downloadAuthenticatedFile(serverUrl, "/api/export/hwpx?kind=calendar", "주중월중_학사일정표.hwpx").catch((e) => onNotify("한글 출력 실패", e.message, true))}>
              <FileText size={14} /> 한글 행사표 (.hwpx)
            </button>
            <button type="button" className="btn btn-sm" onClick={() => downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=calendar", "주중월중_학사일정.xlsx").catch((e) => onNotify("엑셀 출력 실패", e.message, true))}>
              <FileSpreadsheet size={14} /> 엑셀 일정 (.xlsx)
            </button>
            <button type="button" className="btn btn-sm" onClick={() => downloadAuthenticatedFile(serverUrl, "/api/export/print", "교무실_대형현황판.html", { openInTab: true }).catch((e) => onNotify("현황판 열기 실패", e.message, true))}>
              <Printer size={14} /> B4/A3 고대비 현황판
            </button>
            <button type="button" className="btn btn-sm" onClick={printDesktopWindow}>
              <Printer size={14} /> 현재 화면 인쇄/PDF
            </button>
          </div>
        </div>
      </div>

      <Tabs.Root value={viewMode} onValueChange={setViewMode} style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <div className="card no-print" style={{ padding: "10px 14px", display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <Tabs.List style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            <Tabs.Trigger value="month" className={`btn btn-sm ${viewMode === "month" ? "btn-primary" : ""}`}>
              <CalendarDays size={14} /> 월간 달력
            </Tabs.Trigger>
            <Tabs.Trigger value="week" className={`btn btn-sm ${viewMode === "week" ? "btn-primary" : ""}`}>
              <CalendarRange size={14} /> 주간업무표
            </Tabs.Trigger>
            <Tabs.Trigger value="staging" className={`btn btn-sm ${viewMode === "staging" ? "btn-primary" : ""}`}>
              <Inbox size={14} /> 취합 대기함 {counts.staging > 0 ? `(${counts.staging})` : ""}
            </Tabs.Trigger>
          </Tabs.List>
          <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
            <select className="select" style={{ width: 170 }} value={departmentFilter} onChange={(e) => setDepartmentFilter(e.target.value)}>
              {departments.map((d) => <option key={d} value={d}>{d === "ALL" ? "전체 부서" : d}</option>)}
            </select>
            <button type="button" className="btn btn-sm btn-primary" onClick={() => (showForm ? setShowForm(false) : openCreateForm())}>
              <PlusCircle size={14} /> {showForm ? "입력창 닫기" : "새 행사 등록"}
            </button>
          </div>
        </div>

        {showForm && (
          <CalendarEventForm
            serverUrl={serverUrl}
            user={user}
            teachers={teachers}
            bellPresets={bellPresets}
            editingEvent={editingEvent}
            initialDate={quickDate}
            onSaved={() => { setShowForm(false); setEditingEvent(null); onRefresh(); }}
            onCancel={() => { setShowForm(false); setEditingEvent(null); }}
            onNotify={onNotify}
          />
        )}

        <Tabs.Content value="month">
          <MonthlyCalendarGrid
            events={filteredEvents}
            teachers={teachers}
            user={user}
            isEventAdmin={isEventAdmin}
            onCommit={handleCommit}
            onEdit={openEditForm}
            onDelete={setDeleteTarget}
            onQuickAddDate={openCreateForm}
          />
        </Tabs.Content>
        <Tabs.Content value="week">
          <WeeklyBriefingTable
            events={filteredEvents}
            teachers={teachers}
            user={user}
            isEventAdmin={isEventAdmin}
            onCommit={handleCommit}
            onEdit={openEditForm}
            onDelete={setDeleteTarget}
          />
        </Tabs.Content>
        <Tabs.Content value="staging">
          <StagingQueueTable
            events={filteredEvents}
            teachers={teachers}
            user={user}
            isEventAdmin={isEventAdmin}
            onCommit={handleCommit}
            onEdit={openEditForm}
            onDelete={setDeleteTarget}
          />
        </Tabs.Content>
      </Tabs.Root>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}
        title="학사 행사 삭제"
        description={deleteTarget ? `'${deleteTarget.title}' (${deleteTarget.eventDate}) 일정을 삭제하고 연동된 결보강·시종 상태를 정리하시겠습니까?` : ""}
        confirmLabel="삭제"
        danger
        busy={deleting}
        onConfirm={handleConfirmDelete}
      />
    </div>
  );
}
