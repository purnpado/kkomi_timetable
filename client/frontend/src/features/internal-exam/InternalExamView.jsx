import React, { useEffect, useState } from "react";
import { Clock, Lock, Plus, RefreshCw, Shield, Trash2, Unlock, UserCheck } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";
import ExamFairnessPanel from "./ExamFairnessPanel";

function calcMinutes(start, end) {
  if (!start || !end) return 45;
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  const diff = eh * 60 + em - (sh * 60 + sm);
  return diff > 0 ? diff : 45;
}

// InternalExamView는 교내 지필고사 과목별 상이한 시험시간 편성, 복도감독 우선배치,
// 사전 봉인/당일 아침 발표 및 당일 아침 긴급 대타를 제어합니다.
export default function InternalExamView({ serverUrl, user, onNotify }) {
  const [config, setConfig] = useState(null);
  const [slots, setSlots] = useState([]);
  const [specs, setSpecs] = useState([]);
  const [fairness, setFairness] = useState(null);
  const [roomCatalog, setRoomCatalog] = useState(null);
  const [selectedSlotId, setSelectedSlotId] = useState("");
  const [subCandidates, setSubCandidates] = useState([]);
  const [newSubId, setNewSubId] = useState("");

  const isExamAdmin = hasRole(user, "INTERNAL_EXAM_ADMIN");

  const loadData = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/internal-exam/slots", user.id);
      setConfig(res.config);
      setSlots(res.slots || []);
      setSpecs(res.specs || []);
      setFairness(res.fairness || null);
      setRoomCatalog(res.roomCatalog || null);
    } catch (err) {
      onNotify("시험감독 조회 오류", err.message, true);
    }
  };

  useEffect(() => { loadData(); }, [user?.id]);

  const updateSpec = (idx, field, val) => {
    setSpecs((prev) =>
      prev.map((sp, i) => {
        if (i !== idx) return sp;
        const next = { ...sp, [field]: val };
        if (field === "startTime" || field === "endTime") {
          next.durationMinutes = calcMinutes(next.startTime, next.endTime);
        }
        return next;
      })
    );
  };

  const handleRegenerate = async () => {
    try {
      const normalizedSpecs = specs.map((sp) => ({
        ...sp,
        period: Number(sp.period),
        grade: Number(sp.grade),
        durationMinutes: Number(sp.durationMinutes) || calcMinutes(sp.startTime, sp.endTime),
        classRooms: Array.isArray(sp.classRooms)
          ? sp.classRooms
          : String(sp.classRooms || "")
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean),
      }));
      await apiFetch(serverUrl, "/api/internal-exam/generate", user.id, {
        method: "POST",
        body: JSON.stringify({ config, specs: normalizedSpecs }),
      });
      onNotify("교내 감독 배정 완료", "과목별 시험시간과 담임 자기반 배제·복도감독 우선배치가 반영되었습니다.");
      loadData();
    } catch (err) {
      onNotify("배정 실패", err.message, true);
    }
  };

  const handleUnseal = async () => {
    try {
      await apiFetch(serverUrl, "/api/internal-exam/unseal", user.id, { method: "POST" });
      onNotify("오늘 시험 고사장 발표 완료", "전 교원 PC에 고사장 배정이 일괄 공개되었습니다.", true);
      loadData();
    } catch (err) {
      onNotify("발표 실패", err.message, true);
    }
  };

  const handleLoadSubCandidates = async (slotId) => {
    setSelectedSlotId(slotId);
    try {
      const recs = await apiFetch(serverUrl, `/api/internal-exam/substitute/recommend?slotId=${slotId}`, user.id);
      setSubCandidates(recs || []);
      if (recs?.length > 0) setNewSubId(recs[0].id);
    } catch (err) {
      onNotify("대타 추천 오류", err.message, true);
    }
  };

  const handleApplySubstitute = async () => {
    if (!selectedSlotId || !newSubId) return;
    try {
      await apiFetch(serverUrl, "/api/internal-exam/substitute/apply", user.id, {
        method: "POST",
        body: JSON.stringify({ slotId: selectedSlotId, newTeacherId: newSubId }),
      });
      onNotify("긴급 대타 교체 완료", "대체 교사에게 최상단 긴급 알림이 전송되었습니다.", true);
      setSelectedSlotId("");
      loadData();
    } catch (err) {
      onNotify("대타 교체 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {config && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
            <div>
              <h2 style={{ margin: 0, fontSize: 17 }}>{config.title} ({config.examDate})</h2>
              <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 4 }}>
                핵심 규칙: <b>담임 자기 반 제외</b> · <b>담당과목 교시 복도감독 우선 배치</b> · 과목별 상이 시험시간 지원
              </div>
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <span className={`badge ${config.isSealed ? "badge-amber" : "badge-green"}`}>
                {config.isSealed ? <Lock size={12} /> : <Unlock size={12} />}
                {config.isSealed ? `사전 봉인 중 (${config.unsealTime} 공개)` : "고사장 공개 상태"}
              </span>
              {isExamAdmin && config.isSealed && (
                <button className="btn btn-primary" onClick={handleUnseal}>
                  <Unlock size={15} /> 오늘 시험 고사장 발표하기
                </button>
              )}
            </div>
          </div>

          {isExamAdmin && (
            <>
              <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid #e2e8f0", display: "flex", flexWrap: "wrap", gap: 14, alignItems: "center" }}>
                <select
                  className="select"
                  style={{ width: 215 }}
                  value={config.deputyMode ? "DEPUTY" : "SINGLE"}
                  onChange={(e) => setConfig({ ...config, deputyMode: e.target.value === "DEPUTY" })}
                >
                  <option value="SINGLE">운영 방식: 정감독 1인 단독</option>
                  <option value="DEPUTY">운영 방식: 정·부감독 2인 공동</option>
                </select>
                <select
                  className="select"
                  style={{ width: 245 }}
                  value={config.balanceMode || "TOTAL_AND_ROLE"}
                  onChange={(e) => setConfig({ ...config, balanceMode: e.target.value })}
                >
                  <option value="TOTAL_AND_ROLE">균등 기준: 1년 총 횟수 + 정·부 교차</option>
                  <option value="STRICT_ROLE">균등 기준: 정감독·부감독 각각 균등</option>
                </select>
                <label style={{ fontSize: 13, display: "flex", alignItems: "center", gap: 6 }}>
                  <input type="checkbox" checked={config.honoraryParentOn} onChange={(e) => setConfig({ ...config, honoraryParentOn: e.target.checked })} />
                  학부모 명예감독 투입 (자녀 학년/반 자동 배제)
                </label>
                <label style={{ fontSize: 13, display: "flex", alignItems: "center", gap: 6 }}>
                  <input type="checkbox" checked={!config.instantRevealMode} onChange={(e) => setConfig({ ...config, instantRevealMode: !e.target.checked, isSealed: !e.target.checked })} />
                  사전 봉인 후 당일 아침 발표
                </label>
                <select
                  className="select"
                  style={{ width: 230 }}
                  value={config.subjectTeacherPolicy || "CORRIDOR_FIRST"}
                  onChange={(e) => setConfig({ ...config, subjectTeacherPolicy: e.target.value })}
                >
                  <option value="CORRIDOR_FIRST">담당과목 교시: 복도감독 우선 배치</option>
                  <option value="HQ_STANDBY">담당과목 교시: 평가본부 대기/공강</option>
                </select>
                <button className="btn btn-green btn-sm" onClick={handleRegenerate}>
                  <RefreshCw size={14} /> 규칙·시험시간 적용 자동 배정
                </button>
              </div>

              <div style={{ marginTop: 14, padding: 12, borderRadius: 10, background: "#f8fafc", border: "1px solid #e2e8f0" }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 8 }}>
                  <b style={{ fontSize: 13 }}><Clock size={14} style={{ verticalAlign: "middle" }} /> 교시·과목별 시험시간 및 고사실 편성 (선택과목·교과교실·별도고사실 지원)</b>
                  <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                    <button
                      type="button"
                      className="btn btn-sm"
                      onClick={() => {
                        const electives = roomCatalog?.electiveRooms || [];
                        const firstG = electives[0]?.grade || 2;
                        const roomList = electives.length > 0
                          ? electives.slice(0, 3).map((e) => e.label)
                          : ["세미나실A(심화수학)", "수학전용1실(심화수학)"];
                        const subjList = electives.length > 0
                          ? Array.from(new Set(electives.slice(0, 3).map((e) => e.subject))).join(", ")
                          : "심화수학";
                        setSpecs((prev) => [
                          ...prev,
                          { period: prev.length + 1, grade: firstG, subject: subjList, startTime: "13:10", endTime: "14:00", durationMinutes: 50, classRooms: roomList },
                        ]);
                      }}
                    >
                      <Plus size={13} /> 고교선택과목·교과교실 교시 추가
                    </button>
                    <button
                      type="button"
                      className="btn btn-sm"
                      onClick={() =>
                        setSpecs((prev) => [
                          ...prev,
                          { period: prev.length + 1, grade: 1, subject: "과학", startTime: "11:45", endTime: "12:25", durationMinutes: 40, classRooms: ["1-1", "1-2"] },
                        ])
                      }
                    >
                      <Plus size={13} /> 일반 교시 추가
                    </button>
                  </div>
                </div>
                <table className="table">
                  <thead>
                    <tr><th>교시</th><th>학년</th><th>시험과목</th><th>시작 시각</th><th>종료 시각</th><th>시험시간(분)</th><th>고사실(쉼표 구분)</th><th>삭제</th></tr>
                  </thead>
                  <tbody>
                    {specs.map((sp, idx) => (
                      <tr key={idx}>
                        <td><input className="input" type="number" min={1} max={7} style={{ width: 56 }} value={sp.period} onChange={(e) => updateSpec(idx, "period", e.target.value)} /></td>
                        <td><input className="input" type="number" min={1} max={3} style={{ width: 56 }} value={sp.grade} onChange={(e) => updateSpec(idx, "grade", e.target.value)} /></td>
                        <td><input className="input" style={{ width: 95 }} value={sp.subject} onChange={(e) => updateSpec(idx, "subject", e.target.value)} /></td>
                        <td><input className="input" type="time" style={{ width: 118 }} value={sp.startTime || "08:50"} onChange={(e) => updateSpec(idx, "startTime", e.target.value)} /></td>
                        <td><input className="input" type="time" style={{ width: 118 }} value={sp.endTime || "09:35"} onChange={(e) => updateSpec(idx, "endTime", e.target.value)} /></td>
                        <td><input className="input" type="number" min={10} max={120} style={{ width: 68 }} value={sp.durationMinutes || 45} onChange={(e) => updateSpec(idx, "durationMinutes", e.target.value)} /></td>
                        <td>
                          <input
                            className="input"
                            value={Array.isArray(sp.classRooms) ? sp.classRooms.join(",") : sp.classRooms}
                            onChange={(e) => updateSpec(idx, "classRooms", e.target.value.split(",").map((s) => s.trim()))}
                          />
                          <div style={{ display: "flex", gap: 4, marginTop: 4, flexWrap: "wrap" }}>
                            <select
                              className="select"
                              style={{ fontSize: 11, padding: "2px 6px", height: 24, maxWidth: 210 }}
                              value=""
                              onChange={(e) => {
                                const v = e.target.value;
                                if (!v) return;
                                const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                                if (v === "__FILL_GRADE__") {
                                  const clsList = roomCatalog?.gradeClassRooms?.[Number(sp.grade)] || [];
                                  updateSpec(idx, "classRooms", Array.from(new Set([...cur, ...clsList])));
                                } else {
                                  updateSpec(idx, "classRooms", Array.from(new Set([...cur, v])));
                                }
                              }}
                            >
                              <option value="">+ 고사실 빠른 추가 (선택·교과·별도)...</option>
                              <option value="__FILL_GRADE__">{sp.grade}학년 전체 정규반 일괄 추가</option>
                              {(roomCatalog?.electiveRooms || []).length > 0 && (
                                <optgroup label="고교선택과목 · 이동수업 분반">
                                  {(roomCatalog.electiveRooms || []).map((el) => (
                                    <option key={el.label} value={el.label}>{el.label}</option>
                                  ))}
                                </optgroup>
                              )}
                              {(roomCatalog?.specialRooms || []).length > 0 && (
                                <optgroup label="교과교실 · 특별실">
                                  {(roomCatalog.specialRooms || []).map((rm) => (
                                    <option key={rm} value={rm}>{rm}</option>
                                  ))}
                                </optgroup>
                              )}
                              <optgroup label="특수 · 분리 고사실">
                                {(roomCatalog?.isolatedRooms || ["별도고사실(독감·감염병)", "특수학급고사실"]).map((ir) => (
                                  <option key={ir} value={ir}>{ir}</option>
                                ))}
                              </optgroup>
                            </select>
                            <button
                              type="button"
                              className="btn btn-sm"
                              style={{ fontSize: 11, padding: "2px 6px", height: 24 }}
                              onClick={() => {
                                const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                                const tag = "별도고사실(독감·감염병)";
                                const next = cur.includes(tag) ? cur.filter((r) => r !== tag) : [...cur, tag];
                                updateSpec(idx, "classRooms", next);
                              }}
                            >
                              + 별도고사실
                            </button>
                          </div>
                        </td>
                        <td>
                          <button type="button" className="btn btn-sm btn-danger" onClick={() => setSpecs((prev) => prev.filter((_, i) => i !== idx))}>
                            <Trash2 size={12} />
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}

      {isExamAdmin && selectedSlotId && (
        <div className="card" style={{ background: "#fff9db", borderColor: "#ffd43b" }}>
          <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>당일 아침 돌발 결원 긴급 대타 추천</h3>
          <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
            <select className="select" style={{ width: 320 }} value={newSubId} onChange={(e) => setNewSubId(e.target.value)}>
              {subCandidates.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name} ({t.subject} · {t.homeroomClass ? `${t.homeroomClass} 담임` : "비담임 공강"} · 누적 {t.internalExamCount}회)
                </option>
              ))}
            </select>
            <button className="btn btn-danger btn-sm" onClick={handleApplySubstitute}>
              <UserCheck size={14} /> 원클릭 대타 교체 및 긴급 알림 발송
            </button>
            <button className="btn btn-sm" onClick={() => setSelectedSlotId("")}>취소</button>
          </div>
        </div>
      )}

      <div className="card">
        <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
          <Shield size={15} style={{ verticalAlign: "middle" }} /> 교시·과목별 시험감독 배정표
        </h3>
        <table className="table">
          <thead>
            <tr><th>교시 · 시험시간</th><th>학년/과목</th><th>역할</th><th>배정 고사장 / 복도</th><th>감독교사</th><th>비고</th>{isExamAdmin && <th>긴급대타</th>}</tr>
          </thead>
          <tbody>
            {slots.map((s) => (
              <tr key={s.id}>
                <td>
                  <b>{s.period}교시</b>{" "}
                  {s.startTime && (
                    <span className="badge badge-blue" style={{ marginLeft: 4 }}>
                      {s.startTime}~{s.endTime} ({s.durationMinutes || 45}분)
                    </span>
                  )}
                </td>
                <td>{s.grade}학년 <b>{s.subject}</b></td>
                <td>
                  <span className={`badge ${s.slotRole === "CORRIDOR" ? "badge-blue" : s.slotRole === "HONORARY" ? "badge-green" : "badge-gray"}`}>
                    {s.slotRole === "MAIN" ? "정감독" : s.slotRole === "SUB" ? "부감독" : s.slotRole === "CORRIDOR" ? "복도감독" : "학부모명예"}
                  </span>
                </td>
                <td>
                  {s.actualRoomHidden ? <span className="badge badge-amber">{s.roomName}</span> : <b>{s.roomName}</b>}
                </td>
                <td><b>{s.teacherName}</b></td>
                <td style={{ fontSize: 12, color: "#64748b" }}>{s.note || "-"}</td>
                {isExamAdmin && (
                  <td>
                    <button className="btn btn-sm" onClick={() => handleLoadSubCandidates(s.id)}>대타 교체</button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ExamFairnessPanel
        serverUrl={serverUrl}
        user={user}
        fairness={fairness}
        isExamAdmin={isExamAdmin}
        onRefresh={loadData}
        onNotify={onNotify}
      />
    </div>
  );
}
