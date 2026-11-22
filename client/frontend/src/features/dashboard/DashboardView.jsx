import React, { useEffect, useMemo, useState } from "react";
import { BookOpen, CalendarDays, CheckSquare, Clock, Lock, Rocket, ShieldAlert, Unlock, Users } from "lucide-react";
import { formatUserDisplayName, hasRole } from "../../events/eventStore";
import { ProgressBar, StatusDot } from "../../components/ui/SmartVisualKit";

function currentHHMM() {
  const now = new Date();
  return `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
}

function toMinutes(hhmm) {
  const [h, m] = String(hhmm || "").split(":").map(Number);
  if (Number.isNaN(h) || Number.isNaN(m)) return -1;
  return h * 60 + m;
}

function StatGauge({ label, value, max, color }) {
  const pct = Math.min(100, Math.round((value / Math.max(1, max)) * 100));
  const r = 22;
  const c = 2 * Math.PI * r;
  const offset = c - (pct / 100) * c;
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10, background: "rgba(255,255,255,0.12)", padding: "8px 12px", borderRadius: 12 }}>
      <svg width="52" height="52" viewBox="0 0 52 52">
        <circle cx="26" cy="26" r={r} stroke="rgba(255,255,255,0.22)" strokeWidth="5" fill="none" />
        <circle
          cx="26" cy="26" r={r} stroke={color} strokeWidth="5" fill="none"
          strokeDasharray={c} strokeDashoffset={offset} strokeLinecap="round" transform="rotate(-90 26 26)"
        />
        <text x="26" y="30" textAnchor="middle" fontSize="12" fontWeight="800" fill="#ffffff">{value}</text>
      </svg>
      <div>
        <div style={{ fontSize: 12, fontWeight: 700, color: "#fff" }}>{label}</div>
        <div style={{ fontSize: 11, color: "#cbd5e1" }}>연간 {pct}%</div>
      </div>
    </div>
  );
}

// DashboardView는 눈에 확 들어오는 히어로 배너와 4대 KPI 요약 카드를 갖춘 오늘의 교무 통합 대시보드입니다.
export default function DashboardView({ user, summary, teachersCount = 0, cellsCount = 0, onNavigate, onOpenApproval }) {
  const [nowHHMM, setNowHHMM] = useState(currentHHMM);
  useEffect(() => {
    const id = setInterval(() => setNowHHMM(currentHHMM()), 30000);
    return () => clearInterval(id);
  }, []);

  const myLessons = [...(summary?.myTodayLessons || [])].sort((a, b) => a.period - b.period);
  const homeroom = [...(summary?.homeroomLessons || [])].sort((a, b) => a.period - b.period);
  const todayEvents = summary?.todayEvents || [];
  const examSlots = summary?.myExamSlots || [];
  const pendingReqs = summary?.pendingApprovals || [];
  const openExt = summary?.openExternalExams || [];
  const bells = useMemo(
    () => [...(summary?.bellSchedule || [])].filter((b) => b.period > 0).sort((a, b) => a.period - b.period),
    [summary?.bellSchedule]
  );
  const bellMap = {};
  bells.forEach((b) => { bellMap[b.period] = b; });

  const timelineState = useMemo(() => {
    if (bells.length === 0) return { statusLabel: "일과시간 미설정", tone: "gray", completedCount: 0, pct: 0, currentPeriod: 0 };
    const nowMin = toMinutes(nowHHMM);
    const firstStart = toMinutes(bells[0].startTime);
    const lastEnd = toMinutes(bells[bells.length - 1].endTime);
    let currentPeriod = 0;
    let completedCount = 0;
    bells.forEach((b) => {
      const s = toMinutes(b.startTime);
      const e = toMinutes(b.endTime);
      if (nowMin >= s && nowMin < e) currentPeriod = b.period;
      if (nowMin >= e) completedCount += 1;
    });
    if (nowMin < firstStart) return { statusLabel: `일과 시작 전 (${nowHHMM})`, tone: "gray", completedCount: 0, pct: 0, currentPeriod: 0 };
    if (nowMin >= lastEnd) return { statusLabel: `정규 일과 종료 (${nowHHMM})`, tone: "green", completedCount: bells.length, pct: 100, currentPeriod: 0 };
    const pct = Math.max(0, Math.min(100, Math.round(((nowMin - firstStart) / Math.max(1, lastEnd - firstStart)) * 100)));
    if (currentPeriod > 0) return { statusLabel: `${currentPeriod}교시 수업 중 (${nowHHMM})`, tone: "amber", completedCount, pct, currentPeriod };
    return { statusLabel: `쉬는·점심시간 (${nowHHMM})`, tone: "blue", completedCount, pct, currentPeriod: 0 };
  }, [bells, nowHHMM]);

  const showSetupGuide = cellsCount === 0 && (hasRole(user, "MASTER") || hasRole(user, "TIMETABLE_ADMIN"));

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="hero-banner" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 14 }}>
        <div>
          <div style={{ fontSize: 12, color: "#bae6fd", fontWeight: 700, marginBottom: 4, display: "flex", alignItems: "center", gap: 6 }}>
            <CalendarDays size={14} /> 꼬미시간 {summary?.serverVersion ? `v${summary.serverVersion}` : ""} · {summary?.todayDate} · {summary?.databaseMode}
          </div>
          <h2 style={{ margin: 0, fontSize: 22, fontWeight: 800 }}>
            안녕하세요, {formatUserDisplayName(user)}{user?.position && user?.position !== user?.name ? ` (${user.position})` : ""}
          </h2>
          <div style={{ fontSize: 13, color: "#e2e8f0", marginTop: 4 }}>
            오늘의 수업·담임반·시험감독·학사일정·결재 현황을 한눈에 확인하세요.
          </div>
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <StatGauge label="교내 감독" value={user?.internalExamCount || 0} max={10} color="#74c0fc" />
          <StatGauge label="외부 감독" value={user?.externalExamCount || 0} max={6} color="#63e6be" />
          <StatGauge label="결보강" value={user?.substitutionCount || 0} max={12} color="#ffd43b" />
        </div>
      </div>

      {bells.length > 0 && (
        <div className="card" style={{ padding: "12px 16px" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 8 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, fontWeight: 800, fontSize: 13.5 }}>
              <Clock size={15} color="#3b5bdb" />
              <span>오늘의 일과 · 현재 교시 타임라인</span>
              <span className={`badge badge-${timelineState.tone}`} style={{ fontVariantNumeric: "tabular-nums" }}>
                <StatusDot tone={timelineState.tone} label={timelineState.statusLabel} />
              </span>
            </div>
            <span className="badge badge-gray" style={{ fontVariantNumeric: "tabular-nums" }}>
              완료 {timelineState.completedCount} / {bells.length}교시
            </span>
          </div>
          <ProgressBar value={timelineState.pct} max={100} tone={timelineState.tone} showPct={false} height={6} />
          <div style={{ display: "grid", gridTemplateColumns: `repeat(${Math.min(7, bells.length)}, minmax(0, 1fr))`, gap: 6, marginTop: 8 }}>
            {bells.map((b) => {
              const nowMin = toMinutes(nowHHMM);
              const isCur = b.period === timelineState.currentPeriod;
              const isDone = nowMin >= toMinutes(b.endTime);
              const lesson = myLessons.find((l) => l.period === b.period);
              return (
                <div
                  key={b.period}
                  style={{
                    padding: "6px 8px",
                    borderRadius: 8,
                    border: isCur ? "2px solid #e67700" : "1px solid var(--border)",
                    background: isCur ? "#fff9db" : isDone ? "#f1f5f9" : "#ffffff",
                    fontSize: 11.5,
                    fontVariantNumeric: "tabular-nums",
                  }}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <b>{b.label || `${b.period}교시`}</b>
                    <StatusDot tone={isCur ? "amber" : isDone ? "green" : "gray"} />
                  </div>
                  <div style={{ color: "#64748b", fontSize: 10.5 }}>{b.startTime}~{b.endTime}</div>
                  {lesson && <div style={{ color: "#1e3a8a", fontWeight: 700, marginTop: 2 }}>{lesson.className} · {lesson.subject}</div>}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {showSetupGuide && (
        <div className="card" style={{ borderColor: "#93c5fd", background: "#f8faff" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 12 }}>
            <div>
              <h3 style={{ margin: "0 0 4px", fontSize: 15, display: "flex", alignItems: "center", gap: 6, color: "#1e3a8a" }}>
                <Rocket size={16} /> 초기 학교 설정 시작 가이드
              </h3>
              <div style={{ fontSize: 12.5, color: "#475569" }}>
                현재 교사 {teachersCount}명 · 편성된 시간표 {cellsCount}셀입니다. 아래 순서대로 기초 데이터를 등록해 보세요.
              </div>
            </div>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {hasRole(user, "MASTER") && (
                <button type="button" className="btn btn-sm btn-primary" onClick={() => onNavigate("admin-remote")}>
                  1. 교사 명렬표·권한 등록
                </button>
              )}
              <button type="button" className="btn btn-sm" onClick={() => onNavigate("timetable")}>
                2. 시수표 설정 및 기초 시간표 작성/엑셀 가져오기
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="kpi-grid">
        <div className="kpi-card" onClick={() => onNavigate("timetable")}>
          <div className="kpi-icon" style={{ background: "#edf2ff", color: "#3b5bdb" }}><BookOpen size={22} /></div>
          <div>
            <div className="kpi-value">{myLessons.length}교시</div>
            <div className="kpi-label">오늘 나의 정규 수업</div>
          </div>
        </div>
        <div className="kpi-card" onClick={() => onNavigate("internal-exam")}>
          <div className="kpi-icon" style={{ background: "#fff9db", color: "#e67700" }}><ShieldAlert size={22} /></div>
          <div>
            <div className="kpi-value">{examSlots.length}건</div>
            <div className="kpi-label">배정된 교내 시험감독</div>
          </div>
        </div>
        <div className="kpi-card" onClick={() => (pendingReqs.length > 0 ? onOpenApproval() : onNavigate("timetable"))}>
          <div className="kpi-icon" style={{ background: "#ffe3e3", color: "#e03131" }}><CheckSquare size={22} /></div>
          <div>
            <div className="kpi-value">{pendingReqs.length}건</div>
            <div className="kpi-label">수업교환 · 결보강 대기</div>
          </div>
        </div>
        <div className="kpi-card" onClick={() => onNavigate("calendar")}>
          <div className="kpi-icon" style={{ background: "#e6fcf5", color: "#099268" }}><CalendarDays size={22} /></div>
          <div>
            <div className="kpi-value">{todayEvents.length}건</div>
            <div className="kpi-label">오늘 확정 학교 행사</div>
          </div>
        </div>
      </div>

      <div className="grid-2">
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 15.5, display: "flex", alignItems: "center", gap: 6 }}>
              <Clock size={16} color="#3b5bdb" /> 오늘의 내 수업 타임라인
            </h3>
            <button className="btn btn-sm" onClick={() => onNavigate("timetable")}>전체 시간표</button>
          </div>
          {myLessons.length === 0 ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>오늘 배정된 정규 수업이 없습니다.</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {myLessons.map((l) => {
                const b = bellMap[l.period];
                return (
                  <div key={l.id} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "10px 12px", borderRadius: 10, background: "#f8faff", borderLeft: "4px solid #3b5bdb", border: "1px solid #e2e8f0" }}>
                    <div>
                      <span className="badge badge-blue" style={{ marginRight: 8 }}>
                        {l.period}교시 {b ? `(${b.startTime}~${b.endTime})` : ""}
                      </span>
                      <b>{l.className}반</b> · {l.subject}
                    </div>
                    <span style={{ fontSize: 12, color: "#64748b" }}>{l.room || "-"}</span>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 15.5, display: "flex", alignItems: "center", gap: 6 }}>
              <Users size={16} color="#099268" /> 우리 반 오늘 시간표 {user?.homeroomClass ? `(${user.homeroomClass}반)` : "(비담임)"}
            </h3>
          </div>
          {!user?.homeroomClass ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>담임 학급이 지정되지 않은 교사 계정입니다.</div>
          ) : homeroom.length === 0 ? (
            <div style={{ color: "#64748b", fontSize: 13 }}>오늘 등록된 학급 수업이 없습니다.</div>
          ) : (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: 8 }}>
              {homeroom.map((h) => (
                <div key={h.id} style={{ padding: 10, borderRadius: 10, background: h.blockedBy ? "#fff5f5" : "#e6fcf5", border: "1px solid #c3fae8", fontSize: 12.5 }}>
                  <b>{h.period}교시</b> {h.subject}
                  <div style={{ fontSize: 11, color: "#475569", marginTop: 2 }}>{h.blockedBy || h.room}</div>
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
              <ShieldAlert size={16} color="#e67700" /> 다가오는 시험감독
            </h3>
            <button className="btn btn-sm" onClick={() => onNavigate("internal-exam")}>상세</button>
          </div>
          {examSlots.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>배정된 감독 교시가 없습니다.</div>
          ) : (
            examSlots.slice(0, 4).map((s) => (
              <div key={s.id} style={{ padding: "8px 10px", borderRadius: 9, background: "#fff9db", marginBottom: 6, fontSize: 12.5, display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <div>
                  <b>{s.period}교시</b> ({s.slotRole === "CORRIDOR" ? "복도감독" : s.slotRole === "MAIN" ? "정감독" : s.slotRole})
                  {s.startTime && <div style={{ fontSize: 11, color: "#9c4d00" }}>{s.startTime}~{s.endTime} ({s.durationMinutes}분)</div>}
                </div>
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
              <div key={ev.id} style={{ padding: "8px 10px", borderRadius: 9, background: "#f8fafc", marginBottom: 6, fontSize: 12.5 }}>
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
            <div key={ex.id} style={{ padding: "8px 10px", borderRadius: 9, background: "#edf2ff", marginBottom: 6, fontSize: 12.5 }}>
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
