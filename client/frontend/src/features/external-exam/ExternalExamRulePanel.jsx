import React from "react";
import { ArrowDown, ArrowUp } from "lucide-react";

const CRITERIA_LABELS = {
  VOLUNTEER: "1순위: 해당 시험 자발적 지원자",
  LOW_COUNT: "누적 횟수/가중치 적은 순",
  LATE_TRANSFER: "전입일 늦은 순 (최근 전입 우선)",
  EARLY_TRANSFER: "전입일 빠른 순 (장기근속 우선)",
  OLDEST_DISPATCH: "이전 외부차출 오래된 순",
  NON_HOMEROOM: "비담임 교사 우선",
  NON_DEPT_HEAD: "비부장 교사 우선",
};

const EXCLUDE_RULES = [
  { code: "GRADE3_HOMEROOM", label: "고3 담임 제외" },
  { code: "EXAM_CHILD", label: "수험생 자녀 제외" },
  { code: "CONTRACT_TEACHER", label: "기간제 제외" },
  { code: "PREGNANT", label: "임산부 보호" },
  { code: "SENIOR", label: "원로교사 제외" },
  { code: "DEPT_HEAD", label: "보직부장 제외" },
];

// ExternalExamRulePanel은 가중치 점수제, 별도 트랙, 전입순 양방향·1년차 유예, 종목별 제외, 초과 선발 및 동점 기준을 설정합니다.
export default function ExternalExamRulePanel({ notice, onUpdateRules }) {
  if (!notice) return null;

  const order = notice.priorityOrder || [];
  const rules = notice.categoryExcludeRules || [];

  const apply = (patch = {}) => {
    onUpdateRules({
      presetMode: patch.presetMode ?? notice.presetMode,
      scoringMode: patch.scoringMode ?? notice.scoringMode ?? "TOTAL_COUNT",
      priorityOrder: patch.priorityOrder ?? order,
      excludeExempt: patch.excludeExempt ?? notice.excludeExempt,
      transferGraceYear: patch.transferGraceYear ?? Boolean(notice.transferGraceYear),
      categoryExcludeRules: patch.categoryExcludeRules ?? rules,
      tieBreaker: patch.tieBreaker ?? notice.tieBreaker ?? "NAME_ASC",
      lotterySeed: patch.lotterySeed ?? notice.lotterySeed ?? "",
      volunteerOverflowMode: patch.volunteerOverflowMode ?? notice.volunteerOverflowMode ?? "PRIORITY_ORDER",
    });
  };

  const moveCriterion = (idx, dir) => {
    const next = [...order];
    const target = idx + dir;
    if (target < 1 || target >= next.length) return;
    [next[idx], next[target]] = [next[target], next[idx]];
    apply({ presetMode: "CUSTOM", priorityOrder: next });
  };

  const toggleTransferDirection = () => {
    const hasEarly = order.includes("EARLY_TRANSFER");
    const next = order.map((c) => {
      if (hasEarly && c === "EARLY_TRANSFER") return "LATE_TRANSFER";
      if (!hasEarly && c === "LATE_TRANSFER") return "EARLY_TRANSFER";
      return c;
    });
    if (!next.includes("EARLY_TRANSFER") && !next.includes("LATE_TRANSFER")) {
      next.push(hasEarly ? "LATE_TRANSFER" : "EARLY_TRANSFER");
    }
    apply({ presetMode: "CUSTOM", priorityOrder: next });
  };

  const toggleExcludeRule = (code) => {
    const next = rules.includes(code) ? rules.filter((r) => r !== code) : [...rules, code];
    apply({ categoryExcludeRules: next });
  };

  return (
    <div style={{ padding: 12, background: "#f8fafc", borderRadius: 10, border: "1px solid #e2e8f0", marginBottom: 14, display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
        <b style={{ fontSize: 13 }}>우리 학교 외부감독 차출 기준 · 점수 · 유예 · 동점 규정</b>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          <button type="button" className="btn btn-sm" onClick={() => apply({ presetMode: "PRESET_MIXED", priorityOrder: [] })}>방식A: 혼합형</button>
          <button type="button" className="btn btn-sm" onClick={() => apply({ presetMode: "PRESET_COOLDOWN", priorityOrder: [] })}>방식B: 순번/쿨타임</button>
          <button type="button" className="btn btn-sm" onClick={() => apply({ presetMode: "PRESET_RECENT_TRANSFER", priorityOrder: [] })}>방식C: 최근 전입</button>
          <button type="button" className="btn btn-sm" onClick={toggleTransferDirection}>
            전입순: {order.includes("EARLY_TRANSFER") ? "장기근속(빠른순)" : "최근전입(늦은순)"} 전환
          </button>
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px, 1fr))", gap: 8 }}>
        <select className="select" value={notice.scoringMode || "TOTAL_COUNT"} onChange={(e) => apply({ scoringMode: e.target.value })}>
          <option value="TOTAL_COUNT">누적 산정: 전체 단순 횟수</option>
          <option value="WEIGHTED_SCORE">누적 산정: 종목 가중치 점수제</option>
          <option value="SEPARATE_TRACK">누적 산정: 종목별 별도 트랙</option>
        </select>
        <select className="select" value={notice.volunteerOverflowMode || "PRIORITY_ORDER"} onChange={(e) => apply({ volunteerOverflowMode: e.target.value })}>
          <option value="PRIORITY_ORDER">지원자 초과: 학교 우선순위순</option>
          <option value="LOW_COUNT_FIRST">지원자 초과: 누적(점수) 적은순</option>
          <option value="FIRST_COME">지원자 초과: 선착순 지원</option>
          <option value="LOTTERY">지원자 초과: 공정 추첨</option>
        </select>
        <select className="select" value={notice.tieBreaker || "NAME_ASC"} onChange={(e) => apply({ tieBreaker: e.target.value })}>
          <option value="NAME_ASC">동점 기준: 가나다(이름) 순</option>
          <option value="AGE_YOUNG">동점 기준: 젊은 교사(연령 낮은순)</option>
          <option value="AGE_OLD">동점 기준: 연장자(연령 높은순)</option>
          <option value="SALARY_LOW">동점 기준: 저호봉 우선</option>
          <option value="SALARY_HIGH">동점 기준: 고호봉 우선</option>
          <option value="LOTTERY">동점 기준: 공정 추첨</option>
        </select>
        {(notice.tieBreaker === "LOTTERY" || notice.volunteerOverflowMode === "LOTTERY") && (
          <input
            className="input"
            value={notice.lotterySeed || ""}
            onChange={(e) => apply({ lotterySeed: e.target.value })}
            placeholder="추첨 시드 (예: 2026-수능)"
          />
        )}
      </div>

      <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center", fontSize: 12 }}>
        <label style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          <input type="checkbox" checked={Boolean(notice.excludeExempt)} onChange={(e) => apply({ excludeExempt: e.target.checked })} />
          면제·제외자 후순위 배제
        </label>
        <label style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          <input type="checkbox" checked={Boolean(notice.transferGraceYear)} onChange={(e) => apply({ transferGraceYear: e.target.checked })} />
          전입 1년차 유예 적용 (비자원시 후순위)
        </label>
        <span style={{ color: "#475569", fontWeight: 700 }}>종목별 제외 규칙:</span>
        {EXCLUDE_RULES.map((er) => {
          const on = rules.includes(er.code);
          return (
            <button
              key={er.code}
              type="button"
              className={`btn btn-sm ${on ? "btn-danger" : ""}`}
              style={{ fontSize: 11, padding: "2px 7px" }}
              onClick={() => toggleExcludeRule(er.code)}
            >
              {on ? "✓ " : ""}{er.label}
            </button>
          );
        })}
      </div>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        {order.map((crit, idx) => (
          <div key={crit} style={{ display: "inline-flex", alignItems: "center", gap: 4, background: "#fff", border: "1px solid #cbd5e1", borderRadius: 8, padding: "4px 8px", fontSize: 12 }}>
            <b>{idx + 1}.</b> {CRITERIA_LABELS[crit] || crit}
            {idx > 0 && (
              <>
                <button type="button" style={{ border: 0, background: "transparent", cursor: "pointer" }} onClick={() => moveCriterion(idx, -1)}><ArrowUp size={12} /></button>
                <button type="button" style={{ border: 0, background: "transparent", cursor: "pointer" }} onClick={() => moveCriterion(idx, 1)}><ArrowDown size={12} /></button>
              </>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
