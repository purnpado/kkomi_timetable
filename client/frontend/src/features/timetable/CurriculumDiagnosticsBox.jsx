import React from "react";
import { AlertTriangle, CheckCircle2 } from "lucide-react";

// CurriculumDiagnosticsBox는 시수 기반 자동 편성 결과(FET 연쇄 복구 건수 및 미배정 시수 원인 역추적)를 표시합니다.
export default function CurriculumDiagnosticsBox({ report }) {
  if (!report) return null;
  const diags = report.diagnostics || [];

  return (
    <div className="card" style={{ borderColor: report.unplacedCount > 0 ? "#fcc419" : "#96f2d7", background: report.unplacedCount > 0 ? "#fff9db" : "#e6fcf5" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
        <div style={{ fontWeight: 700, fontSize: 13.5, display: "flex", alignItems: "center", gap: 6 }}>
          {report.unplacedCount > 0 ? <AlertTriangle size={15} color="#b86e00" /> : <CheckCircle2 size={15} color="#087f5b" />}
          자동 편성 진단 리포트: 총 {report.createdCount}교시 배정 · FET 재귀 연쇄 복구 {report.ejectionRepairedCount || 0}건 · 미배정 {report.unplacedCount || 0}건
        </div>
      </div>
      {diags.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 10 }}>
          {diags.map((d, idx) => (
            <div key={`${d.unitId}-${idx}`} style={{ background: "#fff", border: "1px solid #ffd43b", borderRadius: 8, padding: "8px 10px", fontSize: 12 }}>
              <div style={{ fontWeight: 700, color: "#b86e00" }}>
                [미배정 원인] {d.className}반 {d.subject} ({d.teacherName}) — {d.primaryCause}
              </div>
              {(d.detailReasons || []).map((r, i) => (
                <div key={i} style={{ color: "#475569", marginTop: 2 }}>· {r}</div>
              ))}
              {(d.suggestions || []).map((s, i) => (
                <div key={i} style={{ color: "#087f5b", marginTop: 2 }}>권고: {s}</div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
