import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { FileSpreadsheet, FileText, Lock, Printer, RefreshCw, Shield, Unlock, UserCheck } from "lucide-react";
import { apiFetch, hasRole } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import ActionMenu from "../../components/ui/ActionMenu";
import ExamFairnessPanel from "./ExamFairnessPanel";
import MultiDayExamEditor from "./MultiDayExamEditor";
import InternalExamSwapAndPersonalPanel from "./InternalExamSwapAndPersonalPanel";

function calcMinutes(start, end) {
  if (!start || !end) return 45;
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  const diff = eh * 60 + em - (sh * 60 + sm);
  return diff > 0 ? diff : 45;
}

// InternalExamView는 다중일(3~5일) 교내 지필고사 통합 편성, 연강 감독 방지,
// 일자별 사전 봉인·공개, 당일 아침 긴급 대타 및 한글/엑셀 배정표 출력을 제어합니다.
export default function InternalExamView({ serverUrl, user, onNotify }) {
  const [config, setConfig] = useState(null);
  const [slots, setSlots] = useState([]);
  const [specs, setSpecs] = useState([]);
  const [fairness, setFairness] = useState(null);
  const [roomCatalog, setRoomCatalog] = useState(null);
  const [slotFilterDate, setSlotFilterDate] = useState("ALL");
  const [selectedSlotId, setSelectedSlotId] = useState("");
  const [subCandidates, setSubCandidates] = useState([]);
  const [newSubId, setNewSubId] = useState("");
  const [confirmUnsealDate, setConfirmUnsealDate] = useState(null); // null | "" (all) | "YYYY-MM-DD"
  const [viewTab, setViewTab] = useState("schedule"); // "schedule" | "swap" | "fairness"

  const onNotifyRef = useRef(onNotify);
  onNotifyRef.current = onNotify;
  const isExamAdmin = hasRole(user, "INTERNAL_EXAM_ADMIN");

  const loadData = useCallback(async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/internal-exam/slots", user.id);
      setConfig(res.config);
      setSlots(res.slots || []);
      setSpecs(res.specs || []);
      setFairness(res.fairness || null);
      setRoomCatalog(res.roomCatalog || null);
    } catch (err) {
      onNotifyRef.current?.("시험감독 조회 오류", err.message, true);
    }
  }, [serverUrl, user?.id]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const examDates = useMemo(() => {
    const set = new Set(config?.examDates || []);
    if (config?.examDate) set.add(config.examDate);
    slots.forEach((s) => {
      if (s.examDate) set.add(s.examDate);
    });
    return Array.from(set).filter(Boolean).sort();
  }, [config?.examDate, config?.examDates, slots]);

  const visibleSlots = useMemo(
    () => (slotFilterDate === "ALL" ? slots : slots.filter((s) => s.examDate === slotFilterDate)),
    [slots, slotFilterDate]
  );

  const handleRegenerate = async () => {
    try {
      const normalizedSpecs = specs.map((sp) => ({
        ...sp,
        examDate: sp.examDate || config?.examDate || "",
        period: Number(sp.period),
        grade: Number(sp.grade),
        durationMinutes: Number(sp.durationMinutes) || calcMinutes(sp.startTime, sp.endTime),
        classRooms: Array.isArray(sp.classRooms)
          ? sp.classRooms.filter(Boolean)
          : String(sp.classRooms || "")
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean),
        studyHallRooms: Array.isArray(sp.studyHallRooms)
          ? sp.studyHallRooms.filter(Boolean)
          : String(sp.studyHallRooms || "")
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean),
        authorTeacherIds: Array.isArray(sp.authorTeacherIds) ? sp.authorTeacherIds : [],
      }));
      await apiFetch(serverUrl, "/api/internal-exam/generate", user.id, {
        method: "POST",
        body: JSON.stringify({ config, specs: normalizedSpecs }),
      });
      onNotifyRef.current?.("교내 감독 배정 완료", "다중일 시험시간·연강 방지·담임 배제 및 연간 균등화가 반영되었습니다.");
      loadData();
    } catch (err) {
      onNotifyRef.current?.("배정 실패", err.message, true);
    }
  };

  const handleUnseal = async (targetDate = "") => {
    try {
      await apiFetch(serverUrl, "/api/internal-exam/unseal", user.id, {
        method: "POST",
        body: JSON.stringify(targetDate ? { examDate: targetDate } : {}),
      });
      onNotifyRef.current?.(
        targetDate ? `${targetDate} 고사장 발표 완료` : "전체 시험 고사장 발표 완료",
        "전 교원 PC에 고사장 배정이 일괄 공개되었습니다.",
        true
      );
      loadData();
    } catch (err) {
      onNotifyRef.current?.("발표 실패", err.message, true);
    }
  };

  const handleLoadSubCandidates = async (slotId) => {
    setSelectedSlotId(slotId);
    try {
      const recs = await apiFetch(serverUrl, `/api/internal-exam/substitute/recommend?slotId=${slotId}`, user.id);
      setSubCandidates(recs || []);
      if (recs?.length > 0) setNewSubId(recs[0].id);
    } catch (err) {
      onNotifyRef.current?.("대타 추천 오류", err.message, true);
    }
  };

  const handleApplySubstitute = async () => {
    if (!selectedSlotId || !newSubId) return;
    try {
      await apiFetch(serverUrl, "/api/internal-exam/substitute/apply", user.id, {
        method: "POST",
        body: JSON.stringify({ slotId: selectedSlotId, newTeacherId: newSubId }),
      });
      onNotifyRef.current?.("긴급 대타 교체 완료", "대체 교사에게 최상단 긴급 알림이 전송되었습니다.", true);
      setSelectedSlotId("");
      loadData();
    } catch (err) {
      onNotifyRef.current?.("대타 교체 실패", err.message, true);
    }
  };

  const unsealedSet = useMemo(() => new Set(config?.unsealedDates || []), [config?.unsealedDates]);

  const exportItems = [
    {
      label: "교내 시험감독 배정표 (.hwpx)",
      icon: FileText,
      onSelect: () => downloadAuthenticatedFile(serverUrl, "/api/export/hwpx?kind=internal-exam", "교내시험감독_배정표.hwpx").catch((err) => onNotifyRef.current?.("출력 실패", err.message, true)),
    },
    {
      label: "시수·배정 현황 (.xlsx)",
      icon: FileSpreadsheet,
      onSelect: () => downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=internal-exam", "교내시험감독_배정_시수표.xlsx").catch((err) => onNotifyRef.current?.("엑셀 실패", err.message, true)),
    },
    { separator: true },
    {
      label: "고대비 시험감독 인쇄판 열기",
      icon: Printer,
      onSelect: () => downloadAuthenticatedFile(serverUrl, "/api/export/print?kind=internal-exam", "시험감독_인쇄판.html", { openInTab: true }).catch((err) => onNotifyRef.current?.("인쇄 실패", err.message, true)),
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {config && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
            <div>
              <h2 style={{ margin: 0, fontSize: 17 }}>
                {config.title} (
                {examDates.length > 1 ? `${examDates[0]} ~ ${examDates[examDates.length - 1]} · ${examDates.length}일간` : config.examDate}
                )
              </h2>
              <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 4 }}>
                핵심 규칙: <b>담임 자기 반 제외</b> · <b>담당과목 교시 복도감독 우선</b> · 다중일 연강 감독 방지
              </div>
            </div>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
              <ActionMenu triggerLabel="배정표·인쇄 출력" triggerIcon={FileText} items={exportItems} />
              <span className={`badge ${config.isSealed ? "badge-amber" : "badge-green"}`}>
                {config.isSealed ? <Lock size={12} /> : <Unlock size={12} />}
                {config.isSealed ? `사전 봉인 (${config.unsealTime} 공개)` : "전체 공개"}
              </span>
              {isExamAdmin && config.isSealed && (
                <button type="button" className="btn btn-primary btn-sm" onClick={() => setConfirmUnsealDate("")}>
                  <Unlock size={14} /> 전체 고사장 일괄 발표
                </button>
              )}
            </div>
          </div>

          {isExamAdmin && config.isSealed && examDates.length > 1 && (
            <div style={{ marginTop: 10, padding: "8px 10px", background: "#fffbeb", border: "1px solid #fde68a", borderRadius: 8, display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
              <span style={{ fontSize: 12, fontWeight: 700, color: "#92400e" }}>일자별 부분 공개:</span>
              {examDates.map((d) =>
                unsealedSet.has(d) ? (
                  <span key={d} className="badge badge-green">{d} 공개됨</span>
                ) : (
                  <button key={d} type="button" className="btn btn-sm" onClick={() => setConfirmUnsealDate(d)}>
                    <Unlock size={12} /> {d} 고사장 공개
                  </button>
                )
              )}
            </div>
          )}

          {isExamAdmin && (
            <>
              <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid #e2e8f0", display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center" }}>
                <select className="select" style={{ width: 210 }} value={config.deputyMode ? "DEPUTY" : "SINGLE"} onChange={(e) => setConfig({ ...config, deputyMode: e.target.value === "DEPUTY" })}>
                  <option value="SINGLE">운영 방식: 정감독 1인 단독</option>
                  <option value="DEPUTY">운영 방식: 정·부감독 2인 공동</option>
                </select>
                <select className="select" style={{ width: 240 }} value={config.balanceMode || "TOTAL_AND_ROLE"} onChange={(e) => setConfig({ ...config, balanceMode: e.target.value })}>
                  <option value="TOTAL_AND_ROLE">균등 기준: 1년 총 횟수 + 정·부 교차</option>
                  <option value="STRICT_ROLE">균등 기준: 정감독·부감독 각각 균등</option>
                </select>
                <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 6 }}>
                  <input type="checkbox" checked={config.honoraryParentOn} onChange={(e) => setConfig({ ...config, honoraryParentOn: e.target.checked })} />
                  학부모 명예감독 투입
                </label>
                <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 6 }}>
                  <input
                    type="checkbox"
                    checked={specs.some((sp) => Array.isArray(sp.classRooms) && sp.classRooms.includes("별도고사실(독감·감염병)"))}
                    onChange={(e) => {
                      const tag = "별도고사실(독감·감염병)";
                      setSpecs((prev) => prev.map((sp) => {
                        const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                        return { ...sp, classRooms: e.target.checked ? Array.from(new Set([...cur, tag])) : cur.filter((r) => r !== tag) };
                      }));
                    }}
                  />
                  별도 고사실 개설
                </label>
                <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 6 }}>
                  <input type="checkbox" checked={!config.instantRevealMode} onChange={(e) => setConfig({ ...config, instantRevealMode: !e.target.checked, isSealed: !e.target.checked })} />
                  사전 봉인 후 당일 발표
                </label>
                <select className="select" style={{ width: 220 }} value={config.subjectTeacherPolicy || "CORRIDOR_FIRST"} onChange={(e) => setConfig({ ...config, subjectTeacherPolicy: e.target.value })}>
                  <option value="CORRIDOR_FIRST">담당과목: 복도감독 우선</option>
                  <option value="HQ_STANDBY">담당과목: 평가본부 대기/공강</option>
                </select>
                <select className="select" style={{ width: 215 }} value={config.subjectExcludeMode || "ALL_SUBJECT"} onChange={(e) => setConfig({ ...config, subjectExcludeMode: e.target.value })}>
                  <option value="ALL_SUBJECT">과목 배제: 동일 교과 전체</option>
                  <option value="GRADE_ONLY">과목 배제: 해당 학년 담당만</option>
                  <option value="AUTHORS_ONLY">과목 배제: 출제 교사만</option>
                </select>
                <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 6 }}>
                  <input type="checkbox" checked={Boolean(config.preventSameRoomRepeat)} onChange={(e) => setConfig({ ...config, preventSameRoomRepeat: e.target.checked })} />
                  동일 고사실 중복 배정 금지
                </label>
                <button className="btn btn-green btn-sm" onClick={handleRegenerate}>
                  <RefreshCw size={14} /> 규칙·다중일 통합 자동 배정
                </button>
              </div>
              <MultiDayExamEditor config={config} setConfig={setConfig} specs={specs} setSpecs={setSpecs} roomCatalog={roomCatalog} stats={fairness?.stats || []} />
            </>
          )}
        </div>
      )}

      <Tabs.Root value={viewTab} onValueChange={setViewTab}>
        <Tabs.List className="radix-tabs-list" aria-label="교내 시험감독 세부 탭">
          <Tabs.Trigger value="schedule" className="radix-tabs-trigger">배정표 · 긴급대타 ({visibleSlots.length}건)</Tabs.Trigger>
          <Tabs.Trigger value="swap" className="radix-tabs-trigger">수동 맞교환 · 개인 감독 시간표</Tabs.Trigger>
          <Tabs.Trigger value="fairness" className="radix-tabs-trigger">시수 균등 · 차등 감면 정책</Tabs.Trigger>
        </Tabs.List>
      </Tabs.Root>

      {isExamAdmin && selectedSlotId && viewTab === "schedule" && (
        <div className="card" style={{ background: "#fff9db", borderColor: "#ffd43b" }}>
          <h3 style={{ margin: "0 0 8px", fontSize: 15 }}>당일 아침 돌발 결원 긴급 대타 추천 (연강·일일상한 회피 우선)</h3>
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

      {viewTab === "schedule" && (
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>
            <Shield size={15} style={{ verticalAlign: "middle" }} /> 일자·교시별 시험감독 배정표 ({visibleSlots.length}건)
          </h3>
          {examDates.length > 1 && (
            <Tabs.Root value={slotFilterDate} onValueChange={setSlotFilterDate}>
              <Tabs.List className="radix-tabs-list" aria-label="시험 일자별 필터">
                <Tabs.Trigger value="ALL" className="radix-tabs-trigger">전체 일자</Tabs.Trigger>
                {examDates.map((d, idx) => (
                  <Tabs.Trigger key={d} value={d} className="radix-tabs-trigger">
                    {idx + 1}일차 ({d})
                  </Tabs.Trigger>
                ))}
              </Tabs.List>
            </Tabs.Root>
          )}
        </div>
        <table className="table">
          <thead>
            <tr><th>시험일자 · 교시 · 시험시간</th><th>학년/과목</th><th>역할</th><th>배정 고사장 / 복도</th><th>감독교사</th><th>비고</th>{isExamAdmin && <th>긴급대타</th>}</tr>
          </thead>
          <tbody>
            {visibleSlots.map((s) => (
              <tr key={s.id}>
                <td>
                  <span style={{ fontSize: 12, color: "#475569", marginRight: 6 }}>{s.examDate}</span>
                  <b>{s.period}교시</b>{" "}
                  {s.startTime && (
                    <span className="badge badge-blue" style={{ marginLeft: 4 }}>
                      {s.startTime}~{s.endTime} ({s.durationMinutes || 45}분)
                    </span>
                  )}
                </td>
                <td>{s.grade}학년 <b>{s.subject}</b></td>
                <td>
                  <span className={`badge ${s.slotRole === "CORRIDOR" ? "badge-blue" : s.slotRole === "HONORARY" || s.slotRole === "STUDY_HALL" ? "badge-green" : "badge-gray"}`}>
                    {s.slotRole === "MAIN" ? "정감독" : s.slotRole === "SUB" ? "부감독" : s.slotRole === "CORRIDOR" ? "복도감독" : s.slotRole === "STUDY_HALL" ? "공강자습" : "학부모명예"}
                  </span>
                </td>
                <td>{s.actualRoomHidden ? <span className="badge badge-amber">{s.roomName}</span> : <b>{s.roomName}</b>}</td>
                <td><b>{s.teacherName}</b></td>
                <td style={{ fontSize: 12, color: "#64748b" }}>{s.note || "-"}</td>
                {isExamAdmin && (
                  <td><button className="btn btn-sm" onClick={() => handleLoadSubCandidates(s.id)}>대타 교체</button></td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      )}

      {viewTab === "swap" && (
        <InternalExamSwapAndPersonalPanel serverUrl={serverUrl} user={user} slots={slots} fairness={fairness} isExamAdmin={isExamAdmin} onRefresh={loadData} onNotify={onNotify} />
      )}

      {viewTab === "fairness" && (
        <ExamFairnessPanel serverUrl={serverUrl} user={user} fairness={fairness} isExamAdmin={isExamAdmin} onRefresh={loadData} onNotify={onNotify} />
      )}

      <ConfirmDialog
        open={confirmUnsealDate !== null}
        onOpenChange={(open) => !open && setConfirmUnsealDate(null)}
        title={confirmUnsealDate ? `${confirmUnsealDate} 고사장 공개 확인` : "전체 고사장 일괄 발표 확인"}
        description={
          confirmUnsealDate
            ? `${confirmUnsealDate} 날짜의 시험 고사장 배정을 전 교원 PC에 즉시 공개하시겠습니까?`
            : "봉인된 전체 시험 기간의 고사장 배정을 전 교원 PC에 즉시 일괄 공개하시겠습니까?"
        }
        confirmLabel="지금 공개 발표"
        onConfirm={async () => {
          const target = confirmUnsealDate;
          setConfirmUnsealDate(null);
          await handleUnseal(target || "");
        }}
      />
    </div>
  );
}
