import React, { useState } from "react";
import { CheckCircle2, FileSpreadsheet, FileText, PlusCircle, Printer, XCircle } from "lucide-react";
import { apiFetch, hasRole, printDesktopWindow } from "../../events/eventStore";

const LINK_LABELS = {
  SIMPLE_NOTICE: "[단순안내]",
  CLASS_BLOCK: "[학년/전교 수업블록]",
  TEACHER_TRIP: "[특정교사 출장/결보강]",
  BELL_CHANGE: "[시종시간 변경]",
};

// CalendarView는 부서별 주중·월중 행사 취합, 연동 속성 지정, 행사 담당교사 최종 확정 및
// 한글(.hwpx)·엑셀(.xlsx)·고대비 B4/A3 인쇄 출력을 제공합니다.
export default function CalendarView({ serverUrl, user, events, teachers, onRefresh, onNotify }) {
  const [title, setTitle] = useState("");
  const [department, setDepartment] = useState("교무기획부");
  const [eventDate, setEventDate] = useState(new Date().toISOString().slice(0, 10));
  const [startPeriod, setStartPeriod] = useState(5);
  const [endPeriod, setEndPeriod] = useState(6);
  const [linkType, setLinkType] = useState("CLASS_BLOCK");
  const [grade, setGrade] = useState(1);
  const [leadTeacherId, setLeadTeacherId] = useState(user?.id || "t05");

  const isEventAdmin = hasRole(user, "EVENT_ADMIN");

  const handlePropose = async (e) => {
    e.preventDefault();
    if (!title.trim()) return;
    try {
      await apiFetch(serverUrl, "/api/calendar/events", user.id, {
        method: "POST",
        body: JSON.stringify({
          title,
          department,
          eventDate,
          startPeriod: Number(startPeriod),
          endPeriod: Number(endPeriod),
          targetGrades: [Number(grade)],
          leadTeachers: [leadTeacherId],
          linkType,
          description: `${department} 등록 행사`,
          proposedBy: user.name,
        }),
      });
      setTitle("");
      onNotify("부서 행사 등록(STAGING) 완료", "행사 담당교사가 최종 선택 시 시간표/시종시간에 연동됩니다.");
      onRefresh();
    } catch (err) {
      onNotify("행사 등록 실패", err.message, true);
    }
  };

  const handleCommit = async (eventId, approve) => {
    try {
      await apiFetch(serverUrl, "/api/calendar/commit", user.id, {
        method: "POST",
        body: JSON.stringify({ eventId, approve }),
      });
      onNotify(approve ? "행사 최종 확정 반영" : "행사 반려", approve ? "시간표 및 시종 연동이 완료되었습니다." : "행사 신청이 반려되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("처리 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 17 }}>주중·월중 행사 취합 및 학사일정 연동</h2>
            <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 3 }}>
              확정 시 연동 속성에 따라 수업 블록, 인솔교사 결보강 대기열 이관, 시종시간 변경이 자동 실행됩니다.
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <a className="btn btn-sm" href={`${serverUrl}/api/export/hwpx?kind=calendar`} target="_blank" rel="noreferrer">
              <FileText size={14} /> 한글 행사표 (.hwpx)
            </a>
            <a className="btn btn-sm" href={`${serverUrl}/api/export/xlsx?kind=calendar`} target="_blank" rel="noreferrer">
              <FileSpreadsheet size={14} /> 엑셀 일정 (.xlsx)
            </a>
            <a className="btn btn-sm" href={`${serverUrl}/api/export/print`} target="_blank" rel="noreferrer">
              <Printer size={14} /> B4/A3 고대비 현황판
            </a>
            <button className="btn btn-sm" onClick={printDesktopWindow}>
              <Printer size={14} /> 현재 화면 인쇄/PDF
            </button>
          </div>
        </div>
      </div>

      <div className="card no-print">
        <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>부서별 행사 입력 (대기열 등록)</h3>
        <form onSubmit={handlePropose} style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))", gap: 10 }}>
          <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="행사명 (예: 1학년 학교폭력예방교육)" required />
          <input className="input" value={department} onChange={(e) => setDepartment(e.target.value)} placeholder="주관 부서" />
          <input className="input" type="date" value={eventDate} onChange={(e) => setEventDate(e.target.value)} />
          <select className="select" value={linkType} onChange={(e) => setLinkType(e.target.value)}>
            {Object.entries(LINK_LABELS).map(([k, v]) => (
              <option key={k} value={k}>{v}</option>
            ))}
          </select>
          <div style={{ display: "flex", gap: 6 }}>
            <input className="input" type="number" min={0} max={7} value={startPeriod} onChange={(e) => setStartPeriod(e.target.value)} title="시작 교시" />
            <input className="input" type="number" min={0} max={7} value={endPeriod} onChange={(e) => setEndPeriod(e.target.value)} title="종료 교시" />
          </div>
          <select className="select" value={grade} onChange={(e) => setGrade(e.target.value)}>
            <option value={1}>1학년</option>
            <option value={2}>2학년</option>
            <option value={3}>3학년</option>
          </select>
          <select className="select" value={leadTeacherId} onChange={(e) => setLeadTeacherId(e.target.value)}>
            {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => (
              <option key={t.id} value={t.id}>인솔: {t.name}</option>
            ))}
          </select>
          <button type="submit" className="btn btn-primary">
            <PlusCircle size={15} /> 행사 등록 요청
          </button>
        </form>
      </div>

      <div className="card">
        <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>취합 및 확정 학사일정 목록</h3>
        <table className="table">
          <thead>
            <tr><th>일자/교시</th><th>부서</th><th>행사명</th><th>연동 속성</th><th>상태</th>{isEventAdmin && <th className="no-print">최종 선택</th>}</tr>
          </thead>
          <tbody>
            {(events || []).map((ev) => (
              <tr key={ev.id}>
                <td>{ev.eventDate} ({ev.startPeriod}~{ev.endPeriod}교시)</td>
                <td>{ev.department}</td>
                <td><b>{ev.title}</b></td>
                <td><span className="badge badge-blue">{LINK_LABELS[ev.linkType] || ev.linkType}</span></td>
                <td>
                  <span className={`badge ${ev.status === "APPROVED" ? "badge-green" : ev.status === "REJECTED" ? "badge-red" : "badge-amber"}`}>
                    {ev.status === "APPROVED" ? "확정 반영" : ev.status === "REJECTED" ? "반려" : "승인 대기"}
                  </span>
                </td>
                {isEventAdmin && (
                  <td className="no-print">
                    {ev.status === "STAGING" && (
                      <div style={{ display: "flex", gap: 6 }}>
                        <button className="btn btn-sm btn-green" onClick={() => handleCommit(ev.id, true)}>
                          <CheckCircle2 size={13} /> 최종 선택
                        </button>
                        <button className="btn btn-sm btn-danger" onClick={() => handleCommit(ev.id, false)}>
                          <XCircle size={13} /> 반려
                        </button>
                      </div>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
