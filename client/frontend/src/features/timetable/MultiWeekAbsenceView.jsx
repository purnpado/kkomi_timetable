import React, { useEffect, useMemo, useState } from "react";
import { AlertTriangle, ArrowLeftRight, CalendarRange, CheckCircle2, Send, Sparkles } from "lucide-react";
import { apiFetch, hasRegularClasses, hasRole, isFacultyTeacher } from "../../events/eventStore";

const DAYS = ["", "월", "화", "수", "목", "금"];
const ABSENCE_REASONS = ["갑작스러운 병가", "긴급 경조사", "공무·긴급 출장", "개인 연가·조퇴"];

export function getMondayISO(dateStr) {
  const base = dateStr ? new Date(`${dateStr}T00:00:00`) : new Date();
  const d = Number.isNaN(base.getTime()) ? new Date() : base;
  const day = d.getDay() === 0 ? 7 : d.getDay();
  d.setDate(d.getDate() - (day - 1));
  return formatLocalISO(d);
}

export function addDaysISO(dateStr, days) {
  const base = dateStr ? new Date(`${dateStr}T00:00:00`) : new Date();
  const d = Number.isNaN(base.getTime()) ? new Date() : base;
  d.setDate(d.getDate() + days);
  return formatLocalISO(d);
}

function formatLocalISO(d) {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function defaultWeekdayISO() {
  const now = new Date();
  const wd = now.getDay();
  if (wd === 0) return addDaysISO(formatLocalISO(now), 1);
  if (wd === 6) return addDaysISO(formatLocalISO(now), 2);
  return formatLocalISO(now);
}

// MultiWeekAbsenceView는 [주중 갑작스러운 미출근·결근 원클릭 대책]과
// [이번 주 ↔ 다음 주·다다음 주 주차 간 수업 맞교환 탐색] 기능을 제공합니다.
export default function MultiWeekAbsenceView({ serverUrl, user, teachers, baseCells, activeWeekStart, onRefresh, onNotify }) {
  const isAdmin = hasRole(user, "TIMETABLE_ADMIN");
  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const regularTeachers = useMemo(() => {
    const list = facultyTeachers.filter(hasRegularClasses);
    return list.length > 0 ? list : facultyTeachers;
  }, [facultyTeachers]);

  // 1. 주중 미출근/결근 대책 상태
  const [absentTeacherId, setAbsentTeacherId] = useState("");
  const [startDate, setStartDate] = useState(() => defaultWeekdayISO());

  useEffect(() => {
    if (regularTeachers.length === 0) {
      if (absentTeacherId) setAbsentTeacherId("");
      return;
    }
    if (!regularTeachers.some((t) => t.id === absentTeacherId)) {
      const preferred = regularTeachers.find((t) => t.id === user?.id) || regularTeachers[0];
      setAbsentTeacherId(preferred.id);
    }
  }, [absentTeacherId, regularTeachers, user?.id]);
  const [endDate, setEndDate] = useState(() => defaultWeekdayISO());
  const [startPeriod, setStartPeriod] = useState(1);
  const [endPeriod, setEndPeriod] = useState(7);
  const [absenceReason, setAbsenceReason] = useState("갑작스러운 병가");
  const [plans, setPlans] = useState([]);
  const [choices, setChoices] = useState({});
  const [loadingPlan, setLoadingPlan] = useState(false);

  // 2. 개별 주차 간(다음주·다다음주) 맞교환 탐색 상태
  const [cwSourceId, setCwSourceId] = useState(baseCells?.[0]?.id || "");
  const [cwSourceDate, setCwSourceDate] = useState(() => activeWeekStart || getMondayISO());
  const [cwOffsetWeeks, setCwOffsetWeeks] = useState(1);
  const [cwCandidates, setCwCandidates] = useState([]);
  const [loadingCW, setLoadingCW] = useState(false);

  const selectedSourceCell = useMemo(() => (baseCells || []).find((c) => c.id === cwSourceId) || baseCells?.[0], [baseCells, cwSourceId]);

  const effectiveSourceDate = useMemo(() => {
    if (!selectedSourceCell) return cwSourceDate;
    const mon = getMondayISO(cwSourceDate);
    return addDaysISO(mon, (selectedSourceCell.dayOfWeek || 1) - 1);
  }, [cwSourceDate, selectedSourceCell]);

  const handleFetchAbsencePlan = async (e) => {
    e.preventDefault();
    setLoadingPlan(true);
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/absence-plan", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacherId: absentTeacherId,
          startDate,
          endDate: endDate || startDate,
          startPeriod: Number(startPeriod),
          endPeriod: Number(endPeriod),
          reason: absenceReason,
        }),
      });
      const list = res || [];
      setPlans(list);
      const initChoices = {};
      list.forEach((slot) => {
        const key = `${slot.lessonDate}_${slot.cell.id}`;
        if (slot.crossWeekOptions && slot.crossWeekOptions.length > 0) {
          initChoices[key] = { mode: "CROSS_WEEK_SWAP", crossIdx: 0, subIdx: 0 };
        } else if (slot.substituteOptions && slot.substituteOptions.length > 0) {
          initChoices[key] = { mode: "SUBSTITUTE", crossIdx: 0, subIdx: 0 };
        } else {
          initChoices[key] = { mode: "SKIP", crossIdx: 0, subIdx: 0 };
        }
      });
      setChoices(initChoices);
      if (list.length === 0) {
        onNotify("수업 없음", "선택한 미출근 기간·교시에 해당하는 수업이 없습니다.");
      }
    } catch (err) {
      onNotify("대책 조회 실패", err.message, true);
    } finally {
      setLoadingPlan(false);
    }
  };

  const handleApplyAbsence = async (autoApprove) => {
    const items = [];
    plans.forEach((slot) => {
      const key = `${slot.lessonDate}_${slot.cell.id}`;
      const ch = choices[key];
      if (!ch || ch.mode === "SKIP") return;
      if (ch.mode === "CROSS_WEEK_SWAP" && slot.crossWeekOptions?.[ch.crossIdx]) {
        const opt = slot.crossWeekOptions[ch.crossIdx];
        const partner = (teachers || []).find((t) => t.id === opt.targetCell.teacherId);
        items.push({
          mode: "CROSS_WEEK_SWAP",
          sourceDate: slot.lessonDate,
          targetDate: opt.targetDate,
          sourceCell: slot.cell,
          targetCell: opt.targetCell,
          partnerId: opt.targetCell.teacherId,
          partnerName: partner?.name || opt.targetCell.teacherId,
          reason: `[긴급미출근·주차교환:${absenceReason}] ${opt.reason}`,
        });
      } else if (ch.mode === "SUBSTITUTE" && slot.substituteOptions?.[ch.subIdx]) {
        const sub = slot.substituteOptions[ch.subIdx];
        items.push({
          mode: "SUBSTITUTE",
          sourceDate: slot.lessonDate,
          targetDate: slot.lessonDate,
          sourceCell: slot.cell,
          targetCell: slot.cell,
          partnerId: sub.teacherId,
          partnerName: sub.teacherName,
          reason: `[긴급미출근·공강보강:${absenceReason}] ${sub.teacherName} ${sub.priorityBadge}`,
        });
      }
    });
    if (items.length === 0) {
      onNotify("선택 항목 확인", "적용할 교시별 대책을 1건 이상 선택해 주세요.", true);
      return;
    }
    const absTeacher = regularTeachers.find((t) => t.id === absentTeacherId);
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/absence-apply", user.id, {
        method: "POST",
        body: JSON.stringify({
          requesterId: absentTeacherId,
          requesterName: absTeacher?.name || user.name,
          items,
          autoApprove,
        }),
      });
      onNotify(
        res.autoApproved ? "미출근 대책 즉시 반영 완료" : "미출근 대책 대기열 등록 완료",
        `총 ${res.appliedCount}교시의 대책이 ${res.autoApproved ? "해당 주차 시간표에 즉시 적용" : "승인 대기열에 일괄 등록"}되었습니다.`
      );
      setPlans([]);
      onRefresh();
    } catch (err) {
      onNotify("일괄 적용 실패", err.message, true);
    }
  };

  const handleSearchCrossWeek = async (e) => {
    e.preventDefault();
    if (!selectedSourceCell) return;
    setLoadingCW(true);
    try {
      const srcMon = getMondayISO(effectiveSourceDate);
      const tgtMon = addDaysISO(srcMon, Number(cwOffsetWeeks) * 7);
      const res = await apiFetch(serverUrl, "/api/timetable/cross-week-candidates", user.id, {
        method: "POST",
        body: JSON.stringify({
          sourceCellId: selectedSourceCell.id,
          sourceDate: effectiveSourceDate,
          targetWeekStart: tgtMon,
        }),
      });
      setCwCandidates(res || []);
      if (!res || res.length === 0) {
        onNotify("후보 없음", "선택한 대상 주차에 양방향 충돌 없이 맞교환 가능한 수업이 없습니다.");
      }
    } catch (err) {
      onNotify("주차 간 교환 탐색 실패", err.message, true);
    } finally {
      setLoadingCW(false);
    }
  };

  const handleSubmitCrossWeek = async (cand) => {
    if (!selectedSourceCell) return;
    const srcTeacher = facultyTeachers.find((t) => t.id === selectedSourceCell.teacherId);
    const tgtTeacher = facultyTeachers.find((t) => t.id === cand.targetCell.teacherId);
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: "CROSS_WEEK_SWAP",
          scope: "DATE_ONLY",
          sourceDate: effectiveSourceDate,
          targetDate: cand.targetDate,
          requesterId: selectedSourceCell.teacherId,
          requesterName: srcTeacher?.name || user.name,
          partnerId: cand.targetCell.teacherId,
          partnerName: tgtTeacher?.name || cand.targetCell.teacherId,
          partnerAgreed: true,
          sourceCell: selectedSourceCell,
          targetCell: cand.targetCell,
          chainSteps: cand.chainSteps,
          reason: `[주차간교환(+${cwOffsetWeeks}주)] ${cand.reason}`,
        }),
      });
      onNotify("주차 간 교환 대기열 등록", "담당 교사가 최종 반영하면 해당 두 주차 시간표에만 정확히 교차 반영됩니다.");
      onRefresh();
    } catch (err) {
      onNotify("신청 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card" style={{ borderLeft: "4px solid #f59f00" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 16 }}>
            <AlertTriangle size={16} style={{ verticalAlign: "middle", color: "#f08c00" }} /> 주중 갑작스러운 미출근 · 긴급 결근 일괄 대책
          </h3>
          <span className="badge badge-amber">다음주·다다음주 맞교환 또는 당일 공강교사 자동 추천</span>
        </div>

        <form onSubmit={handleFetchAbsencePlan} style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(135px, 1fr))", gap: 8, marginBottom: 12 }}>
          <select className="select" value={absentTeacherId} onChange={(e) => setAbsentTeacherId(e.target.value)} disabled={!isAdmin}>
            {regularTeachers.map((t) => (
              <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>
            ))}
          </select>
          <input className="input" type="date" value={startDate} onChange={(e) => { setStartDate(e.target.value); if (endDate < e.target.value) setEndDate(e.target.value); }} title="미출근 시작일" />
          <input className="input" type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} title="미출근 종료일" />
          <div style={{ display: "flex", gap: 4, alignItems: "center" }}>
            <select className="select" value={startPeriod} onChange={(e) => setStartPeriod(e.target.value)}>
              {[1, 2, 3, 4, 5, 6, 7].map((p) => <option key={p} value={p}>{p}교시부터</option>)}
            </select>
            <select className="select" value={endPeriod} onChange={(e) => setEndPeriod(e.target.value)}>
              {[1, 2, 3, 4, 5, 6, 7].map((p) => <option key={p} value={p}>{p}교시까지</option>)}
            </select>
          </div>
          <select className="select" value={absenceReason} onChange={(e) => setAbsenceReason(e.target.value)}>
            {ABSENCE_REASONS.map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
          <button type="submit" className="btn btn-primary" disabled={loadingPlan}>
            <Sparkles size={14} /> {loadingPlan ? "분석 중..." : "결근 교시 해결책 조회"}
          </button>
        </form>

        {plans.length > 0 && (
          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <table className="table">
              <thead>
                <tr><th>결근 일자 · 교시</th><th>대상 수업</th><th>대책 방식</th><th>추천 교사 / 다음주·다다음주 교환 수업 선택</th></tr>
              </thead>
              <tbody>
                {plans.map((slot) => {
                  const key = `${slot.lessonDate}_${slot.cell.id}`;
                  const ch = choices[key] || { mode: "SKIP", crossIdx: 0, subIdx: 0 };
                  return (
                    <tr key={key}>
                      <td><b>{slot.lessonDate}</b> ({DAYS[slot.cell.dayOfWeek]} {slot.cell.period}교시)</td>
                      <td><b>{slot.cell.className}</b>반 {slot.cell.subject}</td>
                      <td>
                        <select
                          className="select"
                          value={ch.mode}
                          onChange={(e) => setChoices({ ...choices, [key]: { ...ch, mode: e.target.value } })}
                        >
                          {slot.crossWeekOptions?.length > 0 && <option value="CROSS_WEEK_SWAP">다음주/다다음주 맞교환 (무결손)</option>}
                          {slot.substituteOptions?.length > 0 && <option value="SUBSTITUTE">당일 공강교사 대타 보강</option>}
                          <option value="SKIP">보류 / 직접 처리</option>
                        </select>
                      </td>
                      <td>
                        {ch.mode === "CROSS_WEEK_SWAP" && (
                          <select
                            className="select"
                            value={ch.crossIdx}
                            onChange={(e) => setChoices({ ...choices, [key]: { ...ch, crossIdx: Number(e.target.value) } })}
                          >
                            {(slot.crossWeekOptions || []).map((opt, idx) => (
                              <option key={idx} value={idx}>{opt.reason}</option>
                            ))}
                          </select>
                        )}
                        {ch.mode === "SUBSTITUTE" && (
                          <select
                            className="select"
                            value={ch.subIdx}
                            onChange={(e) => setChoices({ ...choices, [key]: { ...ch, subIdx: Number(e.target.value) } })}
                          >
                            {(slot.substituteOptions || []).map((sub, idx) => (
                              <option key={sub.teacherId} value={idx}>
                                {sub.teacherName} ({sub.subject}) {sub.priorityBadge}
                              </option>
                            ))}
                          </select>
                        )}
                        {ch.mode === "SKIP" && <span style={{ color: "#64748b", fontSize: 12 }}>적용 안 함</span>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>

            <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", flexWrap: "wrap" }}>
              <button type="button" className="btn btn-primary" onClick={() => handleApplyAbsence(false)}>
                <Send size={14} /> 선택 대책 일괄 대기열(STAGING) 등록
              </button>
              {isAdmin && (
                <button type="button" className="btn btn-green" onClick={() => handleApplyAbsence(true)}>
                  <CheckCircle2 size={14} /> 담당자 권한: 즉시 일괄 확정 반영
                </button>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15 }}>
            <CalendarRange size={15} style={{ verticalAlign: "middle", color: "#3b5bdb" }} /> 이번 주 ↔ 다음 주 · 다다음 주 주차 간 수업 맞교환
          </h3>
          <span className="badge badge-blue">기본 정규 시간표 유지 · 해당 2주만 교차 적용</span>
        </div>

        <form onSubmit={handleSearchCrossWeek} style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginBottom: 12 }}>
          <select className="select" style={{ flex: 2, minWidth: 210 }} value={cwSourceId} onChange={(e) => setCwSourceId(e.target.value)}>
            {(baseCells || []).map((c) => {
              const tName = facultyTeachers.find((t) => t.id === c.teacherId)?.name || c.teacherId;
              return (
                <option key={c.id} value={c.id}>
                  {c.className}반 {DAYS[c.dayOfWeek]} {c.period}교시 · {c.subject} ({tName})
                </option>
              );
            })}
          </select>
          <input className="input" style={{ width: 150 }} type="date" value={cwSourceDate} onChange={(e) => setCwSourceDate(e.target.value)} title="기준 주간 날짜" />
          <select className="select" style={{ width: 155 }} value={cwOffsetWeeks} onChange={(e) => setCwOffsetWeeks(Number(e.target.value))}>
            <option value={1}>다음 주 (+1주)</option>
            <option value={2}>다다음 주 (+2주)</option>
            <option value={3}>3주 후 (+3주)</option>
            <option value={4}>4주 후 (+4주)</option>
          </select>
          <button type="submit" className="btn btn-primary" disabled={loadingCW}>
            <ArrowLeftRight size={14} /> {loadingCW ? "탐색 중..." : `맞교환 탐색 (${effectiveSourceDate})`}
          </button>
        </form>

        {cwCandidates.length > 0 && (
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(310px, 1fr))", gap: 10 }}>
            {cwCandidates.map((cand, idx) => (
              <div key={idx} style={{ padding: 10, borderRadius: 10, background: "#f8fafc", border: "1px solid #cbd5e1", display: "flex", flexDirection: "column", gap: 6 }}>
                <div style={{ fontWeight: 700, fontSize: 13, color: "#1e3a8a" }}>{cand.reason}</div>
                {(cand.chainSteps || []).map((st) => (
                  <div key={st.step} style={{ fontSize: 12, color: "#334155" }}>{st.description}</div>
                ))}
                <button type="button" className="btn btn-sm btn-green" style={{ alignSelf: "flex-start", marginTop: 4 }} onClick={() => handleSubmitCrossWeek(cand)}>
                  이 주차 간 맞교환 신청
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
