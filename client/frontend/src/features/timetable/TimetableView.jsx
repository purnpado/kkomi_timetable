import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { AlertTriangle, BookOpenCheck, CheckSquare, ChevronLeft, ChevronRight, Copy, FileSpreadsheet, FileText, History, PlusCircle, Redo2, Sliders, TableProperties, Undo2, Users, Wrench } from "lucide-react";
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

// TimetableView는 60fps HTML5 Canvas 다주간 시간표 조정, 주중 미출근·주차간 교환,
// 교사별 시수표/교과교실·공강제약·시나리오 비교 및 실행취소(Undo/Redo)·나이스(NEIS) 입력 도우미를 제공합니다.
export default function TimetableView({ serverUrl, user, cells, teachers, requests, bellSchedule, onRefresh, onOpenApproval, onNotify }) {
  const [subTab, setSubTab] = useState("board"); // "board" | "multiweek" | "curriculum" | "constraints" | "scenarios" | "builder" | "teachers"
  const [selectedWeekStart, setSelectedWeekStart] = useState(() => getMondayISO());
  const [weekCells, setWeekCells] = useState(null);
  const [filterClass, setFilterClass] = useState("ALL");
  const [activeChain, setActiveChain] = useState(null);
  const [historyStatus, setHistoryStatus] = useState({ canUndo: false, canRedo: false, undoCount: 0, redoCount: 0, lastUndo: "", lastRedo: "" });
  const [reason, setReason] = useState("학년 협의 및 수업 교환");
  const [reqType, setReqType] = useState("SWAP");
  const [sourceId, setSourceId] = useState(cells[0]?.id || "");
  const [partnerId, setPartnerId] = useState("");

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
          targetDate: selectedWeekStart,
        }),
      });
    } catch {
      return { signal: "RED", reason: "서버 판정 연결 오류" };
    }
  };

  const handleRequestFromCanvas = async (srcCell, tgtCell, targetDay, targetPeriod, evalRes) => {
    if (evalRes.mode === "TRIANGLE" || evalRes.mode === "QUAD") {
      setActiveChain({ evalResult: evalRes });
      onNotify(`${evalRes.chainSteps?.length || 3}단계 연쇄 교환 경로 확인`, "아래 연쇄 교환 미리보기에서 1~4단계 변경 과정을 확인한 뒤 신청하세요.");
      return;
    }
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: tgtCell ? "SWAP" : "MOVE",
          requesterId: user.id,
          requesterName: teachersMap[srcCell.teacherId]?.name || user.name,
          partnerId: tgtCell ? tgtCell.teacherId : srcCell.teacherId,
          partnerName: tgtCell ? teachersMap[tgtCell.teacherId]?.name || tgtCell.teacherId : "빈 교시 이동",
          partnerAgreed: true,
          sourceCell: srcCell,
          targetCell: tgtCell || { ...srcCell, id: "", dayOfWeek: targetDay, period: targetPeriod },
          chainSteps: evalRes.chainSteps || [],
          reason: `[Canvas 조정: ${evalRes.mode}] ${evalRes.reason}`,
        }),
      });
      onNotify("대기열(STAGING) 등록 완료", "시간표 담당교사가 최종 선택(서버 반영)을 누르면 마스터 시간표에 반영됩니다.");
      onRefresh();
    } catch (err) {
      onNotify("등록 오류", err.message, true);
    }
  };

  const handleManualSubmit = async (e) => {
    e.preventDefault();
    const src = cells.find((c) => c.id === sourceId) || cells[0];
    if (!src || !partnerId) return;
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: reqType,
          requesterId: user.id,
          requesterName: user.name,
          partnerId,
          partnerName: teachersMap[partnerId]?.name || partnerId,
          partnerAgreed: true,
          sourceCell: src,
          targetCell: { ...src, teacherId: partnerId },
          reason,
        }),
      });
      onNotify("수업 변경 신청 완료", "담당교사의 승인 대기열로 전달되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("신청 실패", err.message, true);
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
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <h2 style={{ margin: 0, fontSize: 17 }}>정규·다주간 스마트 시간표 보드 (Canvas)</h2>
                <select className="select" style={{ width: 150 }} value={filterClass} onChange={(e) => setFilterClass(e.target.value)}>
                  {classList.map((c) => (
                    <option key={c} value={c}>{c === "ALL" ? "전체 학급 보기" : `${c}반 시간표`}</option>
                  ))}
                </select>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                {isAdmin && (
                  <button type="button" className="btn btn-primary btn-sm" onClick={onOpenApproval}>
                    <CheckSquare size={14} /> 대기열 비교 승인 ({(requests || []).filter((r) => r.status === "STAGING").length}건)
                  </button>
                )}
                <ActionMenu triggerLabel="문서 출력" triggerIcon={FileText} items={exportMenuItems} />
              </div>
            </div>

            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 6, padding: "8px 10px", marginBottom: 10, background: "#f8fafc", borderRadius: 8, border: "1px solid #e2e8f0" }}>
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
                <button type="button" className={`btn btn-sm ${selectedWeekStart === "" ? "btn-primary" : ""}`} onClick={() => setSelectedWeekStart("")}>
                  정규 기본 시간표
                </button>
              </div>
              <span className="badge badge-blue">
                {selectedWeekStart ? `조회 주간: ${selectedWeekStart} ~ ${addDaysISO(selectedWeekStart, 4)}` : "정규 기본 시간표 (전체 주차 공통)"}
              </span>
            </div>

            <TimetableCanvas
              cells={activeCells}
              teachersMap={teachersMap}
              bellSchedule={bellSchedule}
              filterClass={filterClass}
              weekStart={selectedWeekStart}
              onEvaluateDrop={handleEvaluateDrop}
              onRequestCommit={handleRequestFromCanvas}
            />
          </div>

          <ChainSwapPreview
            serverUrl={serverUrl}
            user={user}
            cells={activeCells}
            teachersMap={teachersMap}
            activeChain={activeChain}
            onClearChain={() => setActiveChain(null)}
            onRefresh={onRefresh}
            onNotify={onNotify}
          />

          <div className="grid-2">
            <div className="card">
              <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>수업 교환 / 결보강 신청 (STAGING 등록)</h3>
              <form onSubmit={handleManualSubmit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                <div className="grid-2">
                  <select className="select" value={reqType} onChange={(e) => setReqType(e.target.value)}>
                    <option value="SWAP">수업 맞교환</option>
                    <option value="SUBSTITUTE">결보강 신청</option>
                  </select>
                  <select className="select" value={sourceId} onChange={(e) => setSourceId(e.target.value)}>
                    {(cells || []).map((c) => (
                      <option key={c.id} value={c.id}>{c.className} {c.dayOfWeek}요일 {c.period}교시 ({c.subject})</option>
                    ))}
                  </select>
                </div>
                <div className="grid-2">
                  <select className="select" value={partnerId} onChange={(e) => setPartnerId(e.target.value)}>
                    {partnerTeachers.length === 0 ? (
                      <option value="">선택 가능한 교사 없음</option>
                    ) : (
                      partnerTeachers.map((t) => (
                        <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>
                      ))
                    )}
                  </select>
                  <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="신청 사유" />
                </div>
                <button type="submit" className="btn btn-primary" style={{ alignSelf: "flex-start" }} disabled={cells.length === 0}>
                  <PlusCircle size={15} /> 대기열 등록 요청
                </button>
              </form>
            </div>

            <div className="card">
              <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>나이스(NEIS) 입력 도우미 (확정 순 정렬)</h3>
              {approvedList.length === 0 ? (
                <div style={{ fontSize: 13, color: "#64748b" }}>확정된 수업 변경 내역이 없습니다.</div>
              ) : (
                <table className="table">
                  <thead>
                    <tr><th>일자/학급</th><th>교시</th><th>과목코드</th><th>변경 내역</th></tr>
                  </thead>
                  <tbody>
                    {approvedList.map((r) => (
                      <tr key={r.id}>
                        <td>{r.targetDate} · {r.sourceCell?.className}</td>
                        <td>{r.sourceCell?.period}교시</td>
                        <td>
                          <button
                            type="button"
                            className="btn btn-sm"
                            onClick={() => {
                              navigator.clipboard?.writeText(r.sourceCell?.subjectCode || "");
                              onNotify("나이스 코드 복사", `과목코드 '${r.sourceCell?.subjectCode}'가 복사되었습니다.`);
                            }}
                          >
                            <Copy size={11} /> {r.sourceCell?.subjectCode || "코드"}
                          </button>
                        </td>
                        <td>{r.requesterName} → {r.partnerName}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
