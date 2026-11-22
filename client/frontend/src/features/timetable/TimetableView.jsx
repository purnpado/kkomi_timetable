import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { AlertTriangle, BookOpenCheck, CheckSquare, ChevronLeft, ChevronRight, ClipboardList, Compass, FileSpreadsheet, FileText, History, Lock, Redo2, Sliders, TableProperties, Undo2, Users, Workflow, Wrench } from "lucide-react";
import TimetableCanvas from "../../components/canvas/TimetableCanvas";
import { apiFetch, hasRegularClasses, hasRole, isFacultyTeacher, isSubstituteExcluded } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ActionMenu from "../../components/ui/ActionMenu";
import AppTooltip from "../../components/ui/AppTooltip";
import BaseTimetableBuilder from "./BaseTimetableBuilder";
import CurriculumLoadManager from "./CurriculumLoadManager";
import ChainSwapPreview from "./ChainSwapPreview";
import ConstraintManager from "./ConstraintManager";
import ScenarioComparePanel from "./ScenarioComparePanel";
import MultiWeekAbsenceView, { addDaysISO, getMondayISO } from "./MultiWeekAbsenceView";
import TeacherManagerCard from "../admin-remote/TeacherManagerCard";
import SwapExplorerModal from "./SwapExplorerModal";
import TimetableRequestAndNEISPanel from "./TimetableRequestAndNEISPanel";
import TimetableWorkflowPanel from "./TimetableWorkflowPanel";
import SemesterLedgerPanel from "./SemesterLedgerPanel";

// TimetableView는 60fps HTML5 Canvas 다주간 시간표 조정, 주중 미출근·주차간 교환,
// 교사별 시수표/교과교실·공강제약·시나리오 비교 및 실행취소(Undo/Redo)·나이스(NEIS) 입력 도우미를 제공합니다.
export default function TimetableView({ serverUrl, user, cells, teachers, requests, bellSchedule, onRefresh, onOpenApproval, onNotify }) {
  const [subTab, setSubTab] = useState("board");
  const [selectedWeekStart, setSelectedWeekStart] = useState(() => getMondayISO());
  const [changeScope, setChangeScope] = useState("DATE_ONLY");
  const [moveWholeBlock, setMoveWholeBlock] = useState(true);
  const [explorerOpen, setExplorerOpen] = useState(false);
  const [selectedCellId, setSelectedCellId] = useState("");
  const [workflowState, setWorkflowState] = useState({ phase: "COLLECTING" });
  const [weekCells, setWeekCells] = useState(null);
  const [filterClass, setFilterClass] = useState("ALL");
  const [activeChain, setActiveChain] = useState(null);
  const [historyStatus, setHistoryStatus] = useState({ canUndo: false, canRedo: false, undoCount: 0, redoCount: 0, lastUndo: "", lastRedo: "" });
  const [reason, setReason] = useState("학년 협의 및 수업 교환");
  const [reqType, setReqType] = useState("SWAP");
  const [sourceId, setSourceId] = useState(cells[0]?.id || "");
  const [partnerId, setPartnerId] = useState("");

  useEffect(() => {
    let alive = true;
    apiFetch(serverUrl, "/api/timetable/workflow", user.id)
      .then((wf) => { if (alive && wf) setWorkflowState(wf); })
      .catch(() => {});
    return () => { alive = false; };
  }, [serverUrl, user?.id, cells]);

  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const partnerTeachers = useMemo(() => {
    const list =
      reqType === "SUBSTITUTE"
        ? facultyTeachers.filter((t) => !isSubstituteExcluded(t))
        : facultyTeachers.filter(hasRegularClasses);
    return list.length > 0 ? list : facultyTeachers;
  }, [facultyTeachers, reqType]);

  useEffect(() => {
    if (partnerTeachers.length === 0) {
      if (partnerId) setPartnerId("");
      return;
    }
    if (!partnerTeachers.some((t) => t.id === partnerId)) {
      setPartnerId(partnerTeachers[0].id);
    }
  }, [partnerTeachers, partnerId]);

  useEffect(() => {
    if (cells.length > 0 && !cells.some((c) => c.id === sourceId)) {
      setSourceId(cells[0].id);
    }
  }, [cells, sourceId]);

  useEffect(() => {
    let alive = true;
    if (!selectedWeekStart) {
      setWeekCells(null);
      return;
    }
    apiFetch(serverUrl, `/api/timetable/cells?weekStart=${encodeURIComponent(selectedWeekStart)}`, user.id)
      .then((res) => { if (alive) setWeekCells(res || []); })
      .catch(() => { if (alive) setWeekCells(null); });
    return () => { alive = false; };
  }, [serverUrl, user?.id, selectedWeekStart, cells]);

  const activeCells = selectedWeekStart && weekCells ? weekCells : cells;

  const teachersMap = useMemo(() => {
    const m = {};
    (teachers || []).forEach((t) => { m[t.id] = t; });
    return m;
  }, [teachers]);

  const classList = useMemo(() => {
    const set = new Set((cells || []).map((c) => c.className));
    return ["ALL", ...Array.from(set).sort()];
  }, [cells]);

  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");
  const approvedList = (requests || []).filter((r) => r.status === "APPROVED");

  const onNotifyRef = useRef(onNotify);
  onNotifyRef.current = onNotify;
  const onRefreshRef = useRef(onRefresh);
  onRefreshRef.current = onRefresh;

  const loadHistory = useCallback(async () => {
    if (!isAdmin) return;
    try {
      const st = await apiFetch(serverUrl, "/api/timetable/history", user.id);
      setHistoryStatus(st || { canUndo: false, canRedo: false, undoCount: 0, redoCount: 0, lastUndo: "", lastRedo: "" });
    } catch { /* ignore */ }
  }, [isAdmin, serverUrl, user?.id]);

  useEffect(() => { loadHistory(); }, [loadHistory, cells]);

  const handleUndo = useCallback(async () => {
    if (!isAdmin || !historyStatus.canUndo) return;
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/undo", user.id, { method: "POST" });
      if (res?.history) setHistoryStatus(res.history);
      onNotifyRef.current?.("실행취소 (Undo) 완료", "시간표가 직전 상태로 복원되었습니다.");
      onRefreshRef.current?.();
    } catch (err) {
      onNotifyRef.current?.("실행취소 실패", err.message, true);
    }
  }, [isAdmin, historyStatus.canUndo, serverUrl, user?.id]);

  const handleRedo = useCallback(async () => {
    if (!isAdmin || !historyStatus.canRedo) return;
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/redo", user.id, { method: "POST" });
      if (res?.history) setHistoryStatus(res.history);
      onNotifyRef.current?.("다시실행 (Redo) 완료", "실행취소했던 시간표 변경이 다시 적용되었습니다.");
      onRefreshRef.current?.();
    } catch (err) {
      onNotifyRef.current?.("다시실행 실패", err.message, true);
    }
  }, [isAdmin, historyStatus.canRedo, serverUrl, user?.id]);

  useEffect(() => {
    if (!isAdmin) return undefined;
    const onKeyDown = (e) => {
      const el = e.target;
      if (el?.tagName === "INPUT" || el?.tagName === "TEXTAREA" || el?.tagName === "SELECT" || el?.isContentEditable) return;
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      const key = e.key.toLowerCase();
      if (key === "z" && !e.shiftKey) {
        e.preventDefault();
        handleUndo();
      } else if ((key === "z" && e.shiftKey) || key === "y") {
        e.preventDefault();
        handleRedo();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isAdmin, handleUndo, handleRedo]);

  const handleEvaluateDrop = async (sourceCell, targetDay, targetPeriod) => {
    try {
      return await apiFetch(serverUrl, "/api/timetable/evaluate", user.id, {
        method: "POST",
        body: JSON.stringify({
          sourceCellId: sourceCell.id,
          targetDay,
          targetPeriod,
          targetClass: sourceCell.className,
          targetTeacher: sourceCell.teacherId,
          targetDate: changeScope === "DATE_ONLY" ? selectedWeekStart || getMondayISO() : "",
          moveWholeBlock,
        }),
      });
    } catch {
      return { signal: "RED", reason: "서버 판정 연결 오류" };
    }
  };

  const handleRequestFromCanvas = async (srcCell, tgtCell, targetDay, targetPeriod, evalRes) => {
    if (evalRes.mode === "TRIANGLE" || evalRes.mode === "QUAD" || evalRes.mode === "BLOCK_MOVE") {
      setActiveChain({ evalResult: evalRes });
      onNotify(`${evalRes.chainSteps?.length || 3}단계 변경 경로 확인`, "아래 연쇄 교환 미리보기에서 단계별 변경 과정을 확인한 뒤 신청하세요.");
      return;
    }
    const isDateOnly = changeScope === "DATE_ONLY";
    const effDate = isDateOnly ? srcCell.lessonDate || selectedWeekStart || getMondayISO() : "";
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: evalRes.mode || (tgtCell ? "SWAP" : "MOVE"),
          scope: changeScope,
          sourceDate: effDate,
          targetDate: effDate,
          requesterId: user.id,
          requesterName: teachersMap[srcCell.teacherId]?.name || user.name,
          partnerId: tgtCell ? tgtCell.teacherId : srcCell.teacherId,
          partnerName: tgtCell ? teachersMap[tgtCell.teacherId]?.name || tgtCell.teacherId : "빈 교시 이동",
          partnerAgreed: true,
          sourceCell: srcCell,
          targetCell: tgtCell || { ...srcCell, id: "", dayOfWeek: targetDay, period: targetPeriod },
          chainSteps: evalRes.chainSteps || [],
          reason: `[Canvas ${isDateOnly ? "일회성" : "정규"}:${evalRes.mode}] ${evalRes.reason}`,
        }),
      });
      onNotify("대기열(STAGING) 등록 완료", "시간표 담당교사가 최종 선택(서버 반영)을 누르면 반영됩니다.");
      onRefresh();
    } catch (err) {
      onNotify("등록 오류", err.message, true);
    }
  };

  const thisMonday = getMondayISO();
  const exportMenuItems = [
    {
      label: "결보강·수업교환 대장 (.hwpx)",
      icon: FileText,
      onSelect: () => downloadAuthenticatedFile(serverUrl, "/api/export/hwpx?kind=timetable", "결보강_수업교환_대장.hwpx").catch((err) => onNotify("문서 출력 실패", err.message, true)),
    },
    {
      label: "나이스 결보강 업로드 (.xlsx)",
      icon: FileSpreadsheet,
      onSelect: () => downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=timetable", "나이스_결보강대장.xlsx").catch((err) => onNotify("엑셀 출력 실패", err.message, true)),
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", justifyContent: "space-between" }}>
        <Tabs.Root value={subTab} onValueChange={setSubTab}>
          <Tabs.List className="radix-tabs-list" aria-label="시간표 세부 메뉴">
            <Tabs.Trigger value="board" className="radix-tabs-trigger">
              <TableProperties size={14} /> 주간 시간표 & 결보강
            </Tabs.Trigger>
            <Tabs.Trigger value="multiweek" className="radix-tabs-trigger">
              <AlertTriangle size={14} /> 주중 미출근 · 다음주 교환
            </Tabs.Trigger>
            <Tabs.Trigger value="workflow" className="radix-tabs-trigger">
              <Workflow size={14} /> 창체 · 편성단계
            </Tabs.Trigger>
            <Tabs.Trigger value="ledger" className="radix-tabs-trigger">
              <ClipboardList size={14} /> 학기 시수 원장
            </Tabs.Trigger>
            {isAdmin && (
              <>
                <Tabs.Trigger value="curriculum" className="radix-tabs-trigger">
                  <BookOpenCheck size={14} /> 시수표 · 교과교실
                </Tabs.Trigger>
                <Tabs.Trigger value="constraints" className="radix-tabs-trigger">
                  <Sliders size={14} /> 공강·기피 제약
                </Tabs.Trigger>
                <Tabs.Trigger value="scenarios" className="radix-tabs-trigger">
                  <History size={14} /> A/B/C 시나리오
                </Tabs.Trigger>
                <Tabs.Trigger value="builder" className="radix-tabs-trigger">
                  <Wrench size={14} /> 기초 시간표 편집
                </Tabs.Trigger>
                <Tabs.Trigger value="teachers" className="radix-tabs-trigger">
                  <Users size={14} /> 교사 명렬표
                </Tabs.Trigger>
              </>
            )}
          </Tabs.List>
        </Tabs.Root>

        {isAdmin && (
          <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
            <AppTooltip content={historyStatus.lastUndo ? `Ctrl+Z: ${historyStatus.lastUndo}` : "실행취소 (Ctrl+Z)"}>
              <button type="button" className="btn btn-sm" disabled={!historyStatus.canUndo} onClick={handleUndo}>
                <Undo2 size={14} /> 실행취소 ({historyStatus.undoCount || 0})
              </button>
            </AppTooltip>
            <AppTooltip content={historyStatus.lastRedo ? `Ctrl+Y: ${historyStatus.lastRedo}` : "다시실행 (Ctrl+Y)"}>
              <button type="button" className="btn btn-sm" disabled={!historyStatus.canRedo} onClick={handleRedo}>
                <Redo2 size={14} /> 다시실행 ({historyStatus.redoCount || 0})
              </button>
            </AppTooltip>
          </div>
        )}
      </div>

      {subTab === "multiweek" ? (
        <MultiWeekAbsenceView
          serverUrl={serverUrl}
          user={user}
          teachers={teachers}
          baseCells={cells}
          activeWeekStart={selectedWeekStart || thisMonday}
          onRefresh={onRefresh}
          onNotify={onNotify}
        />
      ) : subTab === "workflow" ? (
        <TimetableWorkflowPanel serverUrl={serverUrl} user={user} onRefresh={onRefresh} onNotify={onNotify} />
      ) : subTab === "ledger" ? (
        <SemesterLedgerPanel serverUrl={serverUrl} user={user} onNotify={onNotify} />
      ) : isAdmin && subTab === "curriculum" ? (
        <CurriculumLoadManager serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />
      ) : isAdmin && subTab === "constraints" ? (
        <ConstraintManager serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />
      ) : isAdmin && subTab === "scenarios" ? (
        <ScenarioComparePanel serverUrl={serverUrl} user={user} cells={cells} onRefresh={onRefresh} onHistoryRefresh={loadHistory} onNotify={onNotify} />
      ) : isAdmin && subTab === "builder" ? (
        <BaseTimetableBuilder
          serverUrl={serverUrl}
          user={user}
          cells={cells}
          teachers={teachers}
          bellSchedule={bellSchedule}
          historyStatus={historyStatus}
          onUndo={handleUndo}
          onRedo={handleRedo}
          onRefresh={onRefresh}
          onNotify={onNotify}
        />
      ) : isAdmin && subTab === "teachers" ? (
        <TeacherManagerCard serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />
      ) : (
        <>
          <div className="card">
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 10 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                <h2 style={{ margin: 0, fontSize: 17 }}>정규·다주간 스마트 시간표 보드 (Canvas)</h2>
                <select className="select" style={{ width: 150 }} value={filterClass} onChange={(e) => setFilterClass(e.target.value)}>
                  {classList.map((c) => (
                    <option key={c} value={c}>{c === "ALL" ? "전체 학급 보기" : `${c}반 시간표`}</option>
                  ))}
                </select>
                {workflowState.phase === "LOCKED" && (
                  <span className="badge badge-red"><Lock size={11} /> 정규 잠금됨</span>
                )}
              </div>
              <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                <button type="button" className="btn btn-sm btn-primary" onClick={() => setExplorerOpen(true)}>
                  <Compass size={14} /> 수업 교체 전수 탐색
                </button>
                {isAdmin && (
                  <button type="button" className="btn btn-primary btn-sm" onClick={onOpenApproval}>
                    <CheckSquare size={14} /> 대기열 비교 승인 ({(requests || []).filter((r) => r.status === "STAGING").length}건)
                  </button>
                )}
                <ActionMenu triggerLabel="문서 출력" triggerIcon={FileText} items={exportMenuItems} />
              </div>
            </div>

            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 6, padding: "8px 10px", marginBottom: 8, background: "#f8fafc", borderRadius: 8, border: "1px solid #e2e8f0" }}>
              <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                <button type="button" className="btn btn-sm" onClick={() => setSelectedWeekStart(addDaysISO(selectedWeekStart || thisMonday, -7))}>
                  <ChevronLeft size={13} /> 이전 주
                </button>
                <button type="button" className={`btn btn-sm ${selectedWeekStart === thisMonday ? "btn-primary" : ""}`} onClick={() => setSelectedWeekStart(thisMonday)}>
                  이번 주
                </button>
                <button type="button" className={`btn btn-sm ${selectedWeekStart === addDaysISO(thisMonday, 7) ? "btn-primary" : ""}`} onClick={() => setSelectedWeekStart(addDaysISO(thisMonday, 7))}>
                  다음 주 (+1주)
                </button>
                <button type="button" className={`btn btn-sm ${selectedWeekStart === addDaysISO(thisMonday, 14) ? "btn-primary" : ""}`} onClick={() => setSelectedWeekStart(addDaysISO(thisMonday, 14))}>
                  다다음 주 (+2주)
                </button>
                <button type="button" className="btn btn-sm" onClick={() => setSelectedWeekStart(addDaysISO(selectedWeekStart || thisMonday, 7))}>
                  다음 주 <ChevronRight size={13} />
                </button>
                <button type="button" className={`btn btn-sm ${selectedWeekStart === "" ? "btn-primary" : ""}`} onClick={() => { setSelectedWeekStart(""); setChangeScope(""); }}>
                  정규 기본 시간표
                </button>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                <select className="select" style={{ width: 175 }} value={changeScope} onChange={(e) => setChangeScope(e.target.value)}>
                  <option value="DATE_ONLY">변경 모드: 일회성(주차 한정)</option>
                  <option value="">변경 모드: 정규 영구 수정</option>
                </select>
                <label style={{ fontSize: 12, display: "inline-flex", alignItems: "center", gap: 4 }}>
                  <input type="checkbox" checked={moveWholeBlock} onChange={(e) => setMoveWholeBlock(e.target.checked)} />
                  선택·연강 통째이동
                </label>
                <span className="badge badge-blue">
                  {selectedWeekStart ? `조회 주간: ${selectedWeekStart} ~ ${addDaysISO(selectedWeekStart, 4)}` : "정규 기본 시간표 (전체 주차 공통)"}
                </span>
              </div>
            </div>

            <TimetableCanvas
              cells={activeCells}
              teachersMap={teachersMap}
              bellSchedule={bellSchedule}
              filterClass={filterClass}
              weekStart={selectedWeekStart}
              onSelectCell={(c) => { setSelectedCellId(c.id); setSourceId(c.id); }}
              onEvaluateDrop={handleEvaluateDrop}
              onRequestCommit={handleRequestFromCanvas}
            />
          </div>

          <ChainSwapPreview
            serverUrl={serverUrl}
            user={user}
            cells={activeCells}
            teachersMap={teachersMap}
            weekStart={selectedWeekStart}
            changeScope={changeScope}
            activeChain={activeChain}
            onClearChain={() => setActiveChain(null)}
            onRefresh={onRefresh}
            onNotify={onNotify}
          />

          <TimetableRequestAndNEISPanel
            serverUrl={serverUrl}
            user={user}
            isAdmin={isAdmin}
            cells={cells}
            requests={requests}
            approvedList={approvedList}
            teachersMap={teachersMap}
            partnerTeachers={partnerTeachers}
            reqType={reqType}
            setReqType={setReqType}
            sourceId={sourceId}
            setSourceId={setSourceId}
            partnerId={partnerId}
            setPartnerId={setPartnerId}
            reason={reason}
            setReason={setReason}
            changeScope={changeScope}
            setChangeScope={setChangeScope}
            selectedWeekStart={selectedWeekStart}
            onRefresh={onRefresh}
            onNotify={onNotify}
          />

          <SwapExplorerModal
            open={explorerOpen}
            onOpenChange={setExplorerOpen}
            serverUrl={serverUrl}
            user={user}
            cells={activeCells}
            teachersMap={teachersMap}
            weekStart={selectedWeekStart || thisMonday}
            defaultScope={changeScope}
            initialSourceId={selectedCellId || sourceId}
            onRefresh={onRefresh}
            onNotify={onNotify}
          />
        </>
      )}
    </div>
  );
}
