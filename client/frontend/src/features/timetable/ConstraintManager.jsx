import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Layers, PlusCircle, ShieldAlert, Trash2, Users } from "lucide-react";
import { apiFetch, isFacultyTeacher } from "../../events/eventStore";

const DAYS = [
  { value: 0, label: "전체 요일" },
  { value: 1, label: "월요일" },
  { value: 2, label: "화요일" },
  { value: 3, label: "수요일" },
  { value: 4, label: "목요일" },
  { value: 5, label: "금요일" },
];
const PERIODS = [0, 1, 2, 3, 4, 5, 6, 7];

const TYPE_META = {
  HARD_AVOID: { label: "절대 금지", bg: "#fee2e2", color: "#991b1b" },
  MEETING_FREE: { label: "회의 공강", bg: "#fef3c7", color: "#92400e" },
  SOFT_AVOID: { label: "기피 교시", bg: "#e0f2fe", color: "#075985" },
};

// ConstraintManager는 교사별 고정 공강(절대 금지·회의 공강) 및 희망 공강(기피 교시) 제약을 관리합니다.
export default function ConstraintManager({ serverUrl, user, teachers, onRefresh, onNotify }) {
  const regularTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);

  const [constraints, setConstraints] = useState([]);
  const [filterTeacherId, setFilterTeacherId] = useState("ALL");

  // 개별 교사 등록 폼
  const [teacherId, setTeacherId] = useState("");
  const [dayOfWeek, setDayOfWeek] = useState(3);
  const [period, setPeriod] = useState(6);
  const [constraintType, setConstraintType] = useState("HARD_AVOID");
  const [reason, setReason] = useState("육아·외부 출강 고정 공강");

  // 일괄 등록(부장회의/교과협의회) 폼
  const [targetScope, setTargetScope] = useState("DEPT_HEADS");
  const [subject, setSubject] = useState("");
  const [selectedTeacherIds, setSelectedTeacherIds] = useState([]);
  const [batchDay, setBatchDay] = useState(3);
  const [batchPeriod, setBatchPeriod] = useState(6);
  const [batchType, setBatchType] = useState("MEETING_FREE");
  const [batchReason, setBatchReason] = useState("부장교사 주간 회의");

  const onNotifyRef = useRef(onNotify);
  onNotifyRef.current = onNotify;

  useEffect(() => {
    if (!teacherId && regularTeachers.length > 0) {
      setTeacherId(regularTeachers[0].id);
    }
  }, [regularTeachers, teacherId]);

  const loadConstraints = useCallback(async () => {
    try {
      const list = await apiFetch(serverUrl, "/api/timetable/constraints", user.id);
      setConstraints(list || []);
    } catch (err) {
      onNotifyRef.current?.("제약 목록 조회 실패", err.message, true);
    }
  }, [serverUrl, user?.id]);

  useEffect(() => {
    loadConstraints();
  }, [loadConstraints]);

  const handleSaveSingle = async (e) => {
    e.preventDefault();
    if (!teacherId) return;
    try {
      await apiFetch(serverUrl, "/api/timetable/constraints", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacherId,
          dayOfWeek: Number(dayOfWeek),
          period: Number(period),
          constraintType,
          reason,
        }),
      });
      onNotify("교사 제약 저장 완료", "시간표 자동 편성 및 이동/결보강 판정에 즉시 반영됩니다.");
      await loadConstraints();
      onRefresh?.();
    } catch (err) {
      onNotify("제약 저장 실패", err.message, true);
    }
  };

  const handleSaveBatch = async (e) => {
    e.preventDefault();
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/constraints/batch", user.id, {
        method: "POST",
        body: JSON.stringify({
          targetScope,
          subject,
          teacherIds: selectedTeacherIds,
          groupTag: targetScope === "DEPT_HEADS" ? "부장회의" : targetScope === "SUBJECT" ? `${subject}협의회` : "지정그룹",
          dayOfWeek: Number(batchDay),
          period: Number(batchPeriod),
          constraintType: batchType,
          reason: batchReason,
        }),
      });
      onNotify("일괄 고정 공강 등록 완료", `${res.count || 0}명의 교사 제약이 등록되었습니다.`);
      await loadConstraints();
      onRefresh?.();
    } catch (err) {
      onNotify("일괄 등록 실패", err.message, true);
    }
  };

  const handleDelete = async (id) => {
    try {
      await apiFetch(serverUrl, `/api/timetable/constraints?id=${encodeURIComponent(id)}`, user.id, {
        method: "DELETE",
      });
      onNotify("제약 삭제 완료", "선택한 교사 제약 조건이 해제되었습니다.");
      await loadConstraints();
      onRefresh?.();
    } catch (err) {
      onNotify("제약 삭제 실패", err.message, true);
    }
  };

  const filteredList = useMemo(
    () => (filterTeacherId === "ALL" ? constraints : constraints.filter((c) => c.teacherId === filterTeacherId)),
    [constraints, filterTeacherId]
  );

  const dayLabel = (d) => DAYS.find((item) => item.value === d)?.label || `${d}요일`;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <ShieldAlert size={16} /> 개별 교사 공강 · 기피 교시 설정
          </h3>
          <form onSubmit={handleSaveSingle} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="grid-2">
              <select className="select" value={teacherId} onChange={(e) => setTeacherId(e.target.value)}>
                {regularTeachers.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} ({t.subject || "교과"})
                  </option>
                ))}
              </select>
              <select className="select" value={constraintType} onChange={(e) => setConstraintType(e.target.value)}>
                <option value="HARD_AVOID">절대 금지 (Hard)</option>
                <option value="MEETING_FREE">회의 공강 (Hard)</option>
                <option value="SOFT_AVOID">기피 교시 (Soft)</option>
              </select>
            </div>
            <div className="grid-2">
              <select className="select" value={dayOfWeek} onChange={(e) => setDayOfWeek(Number(e.target.value))}>
                {DAYS.map((d) => (
                  <option key={d.value} value={d.value}>{d.label}</option>
                ))}
              </select>
              <select className="select" value={period} onChange={(e) => setPeriod(Number(e.target.value))}>
                {PERIODS.map((p) => (
                  <option key={p} value={p}>{p === 0 ? "종일 (1~7교시)" : `${p}교시`}</option>
                ))}
              </select>
            </div>
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="사유 (예: 연구년, 보건실, 1교시 희망 공강)" />
            <button type="submit" className="btn btn-primary" style={{ alignSelf: "flex-start" }}>
              <PlusCircle size={15} /> 제약 조건 저장
            </button>
          </form>
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <Users size={16} /> 부장회의 · 교과협의회 고정 공강 일괄 등록
          </h3>
          <form onSubmit={handleSaveBatch} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="grid-2">
              <select className="select" value={targetScope} onChange={(e) => setTargetScope(e.target.value)}>
                <option value="DEPT_HEADS">부장교사 전체 (부장회의)</option>
                <option value="SUBJECT">특정 교과 교사 전체 (교과협의회)</option>
                <option value="TEACHERS">선택한 교사 묶음</option>
              </select>
              <select className="select" value={batchType} onChange={(e) => setBatchType(e.target.value)}>
                <option value="MEETING_FREE">회의 공강 (절대 차단)</option>
                <option value="HARD_AVOID">절대 금지 (Hard)</option>
                <option value="SOFT_AVOID">기피 교시 (Soft)</option>
              </select>
            </div>
            {targetScope === "SUBJECT" && (
              <input className="input" value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="교과명 입력 (예: 영어, 수학, 과학)" />
            )}
            {targetScope === "TEACHERS" && (
              <div style={{ display: "flex", flexWrap: "wrap", gap: 6, maxHeight: 78, overflowY: "auto", padding: 6, border: "1px solid #e2e8f0", borderRadius: 6 }}>
                {regularTeachers.map((t) => {
                  const checked = selectedTeacherIds.includes(t.id);
                  return (
                    <label key={t.id} style={{ fontSize: 12, display: "flex", alignItems: "center", gap: 3 }}>
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) =>
                          setSelectedTeacherIds((prev) => (e.target.checked ? [...prev, t.id] : prev.filter((x) => x !== t.id)))
                        }
                      />
                      {t.name}
                    </label>
                  );
                })}
              </div>
            )}
            <div className="grid-2">
              <select className="select" value={batchDay} onChange={(e) => setBatchDay(Number(e.target.value))}>
                {DAYS.map((d) => (
                  <option key={d.value} value={d.value}>{d.label}</option>
                ))}
              </select>
              <select className="select" value={batchPeriod} onChange={(e) => setBatchPeriod(Number(e.target.value))}>
                {PERIODS.map((p) => (
                  <option key={p} value={p}>{p === 0 ? "종일 (1~7교시)" : `${p}교시`}</option>
                ))}
              </select>
            </div>
            <input className="input" value={batchReason} onChange={(e) => setBatchReason(e.target.value)} placeholder="회의·협의회 명칭" />
            <button type="submit" className="btn btn-green" style={{ alignSelf: "flex-start" }}>
              <Layers size={15} /> 일괄 고정 공강 적용
            </button>
          </form>
        </div>
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>등록된 교사별 제약 현황 ({filteredList.length}건)</h3>
          <select className="select" style={{ width: 180 }} value={filterTeacherId} onChange={(e) => setFilterTeacherId(e.target.value)}>
            <option value="ALL">전체 교사 보기</option>
            {regularTeachers.map((t) => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </select>
        </div>
        {filteredList.length === 0 ? (
          <div style={{ fontSize: 13, color: "#64748b" }}>등록된 고정 공강이나 기피 교시 제약이 없습니다.</div>
        ) : (
          <table className="table">
            <thead>
              <tr><th>교사</th><th>요일·교시</th><th>구분</th><th>그룹 태그</th><th>사유</th><th>관리</th></tr>
            </thead>
            <tbody>
              {filteredList.map((c) => {
                const meta = TYPE_META[c.constraintType] || TYPE_META.HARD_AVOID;
                return (
                  <tr key={c.id}>
                    <td><b>{c.teacherName || c.teacherId}</b></td>
                    <td>{dayLabel(c.dayOfWeek)} · {c.period === 0 ? "종일" : `${c.period}교시`}</td>
                    <td>
                      <span className="badge" style={{ background: meta.bg, color: meta.color }}>
                        {meta.label}
                      </span>
                    </td>
                    <td>{c.groupTag || "-"}</td>
                    <td>{c.reason}</td>
                    <td>
                      <button type="button" className="btn btn-sm" onClick={() => handleDelete(c.id)}>
                        <Trash2 size={13} /> 해제
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
