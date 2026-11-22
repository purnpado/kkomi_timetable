import React, { useMemo, useState } from "react";
import { ArrowRight, Building2, GitBranch, Search } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";

const DAYS = ["", "월", "화", "수", "목", "금"];
const PERIODS = [1, 2, 3, 4, 5, 6, 7];

// ChainSwapPreview는 [삼각·사각 연쇄·블록 이동 미리보기/신청]과
// [교과교실(특별실)별 동시 수용 현황 및 빈 강의실 열람]을 제공합니다.
export default function ChainSwapPreview({ serverUrl, user, cells, teachersMap, weekStart = "", changeScope = "DATE_ONLY", activeChain, onClearChain, onRefresh, onNotify }) {
  const [simSourceId, setSimSourceId] = useState(cells?.[0]?.id || "");
  const [simDay, setSimDay] = useState(2);
  const [simPeriod, setSimPeriod] = useState(2);
  const [moveWholeBlock, setMoveWholeBlock] = useState(true);
  const [instantApply, setInstantApply] = useState(false);
  const [simEval, setSimEval] = useState(null);
  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");

  const roomOptions = useMemo(() => {
    const set = new Set(["과학실1", "컴퓨터실", "음악실", "미술실", "체육관", "수학전용1실"]);
    (cells || []).forEach((c) => {
      if (c.room && !c.room.endsWith("교실")) set.add(c.room);
    });
    return Array.from(set).sort();
  }, [cells]);
  const [selectedRoom, setSelectedRoom] = useState("과학실1");

  const currentEval = activeChain?.evalResult || simEval;
  const currentSteps = currentEval?.chainSteps || [];

  const handleSimulate = async (e) => {
    e.preventDefault();
    const src = (cells || []).find((c) => c.id === simSourceId) || cells?.[0];
    if (!src) return;
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/evaluate", user.id, {
        method: "POST",
        body: JSON.stringify({
          sourceCellId: src.id,
          targetDay: Number(simDay),
          targetPeriod: Number(simPeriod),
          targetClass: src.className,
          targetTeacher: src.teacherId,
          targetDate: changeScope === "DATE_ONLY" ? weekStart : "",
          moveWholeBlock,
        }),
      });
      setSimEval(res);
      if (onClearChain) onClearChain();
    } catch (err) {
      onNotify("경로 탐색 실패", err.message, true);
    }
  };

  const handleSubmitChain = async () => {
    if (!currentSteps.length) return;
    const first = currentSteps[0];
    const last = currentSteps[currentSteps.length - 1];
    const firstCell = (cells || []).find((c) => c.id === first.cellId) || {};
    const isDateOnly = changeScope === "DATE_ONLY";
    const effDate = isDateOnly ? firstCell.lessonDate || weekStart || new Date().toISOString().slice(0, 10) : "";
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: currentEval.mode || "TRIANGLE",
          scope: changeScope,
          sourceDate: effDate,
          targetDate: effDate,
          requesterId: user.id,
          requesterName: first.teacherName || user.name,
          partnerId: last.teacherId || first.teacherId,
          partnerName: `${currentSteps.length}단계(${last.teacherName})`,
          partnerAgreed: true,
          autoApprove: Boolean(isAdmin && instantApply),
          sourceCell: firstCell,
          targetCell: (cells || []).find((c) => c.id === last.cellId) || {
            className: first.className,
            dayOfWeek: first.toDay,
            period: first.toPeriod,
            subject: first.subject,
          },
          chainSteps: currentSteps,
          reason: `[${currentSteps.length}단계 ${isDateOnly ? "일회성" : "정규"}] ${currentEval.reason}`,
        }),
      });
      setSimEval(null);
      if (onClearChain) onClearChain();
      onNotify(
        isAdmin && instantApply ? "연쇄 이동 즉시 반영 완료" : "연쇄 교환 대기열 등록 완료",
        isAdmin && instantApply ? `${currentSteps.length}단계 변경이 즉시 반영되었습니다.` : `${currentSteps.length}단계 변경 과정이 승인 대기열에 등록되었습니다.`
      );
      onRefresh();
    } catch (err) {
      onNotify("신청 실패", err.message, true);
    }
  };

  return (
    <div className="grid-2">
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>
            <GitBranch size={15} style={{ verticalAlign: "middle" }} /> 삼각·사각(1~4단계) 연쇄 교환 과정 미리보기
          </h3>
          {currentEval && (
            <span className={`badge ${currentEval.signal === "GREEN" ? "badge-green" : currentEval.signal === "YELLOW" ? "badge-amber" : "badge-red"}`}>
              {currentEval.mode} ({currentSteps.length}단계)
            </span>
          )}
        </div>

        <form onSubmit={handleSimulate} style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 12 }}>
          <select className="select" style={{ flex: 1, minWidth: 160 }} value={simSourceId} onChange={(e) => setSimSourceId(e.target.value)}>
            {(cells || []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.className}반 {DAYS[c.dayOfWeek]} {c.period}교시 · {c.subject} ({teachersMap[c.teacherId]?.name || c.teacherId})
              </option>
            ))}
          </select>
          <select className="select" style={{ width: 82 }} value={simDay} onChange={(e) => setSimDay(e.target.value)}>
            {[1, 2, 3, 4, 5].map((d) => <option key={d} value={d}>{DAYS[d]}요일</option>)}
          </select>
          <select className="select" style={{ width: 78 }} value={simPeriod} onChange={(e) => setSimPeriod(e.target.value)}>
            {PERIODS.map((p) => <option key={p} value={p}>{p}교시</option>)}
          </select>
          <button type="submit" className="btn btn-sm btn-primary">
            <Search size={13} /> 경로 탐색
          </button>
          <label style={{ fontSize: 11.5, display: "inline-flex", alignItems: "center", gap: 4 }}>
            <input type="checkbox" checked={moveWholeBlock} onChange={(e) => setMoveWholeBlock(e.target.checked)} />
            블록 통째이동
          </label>
        </form>

        {!currentEval ? (
          <div style={{ fontSize: 12.5, color: "#64748b", padding: "10px 0" }}>
            위 시간표 Canvas에서 수업을 드래그하거나 이동 대상 교시를 선택하면 1~4단계 연쇄 이동 과정을 단계별로 확인할 수 있습니다.
          </div>
        ) : currentSteps.length === 0 ? (
          <div className="badge badge-red" style={{ padding: "8px 12px", borderRadius: 8 }}>{currentEval.reason}</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ fontSize: 12.5, fontWeight: 700, color: "#364fc7" }}>{currentEval.reason}</div>
            {currentSteps.map((st) => (
              <div
                key={st.step}
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 8,
                  padding: "8px 10px",
                  borderRadius: 8,
                  background: "#f8fafc",
                  border: "1px solid #cbd5e1",
                  fontSize: 12.5,
                }}
              >
                <div>
                  <span className="badge badge-blue" style={{ marginRight: 6 }}>{st.step}단계</span>
                  <b>{st.teacherName}</b> · {st.className}반 {st.subject}
                  {st.room ? <span style={{ color: "#64748b" }}> ({st.room})</span> : null}
                </div>
                <div style={{ fontWeight: 700, color: "#0f172a", whiteSpace: "nowrap" }}>
                  {DAYS[st.fromDay]} {st.fromPeriod}교시 <ArrowRight size={12} style={{ verticalAlign: "middle" }} /> {DAYS[st.toDay]} {st.toPeriod}교시
                </div>
              </div>
            ))}
            <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap", marginTop: 4 }}>
              <button type="button" className="btn btn-sm btn-green" onClick={handleSubmitChain}>
                {isAdmin && instantApply ? `이 ${currentSteps.length}단계 경로 즉시 반영` : `이 ${currentSteps.length}단계 연쇄 교환 대기열 신청`}
              </button>
              {isAdmin && (
                <label style={{ fontSize: 12, fontWeight: 700, color: "#087f5b", display: "inline-flex", alignItems: "center", gap: 4 }}>
                  <input type="checkbox" checked={instantApply} onChange={(e) => setInstantApply(e.target.checked)} />
                  일과계 즉시 반영
                </label>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="card" style={{ overflowX: "auto" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>
            <Building2 size={15} style={{ verticalAlign: "middle" }} /> 교과교실(특별실)별 시간표 & 빈 강의실
          </h3>
          <select className="select" style={{ width: 150 }} value={selectedRoom} onChange={(e) => setSelectedRoom(e.target.value)}>
            {roomOptions.map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
        </div>
        <table className="table" style={{ fontSize: 11.5 }}>
          <thead>
            <tr><th>교시</th>{DAYS.slice(1).map((d) => <th key={d}>{d}</th>)}</tr>
          </thead>
          <tbody>
            {PERIODS.map((p) => (
              <tr key={p}>
                <td><b>{p}교시</b></td>
                {[1, 2, 3, 4, 5].map((d) => {
                  const occList = (cells || []).filter((c) => c.room === selectedRoom && c.dayOfWeek === d && c.period === p);
                  return (
                    <td key={d} style={{ background: occList.length > 0 ? "#fff9db" : "#e6fcf5", minWidth: 70 }}>
                      {occList.length > 0 ? (
                        occList.map((occ) => (
                          <div key={occ.id} style={{ marginBottom: occList.length > 1 ? 3 : 0 }}>
                            <b>{occ.className} {occ.subject}</b>
                            <div style={{ fontSize: 10.5, color: "#475569" }}>{teachersMap[occ.teacherId]?.name || occ.teacherId}</div>
                          </div>
                        ))
                      ) : (
                        <span style={{ color: "#087f5b" }}>비어있음</span>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
