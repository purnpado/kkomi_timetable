import React from "react";
import { CalendarDays, Clock, Lock, ShieldAlert, Unlock, Users } from "lucide-react";

function StatGauge({ label, value, max, color }) {
  const pct = Math.min(100, Math.round((value / Math.max(1, max)) * 100));
  const r = 24;
  const c = 2 * Math.PI * r;
  const offset = c - (pct / 100) * c;
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
      <svg width="60" height="60" viewBox="0 0 60 60">
        <circle cx="30" cy="30" r={r} stroke="#e2e8f0" strokeWidth="6" fill="none" />
        <circle
          cx="30"
          cy="30"
          r={r}
          stroke={color}
          strokeWidth="6"
          fill="none"
          strokeDasharray={c}
          strokeDashoffset={offset}
          strokeLinecap="round"
          transform="rotate(-90 30 30)"
        />
        <text x="30" y="34" textAnchor="middle" fontSize="12" fontWeight="800" fill="#1e293b">
          {value}
        </text>
      </svg>
      <div>
        <div style={{ fontSize: 12.5, fontWeight: 700 }}>{label}</div>
        <div style={{ fontSize: 11, color: "#64748b" }}>연간 기준 ({pct}%)</div>
      </div>
    </div>
  );
}

// DashboardView는 출근 후 즉시 확인하는 오늘의 교무 통합 대시보드입니다.
export default function DashboardView({ user, summary, onNavigate, onOpenApproval }) {
  const myLessons = [...(summary?.myTodayLessons || [])].sort((a, b) => a.period - b.period);
  const homeroom = [...(summary?.homeroomLessons || [])].sort((a, b) => a.period - b.period);
  const todayEvents = summary?.todayEvents || [];
  const examSlots = summary?.myExamSlots || [];
  const pendingReqs = summary?.pendingApprovals || [];
  const openExt = summary?.openExternalExams || [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 12 }}>
        <div>
          <h2 style={{ margin: 0, fontSize: 18 }}>안녕하세요, {user?.name} 선생님 ({user?.position})</h2>
          <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 4 }}>
            오늘 {summary?.todayDate} · DB: <b>{summary?.databaseMode}</b> · 서버 IP: <b>{summary?.serverIp}</b>
          </div>
        </div>
        <div style={{ display: "flex", gap: 16, flexWrap: "wrap" }}>
          <StatGauge label="교내 감독" value={user?.internalExamCount || 0} max={10} color="#3b5bdb" />
          <StatGauge label="외부 감독" value={user?.externalExamCount || 0} max={6} color="#0ca678" />
          <StatGauge label="결보강/대타" value={user?.substitutionCount || 0} max={12} color="#f59f00" />
        </div>
      </div>

      <div className="grid-2">
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
              <Clock size={16} color="#3b5bdb" /> 오늘의 내 수업 타임라인
            </h3>
            <button className="btn btn-sm" onClick={() => onNavigate("timetable")}>전체 시간표</button>
          </div>
          {myLessons.length === 0 ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>오늘 배정된 정규 수업이 없습니다.</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {myLessons.map((l) => (
                <div key={l.id} style={{ display: "flex", justifyContent: "space-between", padding: "9px 12px", borderRadius: 9, background: "#f8fafc", border: "1px solid #e2e8f0" }}>
                  <div>
                    <span className="badge badge-blue" style={{ marginRight: 8 }}>{l.period}교시</span>
                    <b>{l.className}반</b> · {l.subject}
                  </div>
                  <span style={{ fontSize: 12, color: "#64748b" }}>{l.room || "-"}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
              <Users size={16} color="#0ca678" /> 우리 반 오늘 시간표 {user?.homeroomClass ? `(${user.homeroomClass}반)` : "(비담임)"}
            </h3>
          </div>
          {!user?.homeroomClass ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>담임 학급이 지정되지 않은 교사 계정입니다.</div>
          ) : homeroom.length === 0 ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>오늘 등록된 학급 수업이 없습니다.</div>
          ) : (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: 8 }}>
              {homeroom.map((h) => (
                <div key={h.id} style={{ padding: 8, borderRadius: 8, background: h.blockedBy ? "#fff5f5" : "#e6fcf5", fontSize: 12.5 }}>
                  <b>{h.period}교시</b> {h.subject}
                  <div style={{ fontSize: 11, color: "#475569" }}>{h.blockedBy || h.room}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="grid-3">
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
              <ShieldAlert size={16} color="#f59f00" /> 다가오는 시험감독
            </h3>
            <button className="btn btn-sm" onClick={() => onNavigate("internal-exam")}>상세</button>
          </div>
          {examSlots.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>배정된 감독 교시가 없습니다.</div>
          ) : (
            examSlots.slice(0, 4).map((s) => (
              <div key={s.id} style={{ padding: "7px 10px", borderRadius: 8, background: "#fff9db", marginBottom: 6, fontSize: 12.5, display: "flex", justifyContent: "space-between" }}>
                <span>
                  <b>{s.period}교시</b> ({s.slotRole === "CORRIDOR" ? "복도감독" : s.slotRole === "MAIN" ? "정감독" : s.slotRole})
                </span>
                <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                  {s.actualRoomHidden ? <Lock size={12} /> : <Unlock size={12} />} {s.roomName}
                </span>
              </div>
            ))
          )}
        </div>

        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
              <CalendarDays size={16} color="#3b5bdb" /> 오늘 및 주간 행사
            </h3>
            <button className="btn btn-sm" onClick={() => onNavigate("calendar")}>학사일정</button>
          </div>
          {todayEvents.length === 0 && (summary?.weekEvents || []).length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>확정된 행사가 없습니다.</div>
          ) : (
            (todayEvents.length > 0 ? todayEvents : summary.weekEvents).slice(0, 4).map((ev) => (
              <div key={ev.id} style={{ padding: "7px 10px", borderRadius: 8, background: "#f8fafc", marginBottom: 6, fontSize: 12.5 }}>
                <div style={{ fontWeight: 700 }}>{ev.title}</div>
                <div style={{ fontSize: 11.5, color: "#64748b" }}>{ev.eventDate} · {ev.department} · {ev.linkType}</div>
              </div>
            ))
          )}
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>결재·외부감독 모집 현황</h3>
          <div style={{ fontSize: 13, marginBottom: 8 }}>
            수업교환/결보강 대기열: <b>{pendingReqs.length}건</b>{" "}
            {pendingReqs.length > 0 && (
              <button className="btn btn-sm btn-primary" style={{ marginLeft: 6 }} onClick={onOpenApproval}>
                비교 승인 열기
              </button>
            )}
          </div>
          {openExt.map((ex) => (
            <div key={ex.id} style={{ padding: "7px 10px", borderRadius: 8, background: "#eef2ff", marginBottom: 6, fontSize: 12.5 }}>
              <div style={{ fontWeight: 700 }}>{ex.title}</div>
              <div style={{ fontSize: 11.5, color: "#475569" }}>
                시험일: {ex.examDate} · 지원 {ex.volunteerIds?.length || 0} / 필요 {ex.requiredCount}명
              </div>
              <button className="btn btn-sm" style={{ marginTop: 6 }} onClick={() => onNavigate("external-exam")}>
                지원/순번 보기
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
