import React, { useEffect, useMemo, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, CheckCircle2, Compass, RefreshCw, Sparkles, X } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";

const DAYS = ["", "월", "화", "수", "목", "금"];
const TYPE_LABELS = {
  ALL: "전체",
  MOVE: "빈교시 이동",
  SWAP: "1:1 맞교환",
  TRIANGLE: "삼각 연쇄",
  QUAD: "사각 연쇄",
  CROSS_WEEK: "다주간 교환",
  BLOCK_MOVE: "블록 통째이동",
};

// SwapExplorerModal은 선택한 수업의 모든 교체/이동 경우의 수를 전수 탐색해 품질 점수순으로 정렬·적용합니다.
export default function SwapExplorerModal({
  open,
  onOpenChange,
  serverUrl,
  user,
  cells,
  teachersMap,
  weekStart,
  defaultScope = "DATE_ONLY",
  initialSourceId = "",
  onRefresh,
  onNotify,
}) {
  const [sourceCellId, setSourceCellId] = useState(initialSourceId || cells?.[0]?.id || "");
  const [scope, setScope] = useState(defaultScope);
  const [includeCrossWk, setIncludeCrossWk] = useState(true);
  const [includeBlocks, setIncludeBlocks] = useState(true);
  const [instantApply, setInstantApply] = useState(false);
  const [agreementNote, setAgreementNote] = useState("");
  const [typeFilter, setTypeFilter] = useState("ALL");
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);

  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");

  useEffect(() => {
    if (initialSourceId) setSourceCellId(initialSourceId);
    else if (cells?.length > 0 && !cells.some((c) => c.id === sourceCellId)) setSourceCellId(cells[0].id);
  }, [initialSourceId, cells]);

  useEffect(() => {
    setScope(defaultScope);
  }, [defaultScope]);

  const runExplore = async (targetId = sourceCellId, targetScope = scope) => {
    if (!targetId) return;
    setLoading(true);
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/explore-swaps", user.id, {
        method: "POST",
        body: JSON.stringify({
          sourceCellId: targetId,
          scope: targetScope,
          weekStart: targetScope === "DATE_ONLY" ? weekStart : "",
          includeCrossWk: Boolean(includeCrossWk && targetScope === "DATE_ONLY"),
          maxCrossWeeks: 4,
          includeBlocks,
          maxResults: 100,
        }),
      });
      setResult(res);
    } catch (err) {
      onNotify?.("전수 탐색 실패", err.message, true);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open && sourceCellId) {
      runExplore(sourceCellId, scope);
    }
  }, [open, sourceCellId, scope, includeCrossWk, includeBlocks]);

  const filteredOptions = useMemo(() => {
    const list = result?.options || [];
    if (typeFilter === "ALL") return list;
    return list.filter((o) => o.optionType === typeFilter);
  }, [result, typeFilter]);

  const handleApplyOption = async (opt) => {
    const srcCell = result?.sourceCell || (cells || []).find((c) => c.id === sourceCellId) || {};
    const firstStep = opt.steps?.[0] || {};
    const lastStep = opt.steps?.[opt.steps.length - 1] || {};
    const reqType = opt.optionType === "CROSS_WEEK" ? "CROSS_WEEK_SWAP" : opt.optionType;
    const isDateOnly = scope === "DATE_ONLY" || opt.optionType === "CROSS_WEEK";
    const srcDate = opt.sourceDate || srcCell.lessonDate || weekStart || "";
    const tgtDate = opt.targetDate || srcDate;

    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: reqType,
          scope: isDateOnly ? "DATE_ONLY" : "",
          sourceDate: isDateOnly ? srcDate : "",
          targetDate: isDateOnly ? tgtDate : "",
          requesterId: user.id,
          requesterName: teachersMap[srcCell.teacherId]?.name || user.name,
          partnerId: opt.partnerId || lastStep.teacherId || srcCell.teacherId,
          partnerName: opt.partnerName || lastStep.teacherName || "본인/연쇄이동",
          partnerAgreed: Boolean(agreementNote.trim()) || !opt.partnerId || opt.partnerId === srcCell.teacherId,
          agreementNote: agreementNote.trim(),
          autoApprove: Boolean(isAdmin && instantApply),
          sourceCell: srcCell,
          targetCell: {
            ...srcCell,
            id: "",
            dayOfWeek: firstStep.toDay || srcCell.dayOfWeek,
            period: firstStep.toPeriod || srcCell.period,
          },
          chainSteps: opt.steps || [],
          reason: `[전수탐색:${TYPE_LABELS[opt.optionType] || opt.optionType}] ${opt.title}`,
        }),
      });
      onNotify?.(
        isAdmin && instantApply ? "시간표 즉시 반영 완료" : "교체 경로 대기열 등록 완료",
        isAdmin && instantApply
          ? `${opt.title} (${isDateOnly ? "일회성" : "정규"})이 즉시 반영되었습니다.`
          : `${opt.title} 경로가 승인 대기열에 등록되었습니다.`
      );
      onOpenChange(false);
      onRefresh?.();
    } catch (err) {
      onNotify?.("경로 신청/반영 실패", err.message, true);
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="radix-dialog-overlay" />
        <Dialog.Content className="radix-dialog-content" style={{ maxWidth: 880, width: "95vw" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <Dialog.Title style={{ margin: 0, fontSize: 17, fontWeight: 700, display: "flex", alignItems: "center", gap: 6 }}>
              <Compass size={18} color="#3b5bdb" /> 수업 교체 모든 경우의 수 전수 탐색 & 품질 추천
            </Dialog.Title>
            <Dialog.Close asChild>
              <button type="button" className="btn btn-sm"><X size={14} /></button>
            </Dialog.Close>
          </div>

          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", padding: 10, background: "#f8fafc", borderRadius: 10, marginBottom: 12 }}>
            <select className="select" style={{ flex: 1, minWidth: 230 }} value={sourceCellId} onChange={(e) => setSourceCellId(e.target.value)}>
              {(cells || []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.className}반 {DAYS[c.dayOfWeek]} {c.period}교시 · {c.subject} ({teachersMap[c.teacherId]?.name || c.teacherId})
                  {c.electiveGroup ? ` [선택:${c.electiveGroup}]` : ""}
                </option>
              ))}
            </select>
            <select className="select" style={{ width: 175 }} value={scope} onChange={(e) => setScope(e.target.value)}>
              <option value="DATE_ONLY">일회성 (선택 주차만)</option>
              <option value="">정규 시간표 (매주 영구)</option>
            </select>
            <label style={{ fontSize: 12, display: "inline-flex", alignItems: "center", gap: 4 }}>
              <input type="checkbox" checked={includeCrossWk} disabled={scope !== "DATE_ONLY"} onChange={(e) => setIncludeCrossWk(e.target.checked)} />
              다주간(+1~4주)
            </label>
            <label style={{ fontSize: 12, display: "inline-flex", alignItems: "center", gap: 4 }}>
              <input type="checkbox" checked={includeBlocks} onChange={(e) => setIncludeBlocks(e.target.checked)} />
              블록·연강 통째이동
            </label>
            <button type="button" className="btn btn-sm btn-primary" onClick={() => runExplore()} disabled={loading}>
              <RefreshCw size={13} /> {loading ? "탐색 중..." : "재탐색"}
            </button>
          </div>

          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginBottom: 10 }}>
            <input
              className="input"
              style={{ flex: 1, minWidth: 220 }}
              value={agreementNote}
              onChange={(e) => setAgreementNote(e.target.value)}
              placeholder="상대 교사 사전 합의 메모 (선택, 예: 홍길동 선생님 구두 합의 완료)"
            />
            {isAdmin && (
              <label style={{ fontSize: 12.5, fontWeight: 700, color: "#087f5b", display: "inline-flex", alignItems: "center", gap: 5 }}>
                <input type="checkbox" checked={instantApply} onChange={(e) => setInstantApply(e.target.checked)} />
                일과계 즉시 반영 (대기열 건너뛰기)
              </label>
            )}
          </div>

          <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 12 }}>
            {Object.entries(TYPE_LABELS).map(([k, label]) => {
              const cnt = k === "ALL" ? result?.totalCount || 0 : result?.counts?.[k] || 0;
              return (
                <button
                  key={k}
                  type="button"
                  className={`btn btn-sm ${typeFilter === k ? "btn-primary" : ""}`}
                  onClick={() => setTypeFilter(k)}
                >
                  {label} ({cnt})
                </button>
              );
            })}
          </div>

          <div style={{ maxHeight: 380, overflowY: "auto", display: "flex", flexDirection: "column", gap: 8 }}>
            {loading ? (
              <div style={{ padding: 24, textAlign: "center", color: "#64748b" }}>가능한 모든 이동·맞교환·연쇄 경로를 계산하는 중입니다...</div>
            ) : filteredOptions.length === 0 ? (
              <div style={{ padding: 24, textAlign: "center", color: "#64748b" }}>조건에 맞는 교체 가능 경로가 없습니다. (잠금/블록 상태 또는 제약 조건 확인)</div>
            ) : (
              filteredOptions.map((opt) => (
                <div key={opt.optionId} style={{ border: "1px solid #cbd5e1", borderRadius: 10, padding: "10px 12px", background: "#fff" }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                      <span className={`badge ${opt.score >= 90 ? "badge-green" : opt.score >= 75 ? "badge-blue" : "badge-amber"}`}>
                        <Sparkles size={11} /> {opt.score}점
                      </span>
                      <span className="badge badge-gray">{TYPE_LABELS[opt.optionType] || opt.optionType}</span>
                      <b style={{ fontSize: 13.5 }}>{opt.title}</b>
                      {opt.softWarnings?.length > 0 && <span className="badge badge-amber">기피교시 주의</span>}
                    </div>
                    <button type="button" className="btn btn-sm btn-green" onClick={() => handleApplyOption(opt)}>
                      <CheckCircle2 size={13} /> {isAdmin && instantApply ? "즉시 반영" : "이 경로 신청"}
                    </button>
                  </div>
                  <div style={{ fontSize: 12, color: "#475569", marginTop: 4 }}>{opt.description}</div>
                  {opt.steps?.length > 0 && (
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 6 }}>
                      {opt.steps.map((st) => (
                        <span key={`${opt.optionId}-${st.step}-${st.cellId}`} style={{ fontSize: 11, padding: "2px 8px", background: "#f1f5f9", borderRadius: 6 }}>
                          {st.step}. {st.className} {st.subject} ({st.teacherName}): {DAYS[st.fromDay]}{st.fromPeriod}
                          <ArrowRight size={10} style={{ verticalAlign: "middle", margin: "0 2px" }} />
                          {DAYS[st.toDay]}{st.toPeriod}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
