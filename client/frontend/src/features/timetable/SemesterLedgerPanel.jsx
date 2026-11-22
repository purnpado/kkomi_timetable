import React, { useEffect, useState } from "react";
import { BookOpenCheck, CheckCircle2, RotateCcw, Save } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";

const DAYS = ["", "월", "화", "수", "목", "금"];

// SemesterLedgerPanel은 학기당 과목별 필수 시수 확보 원장 및 미보충 행사/결강 부채 잔고를 관리합니다.
export default function SemesterLedgerPanel({ serverUrl, user, onNotify }) {
  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");
  const [report, setReport] = useState(null);
  const [cfg, setCfg] = useState({ semesterName: "2026학년도 1학기", totalWeeks: 17, minRatioPct: 95, includeCurrent: true });

  const loadLedger = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/ledger", user.id);
      setReport(res);
      if (res?.config) setCfg(res.config);
    } catch (err) {
      onNotify?.("시수 원장 조회 실패", err.message, true);
    }
  };

  useEffect(() => { loadLedger(); }, [serverUrl, user?.id]);

  const handleSaveConfig = async (e) => {
    e.preventDefault();
    try {
      const saved = await apiFetch(serverUrl, "/api/timetable/ledger/config", user.id, {
        method: "POST",
        body: JSON.stringify({
          semesterName: cfg.semesterName,
          totalWeeks: Number(cfg.totalWeeks) || 17,
          minRatioPct: Number(cfg.minRatioPct) || 95,
          includeCurrent: Boolean(cfg.includeCurrent),
        }),
      });
      setCfg(saved);
      onNotify?.("학기 원장 설정 저장", `${saved.semesterName} (${saved.totalWeeks}주) 기준이 저장되었습니다.`);
      loadLedger();
    } catch (err) {
      onNotify?.("설정 저장 실패", err.message, true);
    }
  };

  const handleToggleDebt = async (debtId, resolved) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/ledger/debt", user.id, {
        method: "POST",
        body: JSON.stringify({ debtId, resolved }),
      });
      loadLedger();
    } catch (err) {
      onNotify?.("부채 상태 변경 실패", err.message, true);
    }
  };

  const summary = report?.summary || {};
  const rows = report?.rows || [];
  const debts = report?.debts || [];
  const balances = report?.teacherBalances || [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <h3 style={{ margin: 0, fontSize: 16, display: "flex", alignItems: "center", gap: 6 }}>
            <BookOpenCheck size={17} color="#3b5bdb" /> 학기당 과목별 필수 시수 원장 & 미보충 부채 추적
          </h3>
          <form onSubmit={handleSaveConfig} style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
            <input className="input" style={{ width: 150 }} disabled={!isAdmin} value={cfg.semesterName} onChange={(e) => setCfg({ ...cfg, semesterName: e.target.value })} />
            <label style={{ fontSize: 12 }}>
              수업주수
              <input className="input" type="number" min={1} max={30} disabled={!isAdmin} style={{ width: 58, marginLeft: 4 }} value={cfg.totalWeeks} onChange={(e) => setCfg({ ...cfg, totalWeeks: e.target.value })} />
            </label>
            <label style={{ fontSize: 12 }}>
              최소확보율(%)
              <input className="input" type="number" min={50} max={100} disabled={!isAdmin} style={{ width: 58, marginLeft: 4 }} value={cfg.minRatioPct} onChange={(e) => setCfg({ ...cfg, minRatioPct: e.target.value })} />
            </label>
            {isAdmin && (
              <button type="submit" className="btn btn-sm btn-primary"><Save size={13} /> 기준 저장</button>
            )}
          </form>
        </div>

        <div className="grid-4" style={{ marginTop: 12 }}>
          <div className="card" style={{ padding: 12, background: "#f8fafc" }}>
            <div style={{ fontSize: 11.5, color: "#64748b" }}>전체 학기 목표 시수</div>
            <div style={{ fontSize: 20, fontWeight: 700 }}>{summary.totalTargetHours || 0}시간</div>
          </div>
          <div className="card" style={{ padding: 12, background: "#edf2ff" }}>
            <div style={{ fontSize: 11.5, color: "#364fc7" }}>예상 실제 확보 시수</div>
            <div style={{ fontSize: 20, fontWeight: 700, color: "#364fc7" }}>{summary.totalActualHours || 0}시간</div>
          </div>
          <div className="card" style={{ padding: 12, background: "#fff5f5" }}>
            <div style={{ fontSize: 11.5, color: "#c92a2a" }}>부족(미보충) 시수</div>
            <div style={{ fontSize: 20, fontWeight: 700, color: "#c92a2a" }}>{summary.totalDeficitHours || 0}시간</div>
          </div>
          <div className="card" style={{ padding: 12, background: "#fff9db" }}>
            <div style={{ fontSize: 11.5, color: "#b86e00" }}>미해결 부채 항목</div>
            <div style={{ fontSize: 20, fontWeight: 700, color: "#b86e00" }}>{summary.unresolvedDebtCount || 0}건</div>
          </div>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>학급 · 과목별 학기 시수 확보 현황</h3>
        <div style={{ maxHeight: 280, overflowY: "auto" }}>
          <table className="table">
            <thead>
              <tr><th>학급</th><th>과목</th><th>담당</th><th>주당</th><th>학기목표</th><th>행사/결손</th><th>보충</th><th>실확보</th><th>달성률</th><th>상태</th></tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.className}-${r.subject}`}>
                  <td><b>{r.className}</b></td>
                  <td>{r.subject}</td>
                  <td>{r.teacherName}</td>
                  <td>{r.weeklyHours}</td>
                  <td>{r.targetHours}</td>
                  <td style={{ color: r.blockedHours > 0 ? "#c92a2a" : "#64748b" }}>{r.blockedHours}</td>
                  <td style={{ color: r.addedHours > 0 ? "#087f5b" : "#64748b" }}>{r.addedHours}</td>
                  <td><b>{r.actualHours}</b></td>
                  <td>{r.achievedPct}%</td>
                  <td>
                    <span className={`badge ${r.status === "충족" ? "badge-green" : r.status === "주의" ? "badge-amber" : "badge-red"}`}>
                      {r.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>행사 · 미보강 결손 부채 목록 ({debts.length}건)</h3>
          {debts.length === 0 ? (
            <div style={{ fontSize: 12.5, color: "#64748b" }}>현재 추적된 미보충 수업 부채가 없습니다.</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 6, maxHeight: 240, overflowY: "auto" }}>
              {debts.map((d) => (
                <div key={d.id} style={{ padding: "8px 10px", background: "#f8fafc", borderRadius: 8, fontSize: 12, display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                  <div>
                    <span className={`badge ${d.status === "RESOLVED" ? "badge-green" : "badge-red"}`} style={{ marginRight: 6 }}>
                      {d.status === "RESOLVED" ? "보충완료" : "미보충"}
                    </span>
                    <b>{d.date} ({DAYS[d.dayOfWeek]} {d.period}교시)</b> · {d.className} {d.subject} ({d.teacherName})
                    <div style={{ color: "#64748b", marginTop: 2 }}>{d.reason}</div>
                  </div>
                  {isAdmin && (
                    <button type="button" className={`btn btn-sm ${d.status === "RESOLVED" ? "" : "btn-green"}`} onClick={() => handleToggleDebt(d.id, d.status !== "RESOLVED")}>
                      {d.status === "RESOLVED" ? <><RotateCcw size={12} /> 복원</> : <><CheckCircle2 size={12} /> 보충완료</>}
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>교사별 결강 · 대강 보충 잔고</h3>
          {balances.length === 0 ? (
            <div style={{ fontSize: 12.5, color: "#64748b" }}>결강 및 대강 누적 이력이 아직 없습니다.</div>
          ) : (
            <table className="table">
              <thead>
                <tr><th>교사</th><th>결강(차감)</th><th>대강/보강(가산)</th><th>순잔고</th><th>미해결부채</th></tr>
              </thead>
              <tbody>
                {balances.map((b) => (
                  <tr key={b.teacherId}>
                    <td><b>{b.teacherName}</b></td>
                    <td>{b.missedHours}</td>
                    <td>{b.coveredHours}</td>
                    <td>
                      <span className={`badge ${b.netBalance >= 0 ? "badge-green" : "badge-amber"}`}>
                        {b.netBalance >= 0 ? `+${b.netBalance}` : b.netBalance}
                      </span>
                    </td>
                    <td>{b.pendingDebts}건</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}
