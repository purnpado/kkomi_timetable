import React from "react";

const SERIES = [
  { key: "timetableChanges", label: "수업변경", color: "#3b5bdb" },
  { key: "substituteSessions", label: "결보강", color: "#0ea5e9" },
  { key: "internalExamSlots", label: "교내감독", color: "#e67700" },
  { key: "externalDispatches", label: "외부파견", color: "#099268" },
  { key: "approvedEvents", label: "학사행사", color: "#7950f2" },
];

// MonthlyTrendBarChart는 월별 교무 실적 추이를 반응형 SVG 스택드 막대 차트로 시각화합니다.
export function MonthlyTrendBarChart({ monthlyTrend = [] }) {
  if (!monthlyTrend || monthlyTrend.length === 0) {
    return <div style={{ fontSize: 13, color: "#64748b" }}>해당 기간에 집계된 월별 데이터가 없습니다.</div>;
  }

  const colWidth = 84;
  const chartW = Math.max(460, monthlyTrend.length * colWidth + 60);
  const chartH = 210;
  const padLeft = 36;
  const padTop = 24;
  const plotH = 140;
  const rowTotals = monthlyTrend.map((tr) => SERIES.reduce((acc, s) => acc + Number(tr[s.key] || 0), 0));
  const maxVal = Math.max(1, ...rowTotals);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap", fontSize: 12 }}>
        {SERIES.map((s) => (
          <span key={s.key} style={{ display: "inline-flex", alignItems: "center", gap: 5, fontWeight: 700, color: "#334155" }}>
            <span style={{ width: 10, height: 10, borderRadius: 3, background: s.color, display: "inline-block" }} />
            {s.label}
          </span>
        ))}
      </div>

      <div style={{ overflowX: "auto" }}>
        <svg viewBox={`0 0 ${chartW} ${chartH}`} style={{ width: "100%", minWidth: 420, height: "auto", display: "block" }}>
          {[0, 0.5, 1].map((ratio) => {
            const y = padTop + plotH * (1 - ratio);
            const val = Math.round(maxVal * ratio);
            return (
              <g key={ratio}>
                <line x1={padLeft} y1={y} x2={chartW - 16} y2={y} stroke="#e2e8f0" strokeWidth="1" />
                <text x={padLeft - 6} y={y + 4} textAnchor="end" fontSize="10" fill="#64748b" style={{ fontVariantNumeric: "tabular-nums" }}>
                  {val}
                </text>
              </g>
            );
          })}

          {monthlyTrend.map((tr, idx) => {
            const xCenter = padLeft + 36 + idx * colWidth;
            const barW = 34;
            const x = xCenter - barW / 2;
            const total = rowTotals[idx];
            let cursorY = padTop + plotH;

            return (
              <g key={tr.month}>
                {SERIES.map((s) => {
                  const val = Number(tr[s.key] || 0);
                  if (val <= 0) return null;
                  const h = Math.max(2, (val / maxVal) * plotH);
                  cursorY -= h;
                  return (
                    <rect key={s.key} x={x} y={cursorY} width={barW} height={h} fill={s.color} rx="2">
                      <title>{`${tr.month} ${s.label}: ${val}`}</title>
                    </rect>
                  );
                })}
                <text x={xCenter} y={Math.max(12, cursorY - 6)} textAnchor="middle" fontSize="11" fontWeight="800" fill="#0f172a" style={{ fontVariantNumeric: "tabular-nums" }}>
                  {total}
                </text>
                <text x={xCenter} y={padTop + plotH + 18} textAnchor="middle" fontSize="11" fontWeight="700" fill="#475569" style={{ fontVariantNumeric: "tabular-nums" }}>
                  {tr.month}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}

// InlineActivityBar는 교원별 총실적을 최대값 대비 인라인 데이터 막대와 숫자로 표시합니다.
export function InlineActivityBar({ value = 0, max = 1 }) {
  const safeMax = Math.max(1, Number(max) || 1);
  const pct = Math.max(0, Math.min(100, Math.round((Number(value || 0) / safeMax) * 100)));
  return (
    <div style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 108 }}>
      <div style={{ width: 62, height: 8, background: "#e2e8f0", borderRadius: 999, overflow: "hidden" }}>
        <div style={{ width: `${pct}%`, height: "100%", background: "var(--primary)", borderRadius: 999 }} />
      </div>
      <span className="badge badge-blue" style={{ fontVariantNumeric: "tabular-nums" }}>
        <b>{value}</b>
      </span>
    </div>
  );
}
