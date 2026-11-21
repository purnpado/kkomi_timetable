import React, { useEffect, useMemo, useState } from "react";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Download, Lock, Plus, Redo2, Save, Sliders, Sparkles, Trash2, Undo2, Unlock, Upload } from "lucide-react";
import { apiFetch, hasRegularClasses, isFacultyTeacher } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import AppTooltip from "../../components/ui/AppTooltip";
import BellScheduleEditor from "./BellScheduleEditor";

const DAYS = ["", "월", "화", "수", "목", "금"];
const PERIODS = [1, 2, 3, 4, 5, 6, 7];

// BaseTimetableBuilder는 마스터 및 시간표 담당교사가 클라이언트에서 직접
// 학년별·학급별 기초 시간표 및 일과시간을 작성·자동 편성·엑셀 업로드·우클릭 잠금 제어할 수 있는 작성기입니다.
export default function BaseTimetableBuilder({
  serverUrl,
  user,
  cells,
  teachers,
  bellSchedule = [],
  historyStatus,
  onUndo,
  onRedo,
  onRefresh,
  onNotify,
}) {
  const [grade, setGrade] = useState(1);
  const [className, setClassName] = useState("1-1");
  const [newClassInput, setNewClassInput] = useState("");
  const [customClasses, setCustomClasses] = useState([]);
  const [classesPerGrade, setClassesPerGrade] = useState(3);
  const [clearBeforeAuto, setClearBeforeAuto] = useState(false);

  const [selectedDay, setSelectedDay] = useState(1);
  const [selectedPeriod, setSelectedPeriod] = useState(1);
  const [subject, setSubject] = useState("국어");
  const [subjectCode, setSubjectCode] = useState("KOR101");
  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const regularTeachers = useMemo(() => {
    const list = facultyTeachers.filter(hasRegularClasses);
    return list.length > 0 ? list : facultyTeachers;
  }, [facultyTeachers]);
  const [teacherId, setTeacherId] = useState("");
  const [coTeacherId, setCoTeacherId] = useState("");
  const [room, setRoom] = useState("1-1교실");
  const [electiveGroup, setElectiveGroup] = useState("");
  const [confirmDeleteSlot, setConfirmDeleteSlot] = useState(null);

  useEffect(() => {
    if (regularTeachers.length === 0) return;
    if (!regularTeachers.some((t) => t.id === teacherId)) {
      setTeacherId(regularTeachers[0].id);
      if (regularTeachers[0].subject) setSubject(regularTeachers[0].subject);
    }
  }, [regularTeachers, teacherId]);

  const teachersMap = useMemo(() => {
    const m = {};
    regularTeachers.forEach((t) => { m[t.id] = t; });
    return m;
  }, [regularTeachers]);

  const classOptions = useMemo(() => {
    const set = new Set([`${grade}-1`, `${grade}-2`, ...customClasses.filter((c) => c.startsWith(`${grade}-`))]);
    (cells || []).forEach((c) => {
      if (c.grade === grade || c.className.startsWith(`${grade}-`)) set.add(c.className);
    });
    return Array.from(set).sort();
  }, [grade, cells, customClasses]);

  const classCells = useMemo(() => (cells || []).filter((c) => c.className === className), [cells, className]);
  const findSlot = (d, p) => classCells.find((c) => c.dayOfWeek === d && c.period === p);
  const currentSlot = findSlot(selectedDay, selectedPeriod);

  const selectSlot = (d, p) => {
    setSelectedDay(d);
    setSelectedPeriod(p);
    const ex = findSlot(d, p);
    if (ex) {
      setSubject(ex.subject || "");
      setSubjectCode(ex.subjectCode || "");
      setTeacherId(ex.teacherId || regularTeachers[0]?.id || "");
      setCoTeacherId(ex.coTeacherId || "");
      setRoom(ex.room || `${className}교실`);
      setElectiveGroup(ex.electiveGroup || "");
    } else {
      setRoom(`${className}교실`);
      setCoTeacherId("");
      setElectiveGroup("");
    }
  };

  const handleSaveCell = async (e) => {
    e.preventDefault();
    try {
      await apiFetch(serverUrl, "/api/timetable/cells", user.id, {
        method: "POST",
        body: JSON.stringify({
          id: currentSlot?.id || "",
          dayOfWeek: selectedDay,
          period: selectedPeriod,
          className,
          grade: Number(grade),
          subject,
          subjectCode,
          teacherId,
          coTeacherId,
          room,
          electiveGroup,
          isLocked: Boolean(currentSlot?.isLocked),
        }),
      });
      onNotify("기초 시간표 저장 완료", `${className}반 ${DAYS[selectedDay]}요일 ${selectedPeriod}교시(${subject})가 반영되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("시간표 저장 오류", err.message, true);
    }
  };

  const handleDeleteSlot = async (slot) => {
    if (!slot?.id) return;
    try {
      await apiFetch(serverUrl, `/api/timetable/cells?id=${encodeURIComponent(slot.id)}`, user.id, {
        method: "DELETE",
      });
      onNotify("교시 삭제 완료", `${slot.className}반 ${DAYS[slot.dayOfWeek]}요일 ${slot.period}교시가 비워졌습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("삭제 실패", err.message, true);
    }
  };

  const handleToggleLockCell = async (slot) => {
    if (!slot?.id) return;
    try {
      await apiFetch(serverUrl, "/api/timetable/cells", user.id, {
        method: "POST",
        body: JSON.stringify({ ...slot, isLocked: !slot.isLocked }),
      });
      onNotify(
        !slot.isLocked ? "이동금지 잠금 설정" : "이동금지 잠금 해제",
        `${slot.className}반 ${DAYS[slot.dayOfWeek]} ${slot.period}교시(${slot.subject}) 상태가 변경되었습니다.`
      );
      onRefresh();
    } catch (err) {
      onNotify("잠금 변경 실패", err.message, true);
    }
  };

  const handleRegisterHardAvoid = async (slot) => {
    if (!slot?.teacherId) return;
    try {
      const tName = teachersMap[slot.teacherId]?.name || slot.teacherId;
      await apiFetch(serverUrl, "/api/timetable/constraints", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacherId: slot.teacherId,
          dayOfWeek: slot.dayOfWeek,
          period: slot.period,
          constraintType: "HARD_AVOID",
          reason: `기초 시간표(${DAYS[slot.dayOfWeek]} ${slot.period}교시) 고정 공강 지정`,
        }),
      });
      onNotify("절대 금지 공강 등록 완료", `${tName} 선생님의 ${DAYS[slot.dayOfWeek]}요일 ${slot.period}교시 고정 공강이 등록되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("공강 등록 실패", err.message, true);
    }
  };

  const handleAutoGenerate = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/auto-generate", user.id, {
        method: "POST",
        body: JSON.stringify({ grades: [1, 2, 3], classesPerGrade: Number(classesPerGrade), clearExisting: clearBeforeAuto }),
      });
      onNotify("학년·학급별 자동 편성 완료", `총 ${res.createdCount}개의 교시 셀이 충돌 없이 자동 배치되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("자동 편성 실패", err.message, true);
    }
  };

  const handleImportXLSX = async (file) => {
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/import-xlsx", user.id, { method: "POST", body: form });
      onNotify("엑셀 일괄 업로드 완료", `${res.importedCount}건의 기초 시간표 셀이 반영되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("엑셀 업로드 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <BellScheduleEditor serverUrl={serverUrl} user={user} bellSchedule={bellSchedule} onRefresh={onRefresh} onNotify={onNotify} />
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <b>학년·학급 선택:</b>
            {[1, 2, 3].map((g) => (
              <button
                key={g}
                type="button"
                className={`btn btn-sm ${grade === g ? "btn-primary" : ""}`}
                onClick={() => {
                  setGrade(g);
                  const nextCls = `${g}-1`;
                  setClassName(nextCls);
                  setRoom(`${nextCls}교실`);
                }}
              >
                {g}학년
              </button>
            ))}
            <select className="select" style={{ width: 120 }} value={className} onChange={(e) => { setClassName(e.target.value); setRoom(`${e.target.value}교실`); }}>
              {classOptions.map((c) => <option key={c} value={c}>{c}반</option>)}
            </select>
            <input
              className="input"
              style={{ width: 110 }}
              placeholder="새 학급(예:1-4)"
              value={newClassInput}
              onChange={(e) => setNewClassInput(e.target.value)}
            />
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => {
                const v = newClassInput.trim();
                if (!v) return;
                setCustomClasses((prev) => [...prev, v]);
                setClassName(v);
                setRoom(`${v}교실`);
                setNewClassInput("");
              }}
            >
              <Plus size={13} /> 학급 추가
            </button>
          </div>

          <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
            <span style={{ fontSize: 12 }}>학년별 반 수:</span>
            <input className="input" type="number" min={1} max={15} style={{ width: 64 }} value={classesPerGrade} onChange={(e) => setClassesPerGrade(e.target.value)} />
            <label style={{ fontSize: 12, display: "flex", alignItems: "center", gap: 4 }}>
              <input type="checkbox" checked={clearBeforeAuto} onChange={(e) => setClearBeforeAuto(e.target.checked)} />
              초기화 후 재편성
            </label>
            <button type="button" className="btn btn-sm btn-green" onClick={handleAutoGenerate}>
              <Sparkles size={13} /> 전 학년 기초 시간표 자동 편성
            </button>
            <button
              type="button"
              className="btn btn-sm"
              onClick={() =>
                downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=base-template", "기초시간표_업로드양식.xlsx")
                  .catch((err) => onNotify("양식 다운로드 실패", err.message, true))
              }
            >
              <Download size={13} /> 엑셀 양식
            </button>
            <label className="btn btn-sm" style={{ cursor: "pointer" }}>
              <Upload size={13} /> 엑셀 업로드
              <input type="file" accept=".xlsx" style={{ display: "none" }} onChange={(e) => handleImportXLSX(e.target.files?.[0])} />
            </label>
            {onUndo && (
              <AppTooltip content="직전 편집 실행취소 (Ctrl+Z)">
                <button type="button" className="btn btn-sm" disabled={!historyStatus?.canUndo} onClick={onUndo}>
                  <Undo2 size={13} /> 실행취소
                </button>
              </AppTooltip>
            )}
            {onRedo && (
              <AppTooltip content="실행취소 되돌리기 (Ctrl+Y)">
                <button type="button" className="btn btn-sm" disabled={!historyStatus?.canRedo} onClick={onRedo}>
                  <Redo2 size={13} /> 다시실행
                </button>
              </AppTooltip>
            )}
          </div>
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "1.6fr 1fr", gap: 16 }}>
        <div className="card" style={{ overflowX: "auto" }}>
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>{className}반 기초 시간표 편성표 (클릭 편집 · 우클릭 빠른 메뉴)</h3>
          <table className="table">
            <thead>
              <tr><th>교시</th>{DAYS.slice(1).map((d) => <th key={d}>{d}요일</th>)}</tr>
            </thead>
            <tbody>
              {PERIODS.map((p) => {
                const bell = (bellSchedule || []).find((b) => b.period === p);
                return (
                <tr key={p}>
                  <td>
                    <b>{p}교시</b>
                    {bell && <div style={{ fontSize: 10.5, color: "#64748b" }}>{bell.startTime}~{bell.endTime}</div>}
                  </td>
                  {[1, 2, 3, 4, 5].map((d) => {
                    const slot = findSlot(d, p);
                    const isSel = selectedDay === d && selectedPeriod === p;
                    return (
                      <ContextMenu.Root key={d}>
                        <ContextMenu.Trigger asChild>
                          <td
                            onClick={() => selectSlot(d, p)}
                            style={{
                              cursor: "pointer",
                              background: isSel ? "#edf2ff" : slot ? "#f8fafc" : "#fff",
                              outline: isSel ? "2px solid #3b5bdb" : "none",
                              minWidth: 96,
                            }}
                          >
                            {slot ? (
                              <div>
                                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 4 }}>
                                  <span style={{ fontWeight: 700, fontSize: 12.5 }}>{slot.subject}</span>
                                  {slot.isLocked && <span className="cell-locked-badge"><Lock size={9} /> 고정</span>}
                                </div>
                                <div style={{ fontSize: 11, color: "#475569" }}>{teachersMap[slot.teacherId]?.name || slot.teacherId}</div>
                              </div>
                            ) : (
                              <span style={{ fontSize: 11, color: "#94a3b8" }}>+ 빈 교시</span>
                            )}
                          </td>
                        </ContextMenu.Trigger>
                        <ContextMenu.Portal>
                          <ContextMenu.Content className="radix-menu-content">
                            <ContextMenu.Label className="radix-menu-label">
                              {className}반 {DAYS[d]}요일 {p}교시
                            </ContextMenu.Label>
                            <ContextMenu.Item className="radix-menu-item" onSelect={() => selectSlot(d, p)}>
                              이 교시 편집 선택
                            </ContextMenu.Item>
                            {slot && (
                              <>
                                <ContextMenu.Item className="radix-menu-item" onSelect={() => handleToggleLockCell(slot)}>
                                  {slot.isLocked ? <Unlock size={13} /> : <Lock size={13} />}
                                  <span>{slot.isLocked ? "이동금지 잠금 해제" : "이동금지 잠금 설정"}</span>
                                </ContextMenu.Item>
                                <ContextMenu.Item className="radix-menu-item" onSelect={() => handleRegisterHardAvoid(slot)}>
                                  <Sliders size={13} /> 담당교사 고정 공강(HARD) 등록
                                </ContextMenu.Item>
                                <ContextMenu.Separator className="radix-menu-separator" />
                                <ContextMenu.Item className="radix-menu-item radix-menu-item-danger" onSelect={() => setConfirmDeleteSlot(slot)}>
                                  <Trash2 size={13} /> 이 교시 삭제
                                </ContextMenu.Item>
                              </>
                            )}
                          </ContextMenu.Content>
                        </ContextMenu.Portal>
                      </ContextMenu.Root>
                    );
                  })}
                </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 12px", fontSize: 15 }}>
            {className}반 {DAYS[selectedDay]}요일 {selectedPeriod}교시 편집
          </h3>
          {regularTeachers.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b", padding: "12px 0" }}>
              아직 등록된 교과 교사가 없습니다. 먼저 [교사 명렬표 관리] 탭에서 교사를 등록해 주세요.
            </div>
          ) : (
            <form onSubmit={handleSaveCell} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <div className="grid-2">
                <div>
                  <label style={{ fontSize: 12, fontWeight: 700 }}>과목명</label>
                  <input className="input" value={subject} onChange={(e) => setSubject(e.target.value)} required />
                </div>
                <div>
                  <label style={{ fontSize: 12, fontWeight: 700 }}>나이스 과목코드</label>
                  <input className="input" value={subjectCode} onChange={(e) => setSubjectCode(e.target.value)} />
                </div>
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 700 }}>담당교사 (중복·순회일 자동 검증)</label>
                <select
                  className="select"
                  value={teacherId}
                  onChange={(e) => {
                    setTeacherId(e.target.value);
                    if (teachersMap[e.target.value]?.subject) setSubject(teachersMap[e.target.value].subject);
                  }}
                >
                  {regularTeachers.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name} ({t.subject}{t.homeroomClass ? ` · ${t.homeroomClass}담임` : ""})
                    </option>
                  ))}
                </select>
              </div>
              <div className="grid-2">
                <div>
                  <label style={{ fontSize: 12, fontWeight: 700 }}>코티칭 협력교사 (선택)</label>
                  <select className="select" value={coTeacherId} onChange={(e) => setCoTeacherId(e.target.value)}>
                    <option value="">없음</option>
                    {regularTeachers.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>)}
                  </select>
                </div>
                <div>
                  <label style={{ fontSize: 12, fontWeight: 700 }}>교실/특별실</label>
                  <input className="input" value={room} onChange={(e) => setRoom(e.target.value)} />
                </div>
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 700 }}>고교학점제 선택블록 ID (선택)</label>
                <input className="input" value={electiveGroup} onChange={(e) => setElectiveGroup(e.target.value)} placeholder="예: G2-EL-A (일반수업은 비워둠)" />
              </div>
              <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
                <button type="submit" className="btn btn-primary">
                  <Save size={14} /> 교시 저장/반영
                </button>
                {currentSlot && (
                  <button type="button" className="btn btn-danger" onClick={() => setConfirmDeleteSlot(currentSlot)}>
                    <Trash2 size={14} /> 삭제
                  </button>
                )}
              </div>
            </form>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={Boolean(confirmDeleteSlot)}
        onOpenChange={(open) => !open && setConfirmDeleteSlot(null)}
        danger
        title="기초 시간표 교시 삭제 확인"
        description={`${confirmDeleteSlot?.className}반 ${DAYS[confirmDeleteSlot?.dayOfWeek || 1]}요일 ${confirmDeleteSlot?.period}교시(${confirmDeleteSlot?.subject || ""})를 삭제하시겠습니까?`}
        confirmLabel="삭제 실행"
        onConfirm={async () => {
          const target = confirmDeleteSlot;
          setConfirmDeleteSlot(null);
          await handleDeleteSlot(target);
        }}
      />
    </div>
  );
}
