import React, { useCallback, useEffect, useRef, useState } from "react";
import { CheckCircle2, GitCompare, Save, Trash2 } from "lucide-react";
import { apiFetch } from "../../events/eventStore";
import ConfirmDialog from "../../components/ui/ConfirmDialog";

function MetricChips({ metrics }) {
  if (!metrics) return null;
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 6 }}>
      <span className="badge badge-blue">종합 {metrics.score}점</span>
      <span className="badge">총 {metrics.totalCells}셀</span>
      <span className="badge" style={{ background: metrics.conflictCount === 0 ? "#dcfce7" : "#fee2e2", color: metrics.conflictCount === 0 ? "#166534" : "#991b1b" }}>
        충돌 {metrics.conflictCount}건
      </span>
      <span className="badge" style={{ background: metrics.hardAvoidViolated === 0 ? "#dcfce7" : "#fee2e2", color: metrics.hardAvoidViolated === 0 ? "#166534" : "#991b1b" }}>
        금지 위반 {metrics.hardAvoidViolated}건
      </span>
      <span className="badge" style={{ background: "#e0f2fe", color: "#075985" }}>
        기피 {metrics.softAvoidViolated}건
      </span>
      <span className="badge" style={{ background: metrics.consecutive4Plus === 0 ? "#f1f5f9" : "#fef3c7", color: "#334155" }}>
        4연강+ {metrics.consecutive4Plus}회 · 3연강 {metrics.consecutive3Count}회
      </span>
      <span className="badge" style={{ background: "#f0fdf4", color: "#15803d" }}>
        전일 공강 {metrics.freeDayTeachers}명
      </span>
    </div>
  );
}

// ScenarioComparePanel은 현재 시간표 스냅샷을 A/B/C안 시나리오로 저장·품질 지표 비교·채택합니다.
export default function ScenarioComparePanel({ serverUrl, user, cells, onRefresh, onHistoryRefresh, onNotify }) {
  const [scenarios, setScenarios] = useState([]);
  const [currentMetrics, setCurrentMetrics] = useState(null);
  const [slotCode, setSlotCode] = useState("A");
  const [name, setName] = useState("A안 - 고정 공강 우선");
  const [description, setDescription] = useState("부장회의·기피교시 제약을 우선 반영한 초안");
  const [confirmAction, setConfirmAction] = useState(null);

  const onNotifyRef = useRef(onNotify);
  onNotifyRef.current = onNotify;

  const loadScenarios = useCallback(async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/scenarios", user.id);
      setScenarios(res.scenarios || []);
      setCurrentMetrics(res.currentMetrics || null);
    } catch (err) {
      onNotifyRef.current?.("시나리오 목록 조회 실패", err.message, true);
    }
  }, [serverUrl, user?.id]);

  useEffect(() => {
    loadScenarios();
  }, [loadScenarios, cells]);

  const handleSaveScenario = async (e) => {
    e.preventDefault();
    try {
      const saved = await apiFetch(serverUrl, "/api/timetable/scenarios", user.id, {
        method: "POST",
        body: JSON.stringify({ slotCode, name, description }),
      });
      onNotify("시나리오 저장 완료", `${saved.slotCode}안('${saved.name}') · 종합 ${saved.metrics?.score || 0}점`);
      await loadScenarios();
    } catch (err) {
      onNotify("시나리오 저장 실패", err.message, true);
    }
  };

  const handleApply = async (sc) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/scenarios/apply", user.id, {
        method: "POST",
        body: JSON.stringify({ scenarioId: sc.id }),
      });
      onNotify("시나리오 채택 완료", `'${sc.name}'이 정규 시간표로 적용되었습니다. (Ctrl+Z로 직전 상태 복원 가능)`);
      await loadScenarios();
      onHistoryRefresh?.();
      onRefresh?.();
    } catch (err) {
      onNotify("시나리오 적용 실패", err.message, true);
    }
  };

  const handleDelete = async (id) => {
    try {
      await apiFetch(serverUrl, `/api/timetable/scenarios?id=${encodeURIComponent(id)}`, user.id, {
        method: "DELETE",
      });
      onNotify("시나리오 삭제 완료", "선택한 비교 스냅샷이 삭제되었습니다.");
      await loadScenarios();
    } catch (err) {
      onNotify("시나리오 삭제 실패", err.message, true);
    }
  };

  const bestScore = scenarios.reduce((mx, s) => Math.max(mx, s.metrics?.score || 0), -1);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 8px", fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <GitCompare size={16} /> 현재 정규 시간표 품질 진단
          </h3>
          <div style={{ fontSize: 12, color: "#64748b" }}>
            자동 편성이나 수동 조정을 마친 뒤 A/B/C안으로 스냅샷을 저장해 품질을 객관적으로 비교하세요.
          </div>
          <MetricChips metrics={currentMetrics} />
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>현재 시간표를 시나리오로 저장</h3>
          <form onSubmit={handleSaveScenario} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="grid-2">
              <select className="select" value={slotCode} onChange={(e) => setSlotCode(e.target.value)}>
                <option value="A">A안 슬롯</option>
                <option value="B">B안 슬롯</option>
                <option value="C">C안 슬롯</option>
                <option value="D">D안 슬롯</option>
              </select>
              <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="시나리오 이름" />
            </div>
            <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="비고·특징 설명" />
            <button type="submit" className="btn btn-primary" style={{ alignSelf: "flex-start" }}>
              <Save size={14} /> {slotCode}안 스냅샷 저장
            </button>
          </form>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: "0 0 12px", fontSize: 15 }}>저장된 시나리오 A/B/C 비교 ({scenarios.length}개)</h3>
        {scenarios.length === 0 ? (
          <div style={{ fontSize: 13, color: "#64748b" }}>저장된 시나리오가 없습니다. 위 양식에서 A안부터 저장해 보세요.</div>
        ) : (
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(270px, 1fr))", gap: 12 }}>
            {scenarios.map((sc) => {
              const isBest = bestScore >= 0 && sc.metrics?.score === bestScore;
              return (
                <div
                  key={sc.id}
                  style={{
                    border: isBest ? "2px solid #2563eb" : "1px solid #cbd5e1",
                    borderRadius: 10,
                    padding: 12,
                    background: isBest ? "#eff6ff" : "#ffffff",
                    display: "flex",
                    flexDirection: "column",
                    justifyContent: "space-between",
                    gap: 8,
                  }}
                >
                  <div>
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                      <b>[{sc.slotCode}안] {sc.name}</b>
                      {isBest && <span className="badge badge-blue">최적 점수</span>}
                    </div>
                    <div style={{ fontSize: 12, color: "#475569", marginTop: 4 }}>{sc.description || "설명 없음"}</div>
                    <div style={{ fontSize: 11, color: "#64748b", marginTop: 4 }}>저장: {sc.createdBy} · {sc.createdAt}</div>
                    <MetricChips metrics={sc.metrics} />
                  </div>
                  <div style={{ display: "flex", gap: 8, marginTop: 6 }}>
                    <button type="button" className="btn btn-primary btn-sm" onClick={() => setConfirmAction({ type: "apply", scenario: sc })}>
                      <CheckCircle2 size={13} /> 이 안 채택 (정규 반영)
                    </button>
                    <button type="button" className="btn btn-sm" onClick={() => setConfirmAction({ type: "delete", scenario: sc })}>
                      <Trash2 size={13} /> 삭제
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={Boolean(confirmAction)}
        onOpenChange={(open) => !open && setConfirmAction(null)}
        danger={confirmAction?.type === "delete"}
        title={confirmAction?.type === "apply" ? "시나리오 정규 시간표 채택" : "시나리오 스냅샷 삭제"}
        description={
          confirmAction?.type === "apply"
            ? `[${confirmAction.scenario.slotCode}안] '${confirmAction.scenario.name}'을 현재 정규 시간표로 적용하시겠습니까?\n적용 전 상태는 실행취소(Ctrl+Z) 이력에 자동 보관됩니다.`
            : `[${confirmAction?.scenario?.slotCode}안] '${confirmAction?.scenario?.name}' 스냅샷을 삭제하시겠습니까?`
        }
        confirmLabel={confirmAction?.type === "apply" ? "이 안 채택" : "삭제 실행"}
        onConfirm={async () => {
          const act = confirmAction;
          setConfirmAction(null);
          if (!act) return;
          if (act.type === "apply") {
            await handleApply(act.scenario);
          } else {
            await handleDelete(act.scenario.id);
          }
        }}
      />
    </div>
  );
}
