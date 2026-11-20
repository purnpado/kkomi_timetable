import React, { useEffect, useState } from "react";
import { ArrowDown, ArrowUp, CheckCircle2, FileSpreadsheet, HandHeart, Plus } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";

const CRITERIA_LABELS = {
  VOLUNTEER: "1순위: 해당 시험 자발적 지원자",
  LOW_COUNT: "누적 외부감독 횟수 적은 순",
  LATE_TRANSFER: "현임교 전입일 늦은 순 (최근 전입자 우선)",
  OLDEST_DISPATCH: "이전 외부차출 오래된 순 (미파견 최우선)",
  NON_HOMEROOM: "비담임 교사 우선",
  NON_DEPT_HEAD: "비부장 교사 우선",
};

// ExternalExamView는 대외 시험감독(수능·모평·검정고시 등) 지원, 학교별 다단계 차출 조합기 및 파견 확정을 제공합니다.
// 실무 요구사항에 따라 계좌번호/은행명/수당 정산 기능은 포함하지 않습니다.
export default function ExternalExamView({ serverUrl, user, onNotify, onRefresh }) {
  const [notices, setNotices] = useState([]);
  const [activeId, setActiveId] = useState("");
  const [candidates, setCandidates] = useState([]);
  const [selectedNotice, setSelectedNotice] = useState(null);
  const [checkedIds, setCheckedIds] = useState([]);
  const [newTitle, setNewTitle] = useState("");
  const [newDate, setNewDate] = useState("2026-11-26");

  const isExtAdmin = hasRole(user, "EXTERNAL_EXAM_ADMIN");

  const loadNotices = async (pickId) => {
    try {
      const list = await apiFetch(serverUrl, "/api/external-exam/notices", user.id);
      setNotices(list || []);
      const targetId = pickId || activeId || list?.[0]?.id;
      if (targetId) {
        setActiveId(targetId);
        await loadCandidates(targetId);
      }
    } catch (err) {
      onNotify("외부감독 목록 조회 오류", err.message, true);
    }
  };

  const loadCandidates = async (noticeId) => {
    const res = await apiFetch(serverUrl, `/api/external-exam/candidates?noticeId=${noticeId}`, user.id);
    setSelectedNotice(res.notice);
    setCandidates(res.candidates || []);
    if (res.notice?.confirmedIds?.length > 0) {
      setCheckedIds(res.notice.confirmedIds);
    } else {
      const autoPick = (res.candidates || []).filter((c) => !c.isExempt).slice(0, res.notice?.requiredCount || 3).map((c) => c.teacher.id);
      setCheckedIds(autoPick);
    }
  };

  useEffect(() => { loadNotices(); }, [user?.id]);

  const handleVolunteer = async (noticeId) => {
    try {
      await apiFetch(serverUrl, "/api/external-exam/volunteer", user.id, {
        method: "POST",
        body: JSON.stringify({ noticeId }),
      });
      onNotify("외부감독 지원 상태 변경", "자발적 지원 현황이 반영되었습니다.");
      loadNotices(noticeId);
    } catch (err) {
      onNotify("지원 실패", err.message, true);
    }
  };

  const updateRules = async (presetMode, priorityOrder, excludeExempt) => {
    try {
      await apiFetch(serverUrl, "/api/external-exam/rules", user.id, {
        method: "POST",
        body: JSON.stringify({ noticeId: activeId, presetMode, priorityOrder, excludeExempt }),
      });
      await loadCandidates(activeId);
      onNotify("차출 순번 재산정 완료", "학교 내규 우선순위가 즉시 반영되었습니다.");
    } catch (err) {
      onNotify("규칙 변경 실패", err.message, true);
    }
  };

  const moveCriterion = (idx, dir) => {
    if (!selectedNotice) return;
    const order = [...(selectedNotice.priorityOrder || [])];
    const nextIdx = idx + dir;
    if (nextIdx < 1 || nextIdx >= order.length) return; // 인덱스 0(자발적 지원자)은 1순위 고정
    [order[idx], order[nextIdx]] = [order[nextIdx], order[idx]];
    updateRules("CUSTOM", order, selectedNotice.excludeExempt);
  };

  const handleConfirm = async () => {
    try {
      await apiFetch(serverUrl, "/api/external-exam/confirm", user.id, {
        method: "POST",
        body: JSON.stringify({ noticeId: activeId, selectedIds: checkedIds }),
      });
      onNotify("외부감독 파견 명단 확정", "평일 시험일 경우 시간표 담당교사 결보강 대기열로 자동 이관되었습니다.", true);
      await loadNotices(activeId);
      onRefresh();
    } catch (err) {
      onNotify("확정 실패", err.message, true);
    }
  };

  const handleCreateNotice = async (e) => {
    e.preventDefault();
    if (!newTitle.trim()) return;
    await apiFetch(serverUrl, "/api/external-exam/notices", user.id, {
      method: "POST",
      body: JSON.stringify({ title: newTitle, examCategory: "교육청시험", examDate: newDate, location: "관내 시험장", requiredCount: 4, presetMode: "PRESET_MIXED", excludeExempt: true }),
    });
    setNewTitle("");
    loadNotices();
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 17 }}>대외 시험감독 지원 및 다단계 공정 차출</h2>
            <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 3 }}>
              지원자 1순위 우선 선발 + 학교별 차출 내규(전입일·이전차출일·누적횟수) 다단계 조합 지원 (계좌·수당 입력 불필요)
            </div>
          </div>
          {selectedNotice && (
            <button
              type="button"
              className="btn btn-sm"
              onClick={() =>
                downloadAuthenticatedFile(serverUrl, `/api/export/xlsx?kind=external&noticeId=${encodeURIComponent(selectedNotice.id)}`, "외부시험감독_파견명단.xlsx")
                  .catch((err) => onNotify("파견명단 출력 실패", err.message, true))
              }
            >
              <FileSpreadsheet size={14} /> 교육청 파견 명단 (.xlsx)
            </button>
          )}
        </div>

        <div style={{ display: "flex", gap: 8, marginTop: 12, flexWrap: "wrap" }}>
          {notices.map((n) => (
            <button
              key={n.id}
              className={`btn btn-sm ${n.id === activeId ? "btn-primary" : ""}`}
              onClick={() => { setActiveId(n.id); loadCandidates(n.id); }}
            >
              {n.title} ({n.examDate}) {n.isConfirmed ? "✅확정" : ""}
            </button>
          ))}
        </div>

        {isExtAdmin && (
          <form onSubmit={handleCreateNotice} style={{ display: "flex", gap: 8, marginTop: 12, flexWrap: "wrap" }}>
            <input className="input" style={{ width: 260 }} value={newTitle} onChange={(e) => setNewTitle(e.target.value)} placeholder="신규 대외 시험 공고명 (예: 검정고시 감독)" />
            <input className="input" style={{ width: 150 }} type="date" value={newDate} onChange={(e) => setNewDate(e.target.value)} />
            <button type="submit" className="btn btn-sm"><Plus size={14} /> 공고 등록</button>
          </form>
        )}
      </div>

      {selectedNotice && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 12 }}>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <button
                className={`btn ${selectedNotice.volunteerIds?.includes(user.id) ? "btn-green" : "btn-primary"}`}
                onClick={() => handleVolunteer(selectedNotice.id)}
              >
                <HandHeart size={15} />
                {selectedNotice.volunteerIds?.includes(user.id) ? "자발적 지원 완료 (클릭 시 취소)" : "이 시험 자발적 감독 지원하기"}
              </button>
              <span className="badge badge-blue">필요 인원: {selectedNotice.requiredCount}명 · 선택됨: {checkedIds.length}명</span>
            </div>
            {isExtAdmin && (
              <button className="btn btn-green" onClick={handleConfirm}>
                <CheckCircle2 size={15} /> 최종 선택 (파견 명단 확정 및 결보강 자동이관)
              </button>
            )}
          </div>

          {isExtAdmin && (
            <div style={{ padding: 12, background: "#f8fafc", borderRadius: 10, border: "1px solid #e2e8f0", marginBottom: 14 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 8 }}>
                <b style={{ fontSize: 13 }}>우리 학교 차출 우선순위 기준 조합</b>
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                  <button className="btn btn-sm" onClick={() => updateRules("PRESET_MIXED", [], selectedNotice.excludeExempt)}>방식A: 혼합형(누적→전입→오래된순)</button>
                  <button className="btn btn-sm" onClick={() => updateRules("PRESET_COOLDOWN", [], selectedNotice.excludeExempt)}>방식B: 순번/쿨타임형(미파견·오래된순)</button>
                  <button className="btn btn-sm" onClick={() => updateRules("PRESET_RECENT_TRANSFER", [], selectedNotice.excludeExempt)}>방식C: 최근 전입자 우선형</button>
                  <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 4, marginLeft: 8 }}>
                    <input
                      type="checkbox"
                      checked={selectedNotice.excludeExempt}
                      onChange={(e) => updateRules(selectedNotice.presetMode, selectedNotice.priorityOrder, e.target.checked)}
                    />
                    면제 대상자(임산부/수험생자녀/원로) 후순위 제외
                  </label>
                </div>
              </div>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                {(selectedNotice.priorityOrder || []).map((crit, idx) => (
                  <div key={crit} style={{ display: "inline-flex", alignItems: "center", gap: 4, background: "#fff", border: "1px solid #cbd5e1", borderRadius: 8, padding: "4px 8px", fontSize: 12 }}>
                    <b>{idx + 1}.</b> {CRITERIA_LABELS[crit] || crit}
                    {idx > 0 && (
                      <>
                        <button type="button" style={{ border: 0, background: "transparent", cursor: "pointer" }} onClick={() => moveCriterion(idx, -1)}><ArrowUp size={12} /></button>
                        <button type="button" style={{ border: 0, background: "transparent", cursor: "pointer" }} onClick={() => moveCriterion(idx, 1)}><ArrowDown size={12} /></button>
                      </>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          <table className="table">
            <thead>
              <tr><th>선택</th><th>순번</th><th>성명 (직위)</th><th>전입일</th><th>누적 횟수</th><th>최근 파견일</th><th>산정 근거 배지</th></tr>
            </thead>
            <tbody>
              {candidates.map((c) => {
                const checked = checkedIds.includes(c.teacher.id);
                return (
                  <tr key={c.teacher.id} style={c.isExempt ? { opacity: 0.6 } : {}}>
                    <td>
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!isExtAdmin}
                        onChange={(e) => {
                          setCheckedIds((prev) => e.target.checked ? [...prev, c.teacher.id] : prev.filter((id) => id !== c.teacher.id));
                        }}
                      />
                    </td>
                    <td><b>#{c.rank}</b></td>
                    <td>{c.teacher.name} <span style={{ fontSize: 12, color: "#64748b" }}>({c.teacher.position})</span></td>
                    <td>{c.teacher.transferDate}</td>
                    <td>{c.teacher.externalExamCount}회</td>
                    <td>{c.teacher.lastExternalDispatch || "미파견"}</td>
                    <td>
                      <span className={`badge ${c.isVolunteer ? "badge-green" : c.isExempt ? "badge-red" : "badge-blue"}`}>
                        {c.reasonBadge}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
