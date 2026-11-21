import React from "react";
import { BarChart3, CheckCircle2, RotateCcw, ShieldOff } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

function exemptPresetValue(st) {
  if (!st.isExempt) return "ACTIVE";
  if ((st.exemptReason || "").includes("특수")) return "SPECIAL";
  if ((st.exemptReason || "").includes("보건")) return "NURSE";
  return "OTHER";
}

function exemptPresetToPayload(stat, preset) {
  if (preset === "ACTIVE") {
    return {
      teacherId: stat.teacherId,
      internalExamExempt: false,
      internalExamExemptReason: "",
      annualMainCount: stat.baseMain,
      annualSubCount: stat.baseSub,
      annualCorridorCount: stat.baseCorridor,
    };
  }
  const reasons = {
    SPECIAL: "특수반(개별고사장 전담)",
    NURSE: "보건실 상주(학교 지정 제외)",
    OTHER: "특수보직·학교 지정 교내 감독 제외",
  };
  return {
    teacherId: stat.teacherId,
    internalExamExempt: true,
    internalExamExemptReason: reasons[preset] || "특수보직·학교 지정 교내 감독 제외",
    annualMainCount: stat.baseMain,
    annualSubCount: stat.baseSub,
    annualCorridorCount: stat.baseCorridor,
  };
}

// ExamFairnessPanel은 1년간 전교원 시험감독 균등 배정 통계(총 시수 편차·정/부감독 편차)와
// 특수반 등 제외 교원 설정(별도고사실은 순환감독 합산), 연간 기실시 누적 관리를 제공합니다.
export default function ExamFairnessPanel({ serverUrl, user, fairness, isExamAdmin, onRefresh, onNotify }) {
  if (!fairness) return null;

  const updatePolicy = async (payload, okMsg) => {
    try {
      await apiFetch(serverUrl, "/api/internal-exam/teacher-policy", user.id, {
        method: "POST",
        body: JSON.stringify(payload),
      });
      if (okMsg) onNotify("감독 시수 정책 반영", okMsg);
      onRefresh();
    } catch (err) {
      onNotify("정책 반영 실패", err.message, true);
    }
  };

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 12 }}>
        <div>
          <h3 style={{ margin: 0, fontSize: 15.5 }}>
            <BarChart3 size={16} style={{ verticalAlign: "middle", color: "#3b5bdb" }} /> 1년 전교원 시험감독 시수 균등 현황 (별도고사실 순환감독 포함)
          </h3>
          <div style={{ fontSize: 12, color: "#64748b", marginTop: 4 }}>
            감독 참여 교원 <b>{fairness.eligibleCount}명</b> · 지정 제외(특수반 등) <b>{fairness.exemptCount}명</b> · 운영 방식:{" "}
            <b>{fairness.deputyMode ? "정·부감독 2인 공동 운영" : "정감독 1인 단독 운영"}</b>
          </div>
        </div>

        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
          <span className={`badge ${fairness.maxGap <= 1 ? "badge-green" : "badge-amber"}`}>
            <CheckCircle2 size={12} /> 1년 총 시수: {fairness.minTotal}~{fairness.maxTotal}회 (편차 {fairness.maxGap}회)
          </span>
          {fairness.examDayCount > 0 && (
            <span className="badge badge-gray">시험기간 {fairness.examDayCount}일 · 1일 최대 {fairness.maxDailyAssigned}교시</span>
          )}
          {fairness.examDayCount > 0 && (
            <span className={`badge ${fairness.consecutivePairCount === 0 ? "badge-green" : "badge-amber"}`}>
              연강 감독 {fairness.consecutivePairCount}건
            </span>
          )}
          {fairness.deputyMode && (
            <span className={`badge ${fairness.mainGap <= 1 ? "badge-blue" : "badge-amber"}`}>
              정감독 횟수: {fairness.minMain}~{fairness.maxMain}회 (편차 {fairness.mainGap}회)
            </span>
          )}
          {isExamAdmin && (
            <>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => updatePolicy({ resetAllBaseCounts: true }, "전교원 기실시 누적 감독 횟수를 0회로 초기화했습니다. 상단의 [규칙·시험시간 적용 자동 배정]을 눌러 재배정하세요.")}
              >
                <RotateCcw size={12} /> 기실시 누적 0회 초기화
              </button>
              <button
                type="button"
                className="btn btn-sm btn-primary"
                onClick={() => updatePolicy({ commitCurrentToBase: true }, "이번 시험 감독 배정을 전교원 연간 누적 시수에 합산 확정했습니다.")}
              >
                이번 시험 연간 누적 확정
              </button>
            </>
          )}
        </div>
      </div>

      <div style={{ maxHeight: 320, overflowY: "auto" }}>
        <table className="table">
          <thead>
            <tr>
              <th>교사 (교과 · 담임)</th>
              <th>감독 참여/제외 설정 (특수반 등)</th>
              <th>기실시 누적 (정/부/복도)</th>
              <th>이번 시험 (정/부/복도)</th>
              <th>1년 합계 (정 / 부 / 복도)</th>
              <th>1년 총 횟수</th>
            </tr>
          </thead>
          <tbody>
            {(fairness.stats || []).map((st) => (
              <tr key={st.teacherId} style={st.isExempt ? { background: "#f8fafc", color: "#64748b" } : undefined}>
                <td>
                  <b>{st.teacherName}</b> ({st.subject}
                  {st.homeroomClass ? ` · ${st.homeroomClass}` : ""})
                </td>
                <td>
                  {isExamAdmin ? (
                    <select
                      className="select"
                      style={{ width: 210, padding: "4px 6px" }}
                      value={exemptPresetValue(st)}
                      onChange={(e) =>
                        updatePolicy(
                          exemptPresetToPayload(st, e.target.value),
                          `${st.teacherName} 선생님의 감독 제외 설정이 변경되었습니다. 필요 시 [규칙·시험시간 적용 자동 배정]을 눌러 다시 배정하세요.`
                        )
                      }
                    >
                      <option value="ACTIVE">일반 감독 대상 (별도고사실 순환)</option>
                      <option value="SPECIAL">제외: 특수반(특수학급 전담)</option>
                      <option value="NURSE">제외: 보건실 상주(미참여 시)</option>
                      <option value="OTHER">제외: 기타 보직·감면</option>
                    </select>
                  ) : st.isExempt ? (
                    <span className="badge badge-gray">
                      <ShieldOff size={11} /> {st.exemptReason}
                    </span>
                  ) : (
                    <span className="badge badge-green">감독 대상</span>
                  )}
                </td>
                <td>
                  {st.baseMain} / {st.baseSub} / {st.baseCorridor} ({st.baseTotal})
                </td>
                <td>
                  {st.currentMain} / {st.currentSub} / {st.currentCorridor} ({st.currentTotal})
                </td>
                <td>
                  {st.projectedMain} / {st.projectedSub} / {st.projectedCorridor}
                </td>
                <td>
                  {st.isExempt ? (
                    st.currentTotal > 0 ? (
                      <span className="badge badge-amber">
                        전담 <b>{st.projectedTotal}회</b>
                      </span>
                    ) : (
                      <span className="badge badge-gray">제외</span>
                    )
                  ) : (
                    <span className="badge badge-blue">
                      <b>{st.projectedTotal}회</b>
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
