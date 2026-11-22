import React, { useEffect, useMemo, useState } from "react";
import { AlertTriangle, Clock, MapPin, PlusCircle, Repeat, Save, Users, X } from "lucide-react";
import { apiFetch, isFacultyTeacher } from "../../events/eventStore";
import {
  DEFAULT_BELL_PRESETS,
  HOLIDAY_KIND_LABELS,
  LINK_META,
  QUICK_LOCATIONS,
  formatLocalYMD,
} from "./calendarUtils";

const BLANK_FORM = (user, initialDate, teachers) => {
  const faculty = (teachers || []).filter(isFacultyTeacher);
  const preferred = faculty.find((t) => t.id === user?.id) || faculty[0];
  const today = initialDate || formatLocalYMD(new Date());
  return {
    id: "",
    title: "",
    department: "교무기획부",
    eventDate: today,
    endDate: today,
    startPeriod: 5,
    endPeriod: 6,
    linkType: "CLASS_BLOCK",
    holidayKind: "PUBLIC_HOLIDAY",
    location: "",
    bellPreset: "",
    bellOverride: [],
    targetGrades: [1],
    targetClassesText: "",
    leadTeachers: preferred ? [preferred.id] : [],
    recurrence: "NONE",
    recurrenceEnd: "",
    description: "",
  };
};

// CalendarEventForm은 다일·공휴일·단축수업 프리셋·복수학년/반·복수인솔·반복일정 등록 및 실시간 충돌 검사를 제공합니다.
export default function CalendarEventForm({
  serverUrl,
  user,
  teachers,
  bellPresets,
  editingEvent,
  initialDate,
  onSaved,
  onCancel,
  onNotify,
}) {
  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const presets = bellPresets?.length > 0 ? bellPresets : DEFAULT_BELL_PRESETS;
  const [form, setForm] = useState(() => BLANK_FORM(user, initialDate, teachers));
  const [conflicts, setConflicts] = useState([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (editingEvent) {
      setForm({
        id: editingEvent.id || "",
        title: editingEvent.title || "",
        department: editingEvent.department || "교무기획부",
        eventDate: editingEvent.eventDate || formatLocalYMD(new Date()),
        endDate: editingEvent.endDate || editingEvent.eventDate || formatLocalYMD(new Date()),
        startPeriod: Number(editingEvent.startPeriod ?? 0),
        endPeriod: Number(editingEvent.endPeriod ?? 0),
        linkType: editingEvent.linkType || "SIMPLE_NOTICE",
        holidayKind: editingEvent.holidayKind || "PUBLIC_HOLIDAY",
        location: editingEvent.location || "",
        bellPreset: editingEvent.bellPreset || "",
        bellOverride: Array.isArray(editingEvent.bellOverride) ? editingEvent.bellOverride : [],
        targetGrades: Array.isArray(editingEvent.targetGrades) ? editingEvent.targetGrades : [],
        targetClassesText: (editingEvent.targetClasses || []).join(", "),
        leadTeachers: Array.isArray(editingEvent.leadTeachers) ? editingEvent.leadTeachers : [],
        recurrence: editingEvent.recurrence || "NONE",
        recurrenceEnd: editingEvent.recurrenceEnd || "",
        description: editingEvent.description || "",
      });
    } else {
      setForm(BLANK_FORM(user, initialDate, teachers));
    }
  }, [editingEvent, initialDate, teachers, user]);

  const parsedClasses = useMemo(
    () => form.targetClassesText.split(",").map((s) => s.trim()).filter(Boolean),
    [form.targetClassesText]
  );

  useEffect(() => {
    if (!form.eventDate) return undefined;
    const timer = setTimeout(async () => {
      try {
        const res = await apiFetch(serverUrl, "/api/calendar/conflicts", user.id, {
          method: "POST",
          body: JSON.stringify({
            id: form.id,
            title: form.title || "신규 행사",
            eventDate: form.eventDate,
            endDate: form.endDate || form.eventDate,
            startPeriod: form.linkType === "HOLIDAY" ? 0 : Number(form.startPeriod),
            endPeriod: form.linkType === "HOLIDAY" ? 0 : Number(form.endPeriod),
            linkType: form.linkType,
            location: form.location,
            targetGrades: form.targetGrades,
            targetClasses: parsedClasses,
          }),
        });
        setConflicts(Array.isArray(res) ? res : []);
      } catch {
        setConflicts([]);
      }
    }, 250);
    return () => clearTimeout(timer);
  }, [
    serverUrl, user.id, form.id, form.title, form.eventDate, form.endDate,
    form.startPeriod, form.endPeriod, form.linkType, form.location, form.targetGrades, parsedClasses,
  ]);

  const toggleGrade = (g) => {
    setForm((prev) => {
      const has = prev.targetGrades.includes(g);
      return { ...prev, targetGrades: has ? prev.targetGrades.filter((x) => x !== g) : [...prev.targetGrades, g].sort() };
    });
  };

  const toggleLeadTeacher = (id) => {
    setForm((prev) => {
      const has = prev.leadTeachers.includes(id);
      return { ...prev, leadTeachers: has ? prev.leadTeachers.filter((x) => x !== id) : [...prev.leadTeachers, id] };
    });
  };

  const applyPreset = (presetDef) => {
    setForm((prev) => ({
      ...prev,
      bellPreset: presetDef.key,
      bellOverride: presetDef.periods.map((p) => ({ ...p })),
    }));
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!form.title.trim()) return;
    setSaving(true);
    try {
      const isEdit = Boolean(form.id);
      const payload = {
        id: form.id || undefined,
        title: form.title.trim(),
        department: form.department.trim() || "교무기획부",
        eventDate: form.eventDate,
        endDate: form.endDate && form.endDate >= form.eventDate ? form.endDate : form.eventDate,
        startPeriod: form.linkType === "HOLIDAY" ? 0 : Number(form.startPeriod),
        endPeriod: form.linkType === "HOLIDAY" ? 0 : Number(form.endPeriod),
        linkType: form.linkType,
        holidayKind: form.linkType === "HOLIDAY" ? form.holidayKind : "",
        location: form.location.trim(),
        bellPreset: form.linkType === "BELL_CHANGE" ? form.bellPreset : "",
        bellOverride: form.linkType === "BELL_CHANGE" ? form.bellOverride : [],
        targetGrades: form.linkType === "HOLIDAY" && form.targetGrades.length === 0 && parsedClasses.length === 0 ? [1, 2, 3] : form.targetGrades,
        targetClasses: parsedClasses,
        leadTeachers: form.leadTeachers,
        recurrence: isEdit ? "NONE" : form.recurrence,
        recurrenceEnd: isEdit || form.recurrence === "NONE" ? "" : form.recurrenceEnd,
        description: form.description.trim() || `${form.department.trim() || "교무기획부"} 등록 행사`,
      };
      await apiFetch(serverUrl, "/api/calendar/events", user.id, {
        method: isEdit ? "PUT" : "POST",
        body: JSON.stringify(payload),
      });
      onNotify(isEdit ? "행사 수정 완료" : "행사 등록 완료", isEdit ? "변경 사항이 반영되었습니다." : "승인 대기함에 등록되었습니다.");
      onSaved?.();
    } catch (err) {
      onNotify(form.id ? "행사 수정 실패" : "행사 등록 실패", err.message, true);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="card no-print">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
        <h3 style={{ margin: 0, fontSize: 15 }}>{form.id ? "학사 행사 수정" : "새 학사 행사 · 휴업일 등록"}</h3>
        <button type="button" className="btn btn-sm" onClick={onCancel}><X size={14} /> 닫기</button>
      </div>
      <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(175px, 1fr))", gap: 10 }}>
          <input className="input" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="행사명 (예: 1학년 현장체험학습)" required />
          <input className="input" value={form.department} onChange={(e) => setForm({ ...form, department: e.target.value })} placeholder="주관 부서" />
          <input className="input" type="date" value={form.eventDate} onChange={(e) => setForm((p) => ({ ...p, eventDate: e.target.value, endDate: p.endDate < e.target.value ? e.target.value : p.endDate }))} title="시작 일자" />
          <input className="input" type="date" value={form.endDate} min={form.eventDate} onChange={(e) => setForm({ ...form, endDate: e.target.value })} title="종료 일자(다일 연속 행사)" />
          <select className="select" value={form.linkType} onChange={(e) => setForm({ ...form, linkType: e.target.value })}>
            {Object.entries(LINK_META).map(([k, m]) => <option key={k} value={k}>[{m.label}]</option>)}
          </select>
          {form.linkType === "HOLIDAY" ? (
            <select className="select" value={form.holidayKind} onChange={(e) => setForm({ ...form, holidayKind: e.target.value })}>
              {Object.entries(HOLIDAY_KIND_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          ) : (
            <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
              <input className="input" type="number" min={0} max={7} value={form.startPeriod} onChange={(e) => setForm({ ...form, startPeriod: e.target.value })} title="시작 교시(0=종일)" />
              <input className="input" type="number" min={0} max={7} value={form.endPeriod} onChange={(e) => setForm({ ...form, endPeriod: e.target.value })} title="종료 교시" />
              <button type="button" className="btn btn-sm" onClick={() => setForm((p) => ({ ...p, startPeriod: 0, endPeriod: 0 }))}>종일</button>
            </div>
          )}
        </div>

        <div style={{ display: "flex", flexWrap: "wrap", gap: 14, alignItems: "center", fontSize: 12.5 }}>
          <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
            <span className="badge badge-gray">대상</span>
            <button type="button" className={`btn btn-sm ${form.targetGrades.length === 3 ? "btn-primary" : ""}`} onClick={() => setForm((p) => ({ ...p, targetGrades: p.targetGrades.length === 3 ? [] : [1, 2, 3] }))}>전교(1·2·3학년)</button>
            {[1, 2, 3].map((g) => (
              <button key={g} type="button" className={`btn btn-sm ${form.targetGrades.includes(g) ? "btn-primary" : ""}`} onClick={() => toggleGrade(g)}>{g}학년</button>
            ))}
            <input className="input" style={{ width: 150 }} value={form.targetClassesText} onChange={(e) => setForm({ ...form, targetClassesText: e.target.value })} placeholder="특정 반 (예: 1-1, 1-2)" />
          </div>
          <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
            <span className="badge badge-gray"><MapPin size={12} /> 장소</span>
            <input className="input" style={{ width: 140 }} value={form.location} onChange={(e) => setForm({ ...form, location: e.target.value })} placeholder="행사 장소 입력" />
            {QUICK_LOCATIONS.map((loc) => (
              <button key={loc} type="button" className={`btn btn-sm ${form.location === loc ? "btn-primary" : ""}`} onClick={() => setForm({ ...form, location: form.location === loc ? "" : loc })}>{loc}</button>
            ))}
          </div>
        </div>

        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
          <span className="badge badge-blue"><Users size={12} /> 인솔교사 {form.leadTeachers.length}명</span>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 5, maxHeight: 74, overflowY: "auto" }}>
            {facultyTeachers.map((t) => {
              const active = form.leadTeachers.includes(t.id);
              return (
                <button key={t.id} type="button" className={`btn btn-sm ${active ? "btn-primary" : ""}`} onClick={() => toggleLeadTeacher(t.id)}>
                  {t.name} ({t.subject})
                </button>
              );
            })}
          </div>
        </div>

        {form.linkType === "HOLIDAY" && (
          <div style={{ padding: "8px 12px", borderRadius: 8, background: "var(--red-soft)", color: "#991b1b", fontSize: 12.5 }}>
            공휴일·재량휴업일은 확정 시 해당 주간 시간표를 휴업 처리하고 학기 필수 시수 원장에서 자동 차감하며 교사 결강 부채를 만들지 않습니다.
          </div>
        )}

        {form.linkType === "BELL_CHANGE" && (
          <div style={{ border: "1px solid var(--border)", borderRadius: 10, padding: 10, background: "var(--surface-alt)" }}>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginBottom: 8 }}>
              <span className="badge badge-amber"><Clock size={12} /> 단축수업 프리셋</span>
              {presets.map((p) => (
                <button key={p.key} type="button" className={`btn btn-sm ${form.bellPreset === p.key ? "btn-primary" : ""}`} onClick={() => applyPreset(p)}>
                  {p.name}
                </button>
              ))}
            </div>
            {form.bellOverride.length > 0 && (
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(175px, 1fr))", gap: 6 }}>
                {form.bellOverride.map((bp, idx) => (
                  <div key={bp.period} style={{ display: "flex", gap: 4, alignItems: "center", fontSize: 12 }}>
                    <span className="badge badge-gray">{bp.label || `${bp.period}교시`}</span>
                    <input className="input" style={{ padding: "4px 6px", fontSize: 12 }} value={bp.startTime} onChange={(e) => setForm((prev) => ({ ...prev, bellPreset: "CUSTOM", bellOverride: prev.bellOverride.map((item, i) => (i === idx ? { ...item, startTime: e.target.value } : item)) }))} />
                    <span>~</span>
                    <input className="input" style={{ padding: "4px 6px", fontSize: 12 }} value={bp.endTime} onChange={(e) => setForm((prev) => ({ ...prev, bellPreset: "CUSTOM", bellOverride: prev.bellOverride.map((item, i) => (i === idx ? { ...item, endTime: e.target.value } : item)) }))} />
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {!form.id && (
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
            <span className="badge badge-gray"><Repeat size={12} /> 반복 일정</span>
            <select className="select" style={{ width: 150 }} value={form.recurrence} onChange={(e) => setForm({ ...form, recurrence: e.target.value })}>
              <option value="NONE">반복 안 함</option>
              <option value="WEEKLY">매주 반복</option>
              <option value="MONTHLY">매월 반복</option>
            </select>
            {form.recurrence !== "NONE" && (
              <input className="input" style={{ width: 165 }} type="date" min={form.eventDate} value={form.recurrenceEnd} onChange={(e) => setForm({ ...form, recurrenceEnd: e.target.value })} required title="반복 종료일" />
            )}
          </div>
        )}

        {conflicts.length > 0 && (
          <div style={{ padding: "10px 12px", borderRadius: 10, border: "1px solid #fca5a5", background: "#fef2f2", color: "#991b1b", display: "flex", flexDirection: "column", gap: 6 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 700, fontSize: 13 }}>
              <AlertTriangle size={15} /> 충돌 경고 <span className="badge badge-red">{conflicts.length}건</span>
            </div>
            {conflicts.map((c, idx) => (
              <div key={`${c.eventId}-${c.type}-${idx}`} style={{ display: "flex", gap: 8, alignItems: "center", fontSize: 12.5 }}>
                <span className="badge badge-red">{c.type === "LOCATION" ? "장소 중복" : "학년·교시 충돌"}</span>
                <span>{c.message}</span>
              </div>
            ))}
          </div>
        )}

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <input className="input" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="비고 / 공문 안내 내용(선택)" />
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {form.id ? <Save size={15} /> : <PlusCircle size={15} />}
            {form.id ? "수정 저장" : "행사 등록 요청"}
          </button>
        </div>
      </form>
    </div>
  );
}
