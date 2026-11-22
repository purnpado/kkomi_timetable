import React, { useState } from "react";
import { Check, Copy, Handshake, PlusCircle } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

// TimetableRequestAndNEISPanel은 일회성/정규 구분 수동 신청, 상대교사 사전 합의 동의, 나이스(NEIS) 입력 도우미를 제공합니다.
export default function TimetableRequestAndNEISPanel({
  serverUrl,
  user,
  isAdmin,
  cells,
  requests,
  approvedList,
  teachersMap,
  partnerTeachers,
  reqType,
  setReqType,
  sourceId,
  setSourceId,
  partnerId,
  setPartnerId,
  reason,
  setReason,
  changeScope,
  setChangeScope,
  selectedWeekStart,
  onRefresh,
  onNotify,
}) {
  const [partnerAgreed, setPartnerAgreed] = useState(true);
  const [agreementNote, setAgreementNote] = useState("");
  const [instantApply, setInstantApply] = useState(false);
  const [targetDate, setTargetDate] = useState(() => selectedWeekStart || new Date().toISOString().slice(0, 10));

  const effectiveTargetDate = changeScope === "DATE_ONLY" ? targetDate || selectedWeekStart || new Date().toISOString().slice(0, 10) : "";

  const handleManualSubmit = async (e) => {
    e.preventDefault();
    const src = (cells || []).find((c) => c.id === sourceId) || cells?.[0];
    if (!src || !partnerId) return;
    try {
      await apiFetch(serverUrl, "/api/timetable/requests", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestType: reqType,
          scope: changeScope,
          sourceDate: effectiveTargetDate,
          targetDate: effectiveTargetDate,
          requesterId: user.id,
          requesterName: user.name,
          partnerId,
          partnerName: teachersMap[partnerId]?.name || partnerId,
          partnerAgreed,
          agreementNote: agreementNote.trim(),
          autoApprove: Boolean(isAdmin && instantApply),
          sourceCell: src,
          targetCell: { ...src, teacherId: partnerId },
          reason,
        }),
      });
      onNotify(
        isAdmin && instantApply ? "시간표 즉시 반영 완료" : "수업 변경 신청 완료",
        isAdmin && instantApply ? "일과계 즉시 반영으로 처리되었습니다." : "담당교사의 승인 대기열로 전달되었습니다."
      );
      setAgreementNote("");
      onRefresh();
    } catch (err) {
      onNotify("신청 실패", err.message, true);
    }
  };

  const handleAgreeToggle = async (reqItem, agreedVal) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/requests/agree", user.id, {
        method: "POST",
        body: JSON.stringify({
          requestId: reqItem.id,
          agreed: agreedVal,
          note: `${user.name} ${agreedVal ? "사전 합의 동의" : "동의 보류"}`,
        }),
      });
      onNotify("사전 합의 상태 변경", agreedVal ? "상대 교사 합의 동의가 기록되었습니다." : "합의 동의가 취소되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("합의 처리 실패", err.message, true);
    }
  };

  const stagingToConfirm = (requests || []).filter(
    (r) => r.status === "STAGING" && (isAdmin || r.partnerId === user.id || r.requesterId === user.id)
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>수업 교환 / 결보강 신청 (사전 합의 · 일회성/정규 구분)</h3>
          <form onSubmit={handleManualSubmit} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", fontSize: 12 }}>
              <label style={{ display: "inline-flex", alignItems: "center", gap: 4, fontWeight: 700, color: "#1d4ed8" }}>
                <input type="radio" checked={changeScope === "DATE_ONLY"} onChange={() => setChangeScope("DATE_ONLY")} />
                일회성 (해당 주·일자만)
              </label>
              <label style={{ display: "inline-flex", alignItems: "center", gap: 4, fontWeight: 700, color: "#b91c1c" }}>
                <input type="radio" checked={changeScope === ""} onChange={() => setChangeScope("")} />
                정규 시간표 영구 수정
              </label>
              {changeScope === "DATE_ONLY" && (
                <input className="input" type="date" style={{ width: 145 }} value={targetDate} onChange={(e) => setTargetDate(e.target.value)} />
              )}
            </div>

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
                  partnerTeachers.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>)
                )}
              </select>
              <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="신청 사유" />
            </div>

            <div className="grid-2">
              <label style={{ fontSize: 12, display: "inline-flex", alignItems: "center", gap: 5 }}>
                <input type="checkbox" checked={partnerAgreed} onChange={(e) => setPartnerAgreed(e.target.checked)} />
                상대 교사 사전 합의 완료 보고
              </label>
              <input className="input" value={agreementNote} onChange={(e) => setAgreementNote(e.target.value)} placeholder="합의 메모 (예: 쉬는 시간 구두 합의)" />
            </div>

            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
              <button type="submit" className="btn btn-primary" disabled={!cells?.length}>
                <PlusCircle size={15} /> {isAdmin && instantApply ? "즉시 반영 등록" : "대기열 등록 요청"}
              </button>
              {isAdmin && (
                <label style={{ fontSize: 12, fontWeight: 700, color: "#087f5b", display: "inline-flex", alignItems: "center", gap: 5 }}>
                  <input type="checkbox" checked={instantApply} onChange={(e) => setInstantApply(e.target.checked)} />
                  일과계 즉시 반영 (승인 대기 생략)
                </label>
              )}
            </div>
          </form>
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>나이스(NEIS) 입력 도우미 (확정 순 정렬)</h3>
          {approvedList.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>확정된 수업 변경 내역이 없습니다.</div>
          ) : (
            <div style={{ maxHeight: 240, overflowY: "auto" }}>
              <table className="table">
                <thead>
                  <tr><th>구분/일자</th><th>교시</th><th>과목코드</th><th>변경 내역</th></tr>
                </thead>
                <tbody>
                  {approvedList.map((r) => (
                    <tr key={r.id}>
                      <td>
                        <span className={`badge ${r.scope === "DATE_ONLY" || r.requestType === "CROSS_WEEK_SWAP" ? "badge-blue" : "badge-amber"}`} style={{ marginRight: 4 }}>
                          {r.scope === "DATE_ONLY" || r.requestType === "CROSS_WEEK_SWAP" ? "일회성" : "정규"}
                        </span>
                        {r.targetDate} · {r.sourceCell?.className}
                      </td>
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
            </div>
          )}
        </div>
      </div>

      {stagingToConfirm.length > 0 && (
        <div className="card">
          <h3 style={{ margin: "0 0 8px", fontSize: 14.5, display: "flex", alignItems: "center", gap: 6 }}>
            <Handshake size={15} color="#3b5bdb" /> 대기 중인 수업 변경 · 상대교사 사전 합의 확인 ({stagingToConfirm.length}건)
          </h3>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {stagingToConfirm.map((r) => (
              <div key={r.id} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, padding: "6px 10px", background: "#f8fafc", borderRadius: 8 }}>
                <div style={{ fontSize: 12.5 }}>
                  <span className={`badge ${r.scope === "DATE_ONLY" ? "badge-blue" : "badge-amber"}`} style={{ marginRight: 6 }}>
                    {r.scope === "DATE_ONLY" ? `일회성(${r.targetDate})` : "정규"}
                  </span>
                  <b>{r.requesterName}</b> ↔ <b>{r.partnerName}</b> ({r.sourceCell?.className} {r.sourceCell?.period}교시 {r.sourceCell?.subject})
                  <span style={{ color: "#64748b", marginLeft: 6 }}>{r.agreementNote || r.reason}</span>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                  <span className={`badge ${r.partnerAgreed ? "badge-green" : "badge-red"}`}>
                    {r.partnerAgreed ? "상대 합의 완료" : "상대 동의 필요"}
                  </span>
                  {(isAdmin || r.partnerId === user.id) && (
                    <button type="button" className="btn btn-sm" onClick={() => handleAgreeToggle(r, !r.partnerAgreed)}>
                      <Check size={12} /> {r.partnerAgreed ? "합의 취소" : "동의 보고"}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
