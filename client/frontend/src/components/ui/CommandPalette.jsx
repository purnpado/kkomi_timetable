import React, { useMemo, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Calendar, MapPin, Search, User } from "lucide-react";
import { effectiveTeacherType, getTeacherTypeMeta, isFacultyTeacher } from "../../events/eventStore";

// CommandPalette는 Ctrl+K 단축키로 교사 현재 위치(수업 중 학급/공강), 학급 시간표, 학교 행사를 즉시 검색합니다.
export default function CommandPalette({ open, onOpenChange, teachers, cells, events, onNavigate }) {
  const [query, setQuery] = useState("");

  const nowWeekday = (() => {
    const d = new Date().getDay();
    return d >= 1 && d <= 5 ? d : 1;
  })();

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    const tItems = (teachers || [])
      .filter((t) => {
        if (!isFacultyTeacher(t)) return false;
        if (!q) return true;
        const typeLabel = getTeacherTypeMeta(effectiveTeacherType(t)).label.toLowerCase();
        return (
          t.name.toLowerCase().includes(q) ||
          (t.subject || "").toLowerCase().includes(q) ||
          (t.homeroomClass || "").toLowerCase().includes(q) ||
          typeLabel.includes(q)
        );
      })
      .slice(0, 6)
      .map((t) => {
        const effType = effectiveTeacherType(t);
        const typeTag = effType !== "SUBJECT" ? ` · ${getTeacherTypeMeta(effType).shortLabel}` : "";
        const todayCells = (cells || []).filter((c) => c.dayOfWeek === nowWeekday && (c.teacherId === t.id || c.coTeacherId === t.id));
        const loc = todayCells.length > 0 ? todayCells.map((c) => `${c.period}교시 ${c.className}(${c.room || c.subject})`).join(" · ") : "오늘 수업 없음 / 교무실 대기";
        return { kind: "teacher", id: t.id, title: `${t.name} 선생님 (${t.subject}${typeTag})`, sub: loc, targetTab: "timetable" };
      });

    const evItems = (events || [])
      .filter((e) => !q || e.title.toLowerCase().includes(q) || e.department.toLowerCase().includes(q))
      .slice(0, 4)
      .map((e) => ({
        kind: "event",
        id: e.id,
        title: e.title,
        sub: `${e.eventDate} · ${e.department} (${e.linkType})`,
        targetTab: "calendar",
      }));

    return [...tItems, ...evItems];
  }, [query, teachers, cells, events, nowWeekday]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content command-palette-content" style={{ maxWidth: 560, padding: 16 }}>
          <Dialog.Title style={{ margin: "0 0 4px", fontSize: 13.5, fontWeight: 700, color: "#475569" }}>
            교사 위치 · 학급 · 행사 빠른 검색
          </Dialog.Title>
          <Dialog.Description style={{ margin: "0 0 10px", fontSize: 11.5, color: "#64748b" }}>
            검색 결과를 선택하면 해당 메뉴로 즉시 이동합니다 (ESC 닫기).
          </Dialog.Description>
          <div style={{ display: "flex", alignItems: "center", gap: 8, borderBottom: "1px solid #e2e8f0", paddingBottom: 10, marginBottom: 10 }}>
            <Search size={18} color="#3b5bdb" />
            <input
              autoFocus
              className="input"
              style={{ border: 0, outline: "none", fontSize: 15, padding: 4 }}
              placeholder="교사 현재 위치, 과목, 학급, 행사명을 입력하세요..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: 360, overflowY: "auto" }}>
            {results.length === 0 && <div style={{ padding: 20, textAlign: "center", color: "#64748b" }}>검색 결과가 없습니다.</div>}
            {results.map((r) => (
              <button
                key={`${r.kind}-${r.id}`}
                type="button"
                className="btn"
                style={{ justifyContent: "space-between", padding: "10px 12px", textAlign: "left" }}
                onClick={() => {
                  onNavigate(r.targetTab);
                  onOpenChange(false);
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  {r.kind === "teacher" ? <User size={16} color="#3b5bdb" /> : <Calendar size={16} color="#0ca678" />}
                  <div>
                    <div style={{ fontWeight: 700, fontSize: 13.5 }}>{r.title}</div>
                    <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>{r.sub}</div>
                  </div>
                </div>
                <span className="badge badge-gray">
                  <MapPin size={11} /> 이동
                </span>
              </button>
            ))}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
