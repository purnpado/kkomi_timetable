import React, { useEffect, useMemo, useState } from "react";
import { ArrowLeftRight, CalendarDays } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

const ROLE_LABELS = {
  MAIN: "정감독",
  SUB: "부감독",
  CORRIDOR: "복도감독",
  HONORARY: "학부모명예",
  STUDY_HALL: "공강자습",
};

// InternalExamSwapAndPersonalPanel은 배정 후 두 슬롯 간 수동 맞교환과 교사 개인별 감독 시간표를 제공합니다.
export default function InternalExamSwapAndPersonalPanel({ serverUrl, user, slots, fairness, isExamAdmin, onRefresh, onNotify }) {
  const swappableSlots = useMemo(() => (slots || []).filter((s) => s.slotRole !== "HONORARY"), [slots]);
  const [slotId1, setSlotId1] = useState(swappableSlots[0]?.id || "");
  const [slotId2, setSlotId2] = useState(swappableSlots[1]?.id || "");
  const [viewTeacherId, setViewTeacherId] = useState(user?.id || "");
  const [personal, setPersonal] = useState(null);

  useEffect(() => {
    if (swappableSlots.length >= 2) {
      if (!swappableSlots.some((s) => s.id === slotId1)) setSlotId1(swappableSlots[0].id);
      if (!swappableSlots.some((s) => s.id === slotId2)) setSlotId2(swappableSlots[1].id);
    }
  }, [swappableSlots]);

  const loadPersonal = async (tid = viewTeacherId) => {
    try {
      const q = isExamAdmin && tid ? `?teacherId=${encodeURIComponent(tid)}` : "";
      const res = await apiFetch(serverUrl, `/api/internal-exam/personal${q}`, user.id);
      setPersonal(res);
    } catch (err) {
      onNotify?.("개인 감독 시간표 조회 실패", err.message, true);
    }
  };

  useEffect(() => {
    if (user?.id) loadPersonal(viewTeacherId || user.id);
  }, [serverUrl, user?.id, viewTeacherId, slots]);

  const handleSwap = async (e) => {
    e.preventDefault();
    if (!slotId1 || !slotId2 || slotId1 === slotId2) {
      onNotify?.("선택 확인", "서로 다른 두 감독 슬롯을 선택해 주세요.", true);
      return;
    }
    try {
      await apiFetch(serverUrl, "/api/internal-exam/swap", user.id, {
        method: "POST",
        body: JSON.stringify({ slotId1, slotId2 }),
      });
      onNotify?.("수동 맞교환 완료", "두 교사의 감독 배정이 검증 후 맞교환되었습니다.");
      onRefresh?.();
    } catch (err) {
      onNotify?.("맞교환 불가", err.message, true);
    }
  };

  const personalDates = useMemo(() => {
    const set = new Set((personal?.slots || []).map((s) => s.examDate).filter(Boolean));
    return Array.from(set).sort();
  }, [personal]);
  const personalPeriods = useMemo(() => {
    const set = new Set((personal?.slots || []).map((s) => s.period));
    return Array.from(set).sort((a, b) => a - b);
  }, [personal]);

  const teacherOptions = fairness?.stats || [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {isExamAdmin && (
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <ArrowLeftRight size={15} color="#3b5bdb" /> 자동 배정 후 수동 맞교환 (담임·과목·연강금지 자동 검증)
          </h3>
          {swappableSlots.length < 2 ? (
            <div style={{ fontSize: 12.5, color: "#64748b" }}>맞교환 가능한 감독 슬롯이 2건 이상 필요합니다.</div>
          ) : (
            <form onSubmit={handleSwap} style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
              <select className="select" style={{ flex: 1, minWidth: 240 }} value={slotId1} onChange={(e) => setSlotId1(e.target.value)}>
                {swappableSlots.map((sl) => (
                  <option key={sl.id} value={sl.id}>
                    {sl.examDate} {sl.period}교시 · {sl.roomName} ({ROLE_LABELS[sl.slotRole] || sl.slotRole}) — {sl.teacherName}
                  </option>
                ))}
              </select>
              <ArrowLeftRight size={16} color="#64748b" />
              <select className="select" style={{ flex: 1, minWidth: 240 }} value={slotId2} onChange={(e) => setSlotId2(e.target.value)}>
                {swappableSlots.map((sl) => (
                  <option key={sl.id} value={sl.id}>
                    {sl.examDate} {sl.period}교시 · {sl.roomName} ({ROLE_LABELS[sl.slotRole] || sl.slotRole}) — {sl.teacherName}
                  </option>
                ))}
              </select>
              <button type="submit" className="btn btn-sm btn-primary">
                <ArrowLeftRight size={13} /> 두 슬롯 맞교환
              </button>
            </form>
          )}
        </div>
      )}

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <CalendarDays size={15} color="#3b5bdb" /> 개인별 교내 시험감독 시간표 ({personal?.teacherName || user?.name})
          </h3>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            {personal?.isSealed && <span className="badge badge-amber">사전 봉인 상태</span>}
            {isExamAdmin && teacherOptions.length > 0 && (
              <select className="select" style={{ width: 220 }} value={viewTeacherId} onChange={(e) => setViewTeacherId(e.target.value)}>
                {teacherOptions.map((st) => (
                  <option key={st.teacherId} value={st.teacherId}>{st.teacherName} ({st.subject})</option>
                ))}
              </select>
            )}
          </div>
        </div>

        {(personal?.slots || []).length === 0 ? (
          <div style={{ fontSize: 13, color: "#64748b" }}>배정된 교내 시험감독 일정이 없습니다.</div>
        ) : personalDates.length > 0 ? (
          <table className="table">
            <thead>
              <tr><th>교시</th>{personalDates.map((d) => <th key={d}>{d}</th>)}</tr>
            </thead>
            <tbody>
              {personalPeriods.map((p) => (
                <tr key={p}>
                  <td><b>{p}교시</b></td>
                  {personalDates.map((d) => {
                    const hit = (personal?.slots || []).find((s) => s.examDate === d && s.period === p);
                    return (
                      <td key={d} style={{ background: hit ? "#edf2ff" : "#fff" }}>
                        {hit ? (
                          <div>
                            <span className="badge badge-blue" style={{ marginRight: 4 }}>{ROLE_LABELS[hit.slotRole] || hit.slotRole}</span>
                            <b>{hit.roomName}</b>
                            <div style={{ fontSize: 11, color: "#475569" }}>{hit.grade}학년 {hit.subject} ({hit.startTime}~{hit.endTime})</div>
                          </div>
                        ) : (
                          <span style={{ color: "#94a3b8", fontSize: 11.5 }}>공강</span>
                        )}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </div>
    </div>
  );
}
