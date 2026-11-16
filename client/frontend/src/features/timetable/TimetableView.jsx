import React, { useMemo, useState } from "react";
import { CheckSquare, Copy, FileSpreadsheet, FileText, PlusCircle, TableProperties, Users, Wrench } from "lucide-react";
import TimetableCanvas from "../../components/canvas/TimetableCanvas";
import { apiFetch, hasRole } from "../../events/eventStore";
import BaseTimetableBuilder from "./BaseTimetableBuilder";
import TeacherManagerCard from "../admin-remote/TeacherManagerCard";

// TimetableView는 60fps HTML5 Canvas 시간표 조정, 교사 간 수업교환 신청,
// 마스터/담당교사의 학년·학급별 기초 시간표 작성 및 나이스(NEIS) 입력 도우미를 제공합니다.
export default function TimetableView({ serverUrl, user, cells, teachers, requests, onRefresh, onOpenApproval, onNotify }) {
  const [subTab, setSubTab] = useState("board"); // "board" | "builder" | "teachers"
  const [filterClass, setFilterClass] = useState("2-1");
  const [reason, setReason] = useState("학년 협의 및 수업 교환");
  const [reqType, setReqType] = useState("SWAP");
  const [sourceId, setSourceId] = useState(cells[0]?.id || "");
  const [partnerId, setPartnerId] = useState("t03");

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
        }),
      });
    } catch {
      return { signal: "RED", reason: "서버 판정 연결 오류" };
    }
  };

  const handleRequestFromCanvas = async (srcCell, tgtCell, targetDay, targetPeriod, evalRes) => {
    try {
      const payload = {
        requestType: tgtCell ? "SWAP" : "SUBSTITUTE",
        requesterId: user.id,
        requesterName: teachersMap[srcCell.teacherId]?.name || user.name,
        partnerId: tgtCell ? tgtCell.teacherId : srcCell.teacherId,
        partnerName: tgtCell ? teachersMap[tgtCell.teacherId]?.name || tgtCell.teacherId : "빈 교시 이동",
        partnerAgreed: true,
        sourceCell: srcCell,
        targetCell: tgtCell || { ...srcCell, dayOfWeek: targetDay, period: targetPeriod },
        reason: `[Canvas 조정: ${evalRes.mode}] ${evalRes.reason}`,
      };
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify(payload),
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
    if (!src) return;
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

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {isAdmin && (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <button className={`btn ${subTab === "board" ? "btn-primary" : ""}`} onClick={() => setSubTab("board")}>
            <TableProperties size={15} /> 주간 시간표 & 일일 결보강 조정
          </button>
          <button className={`btn ${subTab === "builder" ? "btn-primary" : ""}`} onClick={() => setSubTab("builder")}>
            <Wrench size={15} /> 학년별·학급별 기초 시간표 작성
          </button>
          <button className={`btn ${subTab === "teachers" ? "btn-primary" : ""}`} onClick={() => setSubTab("teachers")}>
            <Users size={15} /> 교사 기초 명렬표 관리
          </button>
        </div>
      )}

      {isAdmin && subTab === "builder" ? (
        <BaseTimetableBuilder serverUrl={serverUrl} user={user} cells={cells} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />
      ) : isAdmin && subTab === "teachers" ? (
        <TeacherManagerCard serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />
      ) : (
        <>
          <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <h2 style={{ margin: 0, fontSize: 17 }}>정규·일일 스마트 시간표 보드 (Canvas)</h2>
            <select className="select" style={{ width: 150 }} value={filterClass} onChange={(e) => setFilterClass(e.target.value)}>
              {classList.map((c) => (
                <option key={c} value={c}>{c === "ALL" ? "전체 학급 보기" : `${c}반 시간표`}</option>
              ))}
            </select>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            {isAdmin && (
              <button className="btn btn-primary btn-sm" onClick={onOpenApproval}>
                <CheckSquare size={14} /> 대기열 비교 승인 ({(requests || []).filter((r) => r.status === "STAGING").length}건)
              </button>
            )}
            <a className="btn btn-sm" href={`${serverUrl}/api/export/hwpx?kind=timetable`} target="_blank" rel="noreferrer">
              <FileText size={14} /> 결보강 대장 (.hwpx)
            </a>
            <a className="btn btn-sm" href={`${serverUrl}/api/export/xlsx?kind=timetable`} target="_blank" rel="noreferrer">
              <FileSpreadsheet size={14} /> 나이스 엑셀 (.xlsx)
            </a>
          </div>
        </div>

        <TimetableCanvas
          cells={cells}
          teachersMap={teachersMap}
          filterClass={filterClass}
          onEvaluateDrop={handleEvaluateDrop}
          onRequestCommit={handleRequestFromCanvas}
        />
      </div>

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
                {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => (
                  <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>
                ))}
              </select>
              <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="신청 사유" />
            </div>
            <button type="submit" className="btn btn-primary" style={{ alignSelf: "flex-start" }}>
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
