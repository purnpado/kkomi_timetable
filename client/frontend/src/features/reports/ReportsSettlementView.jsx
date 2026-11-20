import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { BarChart3, FileSpreadsheet, FileText, Printer, RefreshCw } from "lucide-react";
import { apiFetch } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";

const isSystemAdmin = (t) =>
  t.username === "admin" && (t.subject === "시스템" || t.name === "최고관리자" || t.name === "시스템 관리자");

// ReportsSettlementView는 월별·학기별·학년도별 결보강, 수업교환, 내외부 시험감독 및 행사 실적을
// 교원별로 통합 집계하고 공문 결재용 한글(.hwpx)·엑셀(.xlsx)·고대비 인쇄물을 생성합니다.
export default function ReportsSettlementView({ serverUrl, user, teachers, onNotify }) {
  const now = new Date();
  const [mode, setMode] = useState("ALL"); // MONTH | SEMESTER_1 | SEMESTER_2 | YEAR | ALL | CUSTOM
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth() + 1);
  const [startDate, setStartDate] = useState(new Date(now.getFullYear(), now.getMonth(), 1).toISOString().slice(0, 10));
  const [endDate, setEndDate] = useState(now.toISOString().slice(0, 10));
  const [certTeacherId, setCertTeacherId] = useState("");
  const [report, setReport] = useState(null);

  const onNotifyRef = useRef(onNotify);
  onNotifyRef.current = onNotify;

  const queryParams = useMemo(() => {
    const p = new URLSearchParams({ mode, year: String(year), month: String(month) });
    if (mode === "CUSTOM") {
      if (startDate) p.set("startDate", startDate);
      if (endDate) p.set("endDate", endDate);
    }
    return p.toString();
  }, [mode, year, month, startDate, endDate]);

  const loadReport = useCallback(async () => {
    try {
      const data = await apiFetch(serverUrl, `/api/stats/summary?${queryParams}`, user.id);
      setReport(data);
    } catch (err) {
      onNotifyRef.current?.("통합 통계 조회 실패", err.message, true);
    }
  }, [serverUrl, user?.id, queryParams]);

  useEffect(() => {
    loadReport();
  }, [loadReport]);

  const regularTeachers = useMemo(
    () => (teachers || []).filter((t) => !t.isParentHonorary && !isSystemAdmin(t)),
    [teachers]
  );

  const handleDownload = (path, fileName, options) => {
    downloadAuthenticatedFile(serverUrl, path, fileName, options).catch((err) =>
      onNotifyRef.current?.("보고서 출력 실패", err.message, true)
    );
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 17, display: "flex", alignItems: "center", gap: 6 }}>
              <BarChart3 size={18} color="#3b5bdb" /> 월별 · 학기별 교무업무 통합 정산 및 공문 출력
            </h2>
            <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 3 }}>
              {report ? `${report.periodLabel} (${report.generatedAt} 집계 · 계좌/금액 제외 실무 시수 기준)` : "집계 중..."}
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <button type="button" className="btn btn-sm btn-green" onClick={() => handleDownload(`/api/export/xlsx?kind=settlement&${queryParams}`, "교무업무_통합정산표.xlsx")}>
              <FileSpreadsheet size={14} /> 통합 정산표 (.xlsx)
            </button>
            <button type="button" className="btn btn-sm btn-primary" onClick={() => handleDownload(`/api/export/hwpx?kind=settlement&${queryParams}`, "교무업무_통합정산_보고서.hwpx")}>
              <FileText size={14} /> 정산 보고서 (.hwpx)
            </button>
            <button type="button" className="btn btn-sm" onClick={() => handleDownload(`/api/export/print?kind=settlement&${queryParams}`, "통합정산_인쇄판.html", { openInTab: true })}>
              <Printer size={14} /> 고대비 인쇄판
            </button>
          </div>
        </div>

        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginTop: 12, paddingTop: 12, borderTop: "1px solid #e2e8f0" }}>
          <select className="select" style={{ width: 150 }} value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="ALL">전체 누적 기간</option>
            <option value="MONTH">월별 정산</option>
            <option value="SEMESTER_1">1학기 (3~8월)</option>
            <option value="SEMESTER_2">2학기 (9~익년 2월)</option>
            <option value="YEAR">학년도 전체</option>
            <option value="CUSTOM">기간 직접 지정</option>
          </select>
          {mode !== "ALL" && mode !== "CUSTOM" && (
            <input className="input" type="number" min={2020} max={2035} style={{ width: 88 }} value={year} onChange={(e) => setYear(Number(e.target.value))} />
          )}
          {mode === "MONTH" && (
            <select className="select" style={{ width: 88 }} value={month} onChange={(e) => setMonth(Number(e.target.value))}>
              {[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12].map((m) => (
                <option key={m} value={m}>{m}월</option>
              ))}
            </select>
          )}
          {mode === "CUSTOM" && (
            <>
              <input className="input" type="date" style={{ width: 142 }} value={startDate} onChange={(e) => setStartDate(e.target.value)} />
              <span>~</span>
              <input className="input" type="date" style={{ width: 142 }} value={endDate} onChange={(e) => setEndDate(e.target.value)} />
            </>
          )}
          <button type="button" className="btn btn-sm" onClick={loadReport}>
            <RefreshCw size={13} /> 새로고침
          </button>

          <div style={{ marginLeft: "auto", display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
            <select className="select" style={{ width: 155 }} value={certTeacherId} onChange={(e) => setCertTeacherId(e.target.value)}>
              <option value="">전 교원 결보강</option>
              {regularTeachers.map((t) => (
                <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>
              ))}
            </select>
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => handleDownload(`/api/export/hwpx?kind=substitution-cert&teacherId=${encodeURIComponent(certTeacherId)}&${queryParams}`, "결보강_수업교환_확인서.hwpx")}
            >
              <FileText size={13} /> 결보강 확인서 (.hwpx)
            </button>
          </div>
        </div>
      </div>

      {report && (
        <>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 12 }}>
            <div className="card"><div style={{ fontSize: 12, color: "#64748b" }}>확정 수업변경</div><div style={{ fontSize: 22, fontWeight: 800, color: "#1d4ed8" }}>{report.totalChanges}건</div></div>
            <div className="card"><div style={{ fontSize: 12, color: "#64748b" }}>결보강 대타</div><div style={{ fontSize: 22, fontWeight: 800, color: "#0369a1" }}>{report.totalSubstitutes}건</div></div>
            <div className="card"><div style={{ fontSize: 12, color: "#64748b" }}>교내 시험감독</div><div style={{ fontSize: 22, fontWeight: 800, color: "#b45309" }}>{report.totalInternal}슬롯</div></div>
            <div className="card"><div style={{ fontSize: 12, color: "#64748b" }}>외부 시험파견</div><div style={{ fontSize: 22, fontWeight: 800, color: "#047857" }}>{report.totalExternal}명</div></div>
            <div className="card"><div style={{ fontSize: 12, color: "#64748b" }}>확정 학사행사</div><div style={{ fontSize: 22, fontWeight: 800, color: "#6d28d9" }}>{report.totalEvents}건</div></div>
          </div>

          <div className="card">
            <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>월별 교무 실적 추이 ({report.monthlyTrend?.length || 0}개월)</h3>
            {(report.monthlyTrend || []).length === 0 ? (
              <div style={{ fontSize: 13, color: "#64748b" }}>해당 기간에 집계된 월별 데이터가 없습니다.</div>
            ) : (
              <table className="table">
                <thead>
                  <tr><th>월(YYYY-MM)</th><th>수업변경(확정)</th><th>결보강</th><th>교내감독</th><th>외부파견</th><th>확정행사</th></tr>
                </thead>
                <tbody>
                  {report.monthlyTrend.map((tr) => (
                    <tr key={tr.month}>
                      <td><b>{tr.month}</b></td>
                      <td>{tr.timetableChanges}건</td>
                      <td>{tr.substituteSessions}건</td>
                      <td>{tr.internalExamSlots}회</td>
                      <td>{tr.externalDispatches}명</td>
                      <td>{tr.approvedEvents}건</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          <div className="card">
            <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>교원별 기간 누적 실적 정산표 ({report.teacherStats?.length || 0}명)</h3>
            <div style={{ maxHeight: 360, overflowY: "auto" }}>
              <table className="table">
                <thead>
                  <tr><th>교사 (교과)</th><th>수업교환</th><th>보강수행</th><th>결보강요청</th><th>교내감독 (정/부/복도)</th><th>외부파견</th><th>행사참여</th><th>총실적</th></tr>
                </thead>
                <tbody>
                  {(report.teacherStats || []).map((st) => (
                    <tr key={st.teacherId}>
                      <td><b>{st.teacherName}</b> <span style={{ color: "#64748b", fontSize: 12 }}>({st.subject}{st.homeroomClass ? `·${st.homeroomClass}` : ""})</span></td>
                      <td>{st.swapCount}</td>
                      <td>{st.substituteGivenCount}</td>
                      <td>{st.substituteTakenCount}</td>
                      <td>{st.internalTotal} ({st.internalMainCount}/{st.internalSubCount}/{st.internalCorrCount})</td>
                      <td>{st.externalExamCount}</td>
                      <td>{st.eventLeadCount}</td>
                      <td><span className="badge badge-blue"><b>{st.totalActivity}</b></span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
