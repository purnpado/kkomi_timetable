import React, { useEffect, useState } from "react";
import { CheckCircle2, Edit3, FileSpreadsheet, HandHeart, Plus, Settings2, Trash2 } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import ExternalExamRulePanel from "./ExternalExamRulePanel";
import ExternalExamConfigModal from "./ExternalExamConfigModal";

// ExternalExamView는 대외 시험감독 공고 관리, 학교 기본 내규 프리셋, 가중치/별도 트랙 차출 조합기 및 파견 확정을 제공합니다.
// 실무 요구사항에 따라 계좌번호/은행명/수당 정산 기능은 포함하지 않습니다.
export default function ExternalExamView({ serverUrl, user, onNotify, onRefresh }) {
  const [notices, setNotices] = useState([]);
  const [schoolConfig, setSchoolConfig] = useState(null);
  const [configOpen, setConfigOpen] = useState(false);
  const [editNotice, setEditNotice] = useState(null);
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [activeId, setActiveId] = useState("");
  const [candidates, setCandidates] = useState([]);
  const [selectedNotice, setSelectedNotice] = useState(null);
  const [checkedIds, setCheckedIds] = useState([]);
  const [newTitle, setNewTitle] = useState("");
  const [newCategory, setNewCategory] = useState("검정고시");
  const [newDate, setNewDate] = useState("2026-11-26");
  const [newRequired, setNewRequired] = useState(4);

  const isExtAdmin = hasRole(user, "EXTERNAL_EXAM_ADMIN");

  const loadNotices = async (pickId) => {
    try {
      const [list, cfg] = await Promise.all([
        apiFetch(serverUrl, "/api/external-exam/notices", user.id),
        apiFetch(serverUrl, "/api/external-exam/config", user.id).catch(() => null),
      ]);
      setNotices(list || []);
      if (cfg) {
        setSchoolConfig(cfg);
        if (cfg.categories?.length > 0 && !cfg.categories.some((c) => c.name === newCategory)) {
          setNewCategory(cfg.categories[0].name);
        }
      }
      const targetId = pickId || activeId || list?.[0]?.id;
      if (targetId && (list || []).some((n) => n.id === targetId)) {
        setActiveId(targetId);
        await loadCandidates(targetId);
      } else if (list?.[0]?.id) {
        setActiveId(list[0].id);
        await loadCandidates(list[0].id);
      } else {
        setActiveId("");
        setSelectedNotice(null);
        setCandidates([]);
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

  const updateRules = async (ruleOpts) => {
    try {
      await apiFetch(serverUrl, "/api/external-exam/rules", user.id, {
        method: "POST",
        body: JSON.stringify({ noticeId: activeId, ...ruleOpts }),
      });
      await loadCandidates(activeId);
      onNotify("차출 순번 재산정 완료", "학교 내규 우선순위·가중치·유예 기준이 즉시 반영되었습니다.");
    } catch (err) {
      onNotify("규칙 변경 실패", err.message, true);
    }
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
    try {
      const created = await apiFetch(serverUrl, "/api/external-exam/notices", user.id, {
        method: "POST",
        body: JSON.stringify({
          title: newTitle,
          examCategory: newCategory,
          examDate: newDate,
          location: "관내 시험장",
          requiredCount: Number(newRequired) || 4,
        }),
      });
      setNewTitle("");
      loadNotices(created?.id);
    } catch (err) {
      onNotify("공고 등록 실패", err.message, true);
    }
  };

  const handleDeleteNotice = async (notice) => {
    try {
      await apiFetch(serverUrl, `/api/external-exam/notices?id=${encodeURIComponent(notice.id)}`, user.id, { method: "DELETE" });
      onNotify("외부 시험 공고 삭제", `${notice.title} 공고가 삭제되었습니다.`);
      loadNotices();
      onRefresh?.();
    } catch (err) {
      onNotify("삭제 실패", err.message, true);
    }
  };

  const categories = schoolConfig?.categories || [{ name: "검정고시" }, { name: "수능감독" }, { name: "임용고사" }];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 17 }}>대외 시험감독 지원 및 다단계 공정 차출</h2>
            <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 3 }}>
              종목 가중치·별도 트랙·전입 유예·종목 제외·초과 선발 및 동점 기준 지원 (계좌·수당 입력 불필요)
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {isExtAdmin && (
              <button type="button" className="btn btn-sm" onClick={() => setConfigOpen(true)}>
                <Settings2 size={14} /> 학교 내규 · 종목 설정
              </button>
            )}
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
        </div>

        <div style={{ display: "flex", gap: 8, marginTop: 12, flexWrap: "wrap", alignItems: "center" }}>
          {notices.map((n) => (
            <div key={n.id} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
              <button
                className={`btn btn-sm ${n.id === activeId ? "btn-primary" : ""}`}
                onClick={() => { setActiveId(n.id); loadCandidates(n.id); }}
              >
                [{n.examCategory || "기타"}] {n.title} ({n.examDate}) {n.isConfirmed ? "✅확정" : ""}
              </button>
              {isExtAdmin && (
                <>
                  <button type="button" className="btn btn-sm" title="공고 수정" onClick={() => setEditNotice(n)}><Edit3 size={12} /></button>
                  <button type="button" className="btn btn-sm btn-danger" title="공고 삭제" onClick={() => setDeleteTarget(n)}><Trash2 size={12} /></button>
                </>
              )}
            </div>
          ))}
        </div>

        {isExtAdmin && (
          <form onSubmit={handleCreateNotice} style={{ display: "flex", gap: 8, marginTop: 12, flexWrap: "wrap" }}>
            <select className="select" style={{ width: 135 }} value={newCategory} onChange={(e) => setNewCategory(e.target.value)}>
              {categories.map((cat) => <option key={cat.name} value={cat.name}>{cat.name}</option>)}
            </select>
            <input className="input" style={{ width: 240 }} value={newTitle} onChange={(e) => setNewTitle(e.target.value)} placeholder="신규 대외 시험 공고명 (예: 검정고시)" />
            <input className="input" style={{ width: 145 }} type="date" value={newDate} onChange={(e) => setNewDate(e.target.value)} />
            <input className="input" style={{ width: 72 }} type="number" min={1} max={50} value={newRequired} onChange={(e) => setNewRequired(e.target.value)} title="차출 인원" />
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

          {isExtAdmin && <ExternalExamRulePanel notice={selectedNotice} onUpdateRules={updateRules} />}

          <table className="table">
            <thead>
              <tr><th>선택</th><th>순번</th><th>성명 (직위)</th><th>전입일</th><th>누적 · 환산점수</th><th>최근 파견일</th><th>산정 근거 배지</th></tr>
            </thead>
            <tbody>
              {candidates.map((c) => {
                const checked = checkedIds.includes(c.teacher.id);
                return (
                  <tr key={c.teacher.id} style={c.isExempt || c.isGracePeriod ? { opacity: 0.7 } : {}}>
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
                    <td>{c.teacher.externalExamCount}회 ({c.effectiveScore ?? c.teacher.externalExamCount}점)</td>
                    <td>{c.teacher.lastExternalDispatch || "미파견"}</td>
                    <td>
                      <span className={`badge ${c.isVolunteer ? "badge-green" : c.isExempt ? "badge-red" : c.isGracePeriod ? "badge-amber" : "badge-blue"}`}>
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

      <ExternalExamConfigModal
        configOpen={configOpen}
        onConfigOpenChange={setConfigOpen}
        editNotice={editNotice}
        onEditNoticeClose={() => setEditNotice(null)}
        schoolConfig={schoolConfig}
        serverUrl={serverUrl}
        user={user}
        onSaved={(id) => loadNotices(id)}
        onNotify={onNotify}
      />

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
        danger
        title="외부 시험 공고 삭제 확인"
        description={`${deleteTarget?.title} (${deleteTarget?.examDate}) 공고와 미승인 결보강 이관을 삭제하시겠습니까?`}
        confirmLabel="공고 삭제"
        onConfirm={async () => {
          const target = deleteTarget;
          setDeleteTarget(null);
          await handleDeleteNotice(target);
        }}
      />
    </div>
  );
}
