import React from "react";
import { Clock, Maximize2, Shield } from "lucide-react";
import { formatUserDisplayName } from "../../events/eventStore";

// SideDockWidget은 화면 우측 가장자리에 폭 260px대로 고정되는 세로 타임라인 위젯 뷰입니다.
export default function SideDockWidget({ user, summary, onRestoreFull }) {
  const lessons = [...(summary?.myTodayLessons || [])].sort((a, b) => a.period - b.period);
  const exams = summary?.myExamSlots || [];

  return (
    <div style={{ padding: 12, display: "flex", flexDirection: "column", gap: 10, height: "100vh", overflowY: "auto", background: "#f8fafc" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", borderBottom: "1px solid #cbd5e1", paddingBottom: 8 }}>
        <div>
          <div style={{ fontWeight: 800, fontSize: 14, color: "#1e293b" }}>🐾 꼬미시간 위젯</div>
          <div style={{ fontSize: 11.5, color: "#64748b" }}>{formatUserDisplayName(user)} · {summary?.todayDate}</div>
        </div>
        <button className="btn btn-sm btn-primary" onClick={onRestoreFull} title="전체 대시보드 창으로 복원">
          <Maximize2 size={13} /> 복원
        </button>
      </div>

      <div className="card" style={{ padding: 10 }}>
        <div style={{ fontSize: 12, fontWeight: 700, marginBottom: 6, display: "flex", alignItems: "center", gap: 4 }}>
          <Clock size={13} color="#3b5bdb" /> 오늘 내 수업 타임라인
        </div>
        {lessons.length === 0 ? (
          <div style={{ fontSize: 12, color: "#64748b" }}>오늘 정규 수업이 없습니다.</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {lessons.map((l) => (
              <div key={l.id} style={{ padding: "6px 8px", borderRadius: 7, background: "#eef2ff", borderLeft: "3px solid #3b5bdb", fontSize: 12 }}>
                <b>{l.period}교시</b> · {l.className}반 {l.subject}
                <div style={{ fontSize: 11, color: "#475569" }}>{l.room || "-"}</div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="card" style={{ padding: 10 }}>
        <div style={{ fontSize: 12, fontWeight: 700, marginBottom: 6, display: "flex", alignItems: "center", gap: 4 }}>
          <Shield size={13} color="#f59f00" /> 시험감독 일정
        </div>
        {exams.length === 0 ? (
          <div style={{ fontSize: 12, color: "#64748b" }}>배정된 감독 일정이 없습니다.</div>
        ) : (
          exams.map((ex) => (
            <div key={ex.id} style={{ padding: "6px 8px", borderRadius: 7, background: "#fff9db", fontSize: 12, marginBottom: 4 }}>
              <b>{ex.period}교시</b> ({ex.slotRole}) · {ex.roomName}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
