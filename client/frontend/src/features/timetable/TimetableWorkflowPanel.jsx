import React, { useEffect, useState } from "react";
import { CheckCircle2, Lock, MessageSquarePlus, Plus, Send, ShieldCheck, Sparkles, Trash2 } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";

const PHASES = [
  { key: "COLLECTING", label: "1단계 · 교과별 시수 취합", desc: "교과부장 시수표 입력·제출" },
  { key: "DRAFT_REVIEW", label: "2단계 · 가편성 검토", desc: "교사 피드백 수집·조정" },
  { key: "LOCKED", label: "3단계 · 최종 확정 잠금", desc: "정규 수정 잠금 · 일회성만 허용" },
];
const DAYS = ["", "월", "화", "수", "목", "금"];

// TimetableWorkflowPanel은 창체 동시/고정 편성, 교과별 시수 취합, 가편성 검토 피드백, 최종 확정 잠금을 관리합니다.
export default function TimetableWorkflowPanel({ serverUrl, user, onRefresh, onNotify }) {
  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");
  const [workflow, setWorkflow] = useState({ phase: "COLLECTING", note: "" });
  const [submissions, setSubmissions] = useState([]);
  const [feedbacks, setFeedbacks] = useState([]);
  const [caSlots, setCaSlots] = useState([]);

  const [deptName, setDeptName] = useState(user?.subject || "국어과");
  const [gradeScope, setGradeScope] = useState("전학년");
  const [subNote, setSubNote] = useState("");
  const [fbTarget, setFbTarget] = useState("");
  const [fbCat, setFbCat] = useState("기피교시");
  const [fbMsg, setFbMsg] = useState("");
  const [caTitle, setCaTitle] = useState("창체(자치·적응)");
  const [caGrade, setCaGrade] = useState(0);
  const [caDay, setCaDay] = useState(5);
  const [caPeriod, setCaPeriod] = useState(6);

  const loadAll = async () => {
    try {
      const [wf, subs, fbs, loadData] = await Promise.all([
        apiFetch(serverUrl, "/api/timetable/workflow", user.id),
        apiFetch(serverUrl, "/api/timetable/dept-submissions", user.id),
        apiFetch(serverUrl, "/api/timetable/feedbacks", user.id),
        apiFetch(serverUrl, "/api/timetable/loads", user.id),
      ]);
      setWorkflow(wf || { phase: "COLLECTING" });
      setSubmissions(subs || []);
      setFeedbacks(fbs || []);
      setCaSlots(loadData?.config?.creativeActivities || []);
    } catch (err) {
      onNotify?.("워크플로우 조회 실패", err.message, true);
    }
  };

  useEffect(() => { loadAll(); }, [serverUrl, user?.id]);

  const handlePhaseChange = async (nextPhase) => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/workflow/phase", user.id, {
        method: "POST",
        body: JSON.stringify({ phase: nextPhase, note: `${user.name} 단계 전환` }),
      });
      setWorkflow(res);
      onNotify?.("편성 단계 전환 완료", `${PHASES.find((p) => p.key === nextPhase)?.label || nextPhase} 상태로 변경되었습니다.`);
      onRefresh?.();
    } catch (err) {
      onNotify?.("단계 전환 실패", err.message, true);
    }
  };

  const handleApplyCA = async (nextSlots) => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/creative-activities", user.id, {
        method: "POST",
        body: JSON.stringify({ slots: nextSlots }),
      });
      setCaSlots(nextSlots);
      onNotify?.("창체 동시·고정 편성 완료", `${res.appliedCount}개 학급 교시에 창체 고정 수업이 반영되었습니다.`);
      onRefresh?.();
    } catch (err) {
      onNotify?.("창체 편성 실패", err.message, true);
    }
  };

  const handleDeptSubmit = async (e) => {
    e.preventDefault();
    try {
      await apiFetch(serverUrl, "/api/timetable/dept-submissions", user.id, {
        method: "POST",
        body: JSON.stringify({ department: deptName, gradeScope, note: subNote }),
      });
      setSubNote("");
      onNotify?.("교과 시수표 제출 완료", `${deptName} 교과 시수표 취합 제출이 등록되었습니다.`);
      loadAll();
    } catch (err) {
      onNotify?.("제출 실패", err.message, true);
    }
  };

  const handleReviewSub = async (id, status) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/dept-submissions/review", user.id, {
        method: "POST",
        body: JSON.stringify({ id, status, reviewNote: status === "APPROVED" ? "취합 확인" : "시수 보완 요망" }),
      });
      loadAll();
    } catch (err) {
      onNotify?.("검토 처리 실패", err.message, true);
    }
  };

  const handleFeedbackSubmit = async (e) => {
    e.preventDefault();
    try {
      await apiFetch(serverUrl, "/api/timetable/feedbacks", user.id, {
        method: "POST",
        body: JSON.stringify({ targetRef: fbTarget, category: fbCat, message: fbMsg }),
      });
      setFbMsg("");
      onNotify?.("가편성 피드백 등록", "일과계 검토 목록에 의견이 등록되었습니다.");
      loadAll();
    } catch (err) {
      onNotify?.("피드백 등록 실패", err.message, true);
    }
  };

  const handleResolveFb = async (id) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/feedbacks/resolve", user.id, {
        method: "POST",
        body: JSON.stringify({ id, resolution: "가편성 시간표 반영 완료" }),
      });
      loadAll();
    } catch (err) {
      onNotify?.("완료 처리 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <h3 style={{ margin: 0, fontSize: 16, display: "flex", alignItems: "center", gap: 6 }}>
              <ShieldCheck size={17} color="#3b5bdb" /> 시간표 편성 3단계 상태 관리 & 최종 확정(Lock)
            </h3>
            <div style={{ fontSize: 12, color: "#64748b", marginTop: 4 }}>
              최종 확정(LOCKED) 상태에서는 실수로 인한 정규 시간표 영구 수정을 차단하며, 일회성(특정 주차·일자) 결보강·교환만 허용됩니다.
            </div>
          </div>
          {workflow.phase === "LOCKED" && (
            <span className="badge badge-red"><Lock size={12} /> 정규 시간표 잠금 중 ({workflow.lockedBy})</span>
          )}
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 10, marginTop: 12 }}>
          {PHASES.map((p) => {
            const active = workflow.phase === p.key;
            return (
              <div key={p.key} style={{ padding: 12, borderRadius: 10, border: active ? "2px solid #3b5bdb" : "1px solid #cbd5e1", background: active ? "#edf2ff" : "#fff" }}>
                <div style={{ fontWeight: 700, fontSize: 13.5 }}>{p.label}</div>
                <div style={{ fontSize: 11.5, color: "#475569", margin: "4px 0 8px" }}>{p.desc}</div>
                {isAdmin && (
                  <button type="button" className={`btn btn-sm ${active ? "btn-primary" : ""}`} disabled={active} onClick={() => handlePhaseChange(p.key)}>
                    {active ? "현재 단계" : "이 단계로 전환"}
                  </button>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {isAdmin && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>
              <Sparkles size={15} style={{ verticalAlign: "middle" }} /> 창체(자치·동아리 등) 동시/고정 교시 편성
            </h3>
            <button type="button" className="btn btn-sm btn-green" onClick={() => handleApplyCA(caSlots)}>
              <CheckCircle2 size={13} /> 정규 시간표에 창체 고정 반영
            </button>
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 10 }}>
            <input className="input" style={{ width: 170 }} value={caTitle} onChange={(e) => setCaTitle(e.target.value)} placeholder="활동명 (예: 창체(동아리))" />
            <select className="select" style={{ width: 110 }} value={caGrade} onChange={(e) => setCaGrade(Number(e.target.value))}>
              <option value={0}>전학년 동시</option>
              <option value={1}>1학년</option>
              <option value={2}>2학년</option>
              <option value={3}>3학년</option>
            </select>
            <select className="select" style={{ width: 95 }} value={caDay} onChange={(e) => setCaDay(Number(e.target.value))}>
              {[1, 2, 3, 4, 5].map((d) => <option key={d} value={d}>{DAYS[d]}요일</option>)}
            </select>
            <select className="select" style={{ width: 90 }} value={caPeriod} onChange={(e) => setCaPeriod(Number(e.target.value))}>
              {[1, 2, 3, 4, 5, 6, 7].map((p) => <option key={p} value={p}>{p}교시</option>)}
            </select>
            <button
              type="button"
              className="btn btn-sm btn-primary"
              onClick={() => {
                const slot = { id: `ca-${Date.now()}`, title: caTitle.trim() || "창체", grade: caGrade, dayOfWeek: caDay, period: caPeriod, locked: true, simultaneous: true };
                setCaSlots([...caSlots, slot]);
              }}
            >
              <Plus size={13} /> 슬롯 추가
            </button>
          </div>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            {caSlots.map((sl) => (
              <span key={sl.id} className="badge badge-blue" style={{ padding: "5px 10px", gap: 6 }}>
                <b>{sl.title}</b> ({sl.grade === 0 ? "전학년" : `${sl.grade}학년`} · {DAYS[sl.dayOfWeek]} {sl.period}교시 · 고정)
                <button type="button" style={{ border: "none", background: "none", cursor: "pointer" }} onClick={() => setCaSlots(caSlots.filter((x) => x.id !== sl.id))}>
                  <Trash2 size={11} color="#e03131" />
                </button>
              </span>
            ))}
          </div>
        </div>
      )}

      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>교과부장 시수표 제출 · 취합 현황</h3>
          <form onSubmit={handleDeptSubmit} style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 10 }}>
            <input className="input" style={{ width: 105 }} value={deptName} onChange={(e) => setDeptName(e.target.value)} placeholder="교과명" required />
            <input className="input" style={{ width: 85 }} value={gradeScope} onChange={(e) => setGradeScope(e.target.value)} placeholder="대상학년" />
            <input className="input" style={{ flex: 1, minWidth: 120 }} value={subNote} onChange={(e) => setSubNote(e.target.value)} placeholder="특이사항(연강·전용실 등)" />
            <button type="submit" className="btn btn-sm btn-primary"><Send size={12} /> 제출</button>
          </form>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {submissions.map((s) => (
              <div key={s.id} style={{ padding: "8px 10px", background: "#f8fafc", borderRadius: 8, fontSize: 12.5, display: "flex", justifyContent: "space-between", alignItems: "center", gap: 6 }}>
                <div>
                  <b>{s.department}</b> ({s.gradeScope} · {s.submitter}) · {s.totalHours}시수
                  <div style={{ fontSize: 11, color: "#64748b" }}>{s.note || "메모 없음"}</div>
                </div>
                <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
                  <span className={`badge ${s.status === "APPROVED" ? "badge-green" : s.status === "REVISION_REQUESTED" ? "badge-red" : "badge-amber"}`}>{s.status}</span>
                  {isAdmin && s.status === "SUBMITTED" && (
                    <>
                      <button type="button" className="btn btn-sm btn-green" onClick={() => handleReviewSub(s.id, "APPROVED")}>확인</button>
                      <button type="button" className="btn btn-sm btn-danger" onClick={() => handleReviewSub(s.id, "REVISION_REQUESTED")}>보완</button>
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
            <MessageSquarePlus size={15} style={{ verticalAlign: "middle" }} /> 가편성 시간표 검토 피드백
          </h3>
          <form onSubmit={handleFeedbackSubmit} style={{ display: "flex", flexDirection: "column", gap: 6, marginBottom: 10 }}>
            <div style={{ display: "flex", gap: 6 }}>
              <input className="input" style={{ width: 130 }} value={fbTarget} onChange={(e) => setFbTarget(e.target.value)} placeholder="대상 (예: 2-3 수4)" />
              <select className="select" style={{ width: 110 }} value={fbCat} onChange={(e) => setFbCat(e.target.value)}>
                <option value="기피교시">기피교시</option>
                <option value="연강">연강 조정</option>
                <option value="특별실">특별실</option>
                <option value="기타">기타</option>
              </select>
              <input className="input" style={{ flex: 1 }} value={fbMsg} onChange={(e) => setFbMsg(e.target.value)} placeholder="조정 요청 내용" required />
              <button type="submit" className="btn btn-sm btn-primary">등록</button>
            </div>
          </form>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {feedbacks.map((f) => (
              <div key={f.id} style={{ padding: "8px 10px", background: "#f8fafc", borderRadius: 8, fontSize: 12.5, display: "flex", justifyContent: "space-between", alignItems: "center", gap: 6 }}>
                <div>
                  <span className="badge badge-gray" style={{ marginRight: 5 }}>{f.category}</span>
                  <b>{f.authorName}</b> ({f.targetRef || "전체"}): {f.message}
                </div>
                <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
                  <span className={`badge ${f.status === "RESOLVED" ? "badge-green" : "badge-amber"}`}>{f.status === "RESOLVED" ? "반영완료" : "검토중"}</span>
                  {isAdmin && f.status !== "RESOLVED" && (
                    <button type="button" className="btn btn-sm btn-green" onClick={() => handleResolveFb(f.id)}>완료</button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
