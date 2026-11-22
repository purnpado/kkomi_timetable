import React, { useMemo, useState } from "react";
import { Grid3X3 } from "lucide-react";
import { apiFetch } from "../../events/eventStore";
import { HelpTip } from "../../components/ui/SmartVisualKit";

const WEEK_DAYS = [
  { key: 1, label: "월" },
  { key: 2, label: "화" },
  { key: 3, label: "수" },
  { key: 4, label: "목" },
  { key: 5, label: "금" },
];
const PERIODS = [1, 2, 3, 4, 5, 6, 7];

const BRUSHES = [
  { key: "HARD_AVOID", label: "절대 금지", bg: "#fee2e2", color: "#991b1b", border: "#fca5a5" },
  { key: "MEETING_FREE", label: "회의 공강", bg: "#fef3c7", color: "#92400e", border: "#fcd34d" },
  { key: "SOFT_AVOID", label: "기피 교시", bg: "#e0f2fe", color: "#075985", border: "#7dd3fc" },
  { key: "CLEAR", label: "해제", bg: "#f1f5f9", color: "#475569", border: "#cbd5e1" },
];

// ConstraintHeatmap은 월~금 × 1~7교시 클릭형 공강·기피 히트맵 및 전체 교사 제약 농도를 제공합니다.
export default function ConstraintHeatmap({ serverUrl, user, regularTeachers, constraints, filterTeacherId, onFilterChange, onReload, onNotify }) {
  const [brush, setBrush] = useState("HARD_AVOID");
  const [busyKey, setBusyKey] = useState("");

  const activeTeacher = useMemo(
    () => (filterTeacherId && filterTeacherId !== "ALL" ? regularTeachers.find((t) => t.id === filterTeacherId) : null),
    [filterTeacherId, regularTeachers]
  );

  const handleCellClick = async (day, period) => {
    if (!activeTeacher || busyKey) return;
    const key = `${day}-${period}`;
    setBusyKey(key);
    try {
      const exact = (constraints || []).find(
        (c) => c.teacherId === activeTeacher.id && c.dayOfWeek === day && c.period === period
      );
      if (exact && (brush === "CLEAR" || exact.constraintType === brush)) {
        await apiFetch(serverUrl, `/api/timetable/constraints?id=${encodeURIComponent(exact.id)}`, user.id, { method: "DELETE" });
        onNotify("제약 해제 완료", `${activeTeacher.name} (${WEEK_DAYS[day - 1].label} ${period}교시)`);
      } else if (brush !== "CLEAR") {
        if (exact) {
          await apiFetch(serverUrl, `/api/timetable/constraints?id=${encodeURIComponent(exact.id)}`, user.id, { method: "DELETE" });
        }
        const brushLabel = BRUSHES.find((b) => b.key === brush)?.label || "공강 제약";
        await apiFetch(serverUrl, "/api/timetable/constraints", user.id, {
          method: "POST",
          body: JSON.stringify({
            teacherId: activeTeacher.id,
            dayOfWeek: day,
            period,
            constraintType: brush,
            reason: `히트맵 지정 (${brushLabel})`,
          }),
        });
        onNotify("공강 제약 저장", `${activeTeacher.name} · ${WEEK_DAYS[day - 1].label} ${period}교시 (${brushLabel})`);
      }
      await onReload();
    } catch (err) {
      onNotify("히트맵 반영 실패", err.message, true);
    } finally {
      setBusyKey("");
    }
  };

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <Grid3X3 size={16} color="#3b5bdb" />
          <h3 style={{ margin: 0, fontSize: 15 }}>클릭형 5×7 공강 · 기피 교시 히트맵</h3>
          <HelpTip
            label="히트맵 사용법"
            text="교사를 선택한 뒤 브러시(절대 금지·회의 공강·기피 교시·해제)를 골라 교시 칸을 클릭하면 즉시 저장·해제됩니다. 전체 교사 보기에서는 교시별 제약 인원 농도를 확인합니다."
          />
        </div>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
          <select className="select" style={{ width: 185 }} value={filterTeacherId} onChange={(e) => onFilterChange(e.target.value)}>
            <option value="ALL">전체 교사 농도 히트맵</option>
            {regularTeachers.map((t) => (
              <option key={t.id} value={t.id}>{t.name} ({t.subject || "교과"})</option>
            ))}
          </select>
          {activeTeacher && BRUSHES.map((b) => (
            <button
              key={b.key}
              type="button"
              className={`btn btn-sm ${brush === b.key ? "btn-primary" : ""}`}
              onClick={() => setBrush(b.key)}
            >
              {b.label}
            </button>
          ))}
        </div>
      </div>

      <table className="table" style={{ tableLayout: "fixed" }}>
        <thead>
          <tr>
            <th style={{ width: 76, textAlign: "center" }}>교시</th>
            {WEEK_DAYS.map((d) => <th key={d.key} style={{ textAlign: "center" }}>{d.label}요일</th>)}
          </tr>
        </thead>
        <tbody>
          {PERIODS.map((p) => (
            <tr key={p}>
              <td style={{ textAlign: "center", fontWeight: 700, fontVariantNumeric: "tabular-nums" }}>{p}교시</td>
              {WEEK_DAYS.map((d) => {
                if (activeTeacher) {
                  const exact = (constraints || []).find((c) => c.teacherId === activeTeacher.id && c.dayOfWeek === d.key && c.period === p);
                  const inherited = !exact && (constraints || []).find(
                    (c) => c.teacherId === activeTeacher.id && ((c.dayOfWeek === 0 && c.period === p) || (c.dayOfWeek === d.key && c.period === 0))
                  );
                  const match = exact || inherited;
                  const meta = match ? BRUSHES.find((b) => b.key === match.constraintType) : null;
                  return (
                    <td key={d.key} style={{ padding: 4 }}>
                      <button
                        type="button"
                        disabled={Boolean(busyKey)}
                        onClick={() => handleCellClick(d.key, p)}
                        style={{
                          width: "100%",
                          minHeight: 36,
                          borderRadius: 8,
                          border: `1px solid ${meta ? meta.border : "#e2e8f0"}`,
                          background: meta ? meta.bg : "#ffffff",
                          color: meta ? meta.color : "#94a3b8",
                          fontSize: 11.5,
                          fontWeight: 700,
                          cursor: "pointer",
                          fontFamily: "inherit",
                        }}
                      >
                        {meta ? (inherited ? `${meta.label}(종일)` : meta.label) : "+"}
                      </button>
                    </td>
                  );
                }
                const hits = (constraints || []).filter(
                  (c) => (c.dayOfWeek === 0 || c.dayOfWeek === d.key) && (c.period === 0 || c.period === p)
                );
                const count = new Set(hits.map((h) => h.teacherId)).size;
                const bg = count === 0 ? "#ffffff" : count === 1 ? "#eff6ff" : count <= 3 ? "#fef3c7" : "#fee2e2";
                const color = count === 0 ? "#94a3b8" : count === 1 ? "#1d4ed8" : count <= 3 ? "#92400e" : "#991b1b";
                return (
                  <td key={d.key} style={{ padding: 4 }}>
                    <div
                      style={{
                        minHeight: 34,
                        borderRadius: 8,
                        border: "1px solid #e2e8f0",
                        background: bg,
                        color,
                        display: "grid",
                        placeItems: "center",
                        fontSize: 12,
                        fontWeight: 700,
                        fontVariantNumeric: "tabular-nums",
                      }}
                    >
                      {count > 0 ? `${count}명` : "-"}
                    </div>
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
