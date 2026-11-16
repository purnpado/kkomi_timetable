import React, { useEffect, useState } from "react";
import { Clock, Save } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

const MIDDLE_PRESET = [
  { period: 1, label: "1교시", startTime: "08:50", endTime: "09:35" },
  { period: 2, label: "2교시", startTime: "09:45", endTime: "10:30" },
  { period: 3, label: "3교시", startTime: "10:40", endTime: "11:25" },
  { period: 4, label: "4교시", startTime: "11:35", endTime: "12:20" },
  { period: 5, label: "5교시", startTime: "13:20", endTime: "14:05" },
  { period: 6, label: "6교시", startTime: "14:15", endTime: "15:00" },
  { period: 7, label: "7교시", startTime: "15:10", endTime: "15:55" },
];

const HIGH_PRESET = [
  { period: 1, label: "1교시", startTime: "08:40", endTime: "09:30" },
  { period: 2, label: "2교시", startTime: "09:40", endTime: "10:30" },
  { period: 3, label: "3교시", startTime: "10:40", endTime: "11:30" },
  { period: 4, label: "4교시", startTime: "11:40", endTime: "12:30" },
  { period: 5, label: "5교시", startTime: "13:30", endTime: "14:20" },
  { period: 6, label: "6교시", startTime: "14:30", endTime: "15:20" },
  { period: 7, label: "7교시", startTime: "15:30", endTime: "16:20" },
];

// BellScheduleEditor는 정규 시간표 편성 시 1~7교시 일과시간(시종시간)을 설정·저장하는 편집기입니다.
export default function BellScheduleEditor({ serverUrl, user, bellSchedule, onRefresh, onNotify }) {
  const [rows, setRows] = useState(bellSchedule?.length ? bellSchedule : HIGH_PRESET);

  useEffect(() => {
    if (bellSchedule?.length) setRows(bellSchedule);
  }, [bellSchedule]);

  const updateRow = (idx, field, val) => {
    setRows((prev) => prev.map((r, i) => (i === idx ? { ...r, [field]: val } : r)));
  };

  const handleSave = async () => {
    try {
      await apiFetch(serverUrl, "/api/calendar/bells", user.id, {
        method: "POST",
        body: JSON.stringify(rows),
      });
      onNotify("일과시간 저장 완료", "1~7교시 시작·종료 시각이 시간표 및 방해 금지 타이머에 반영되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("일과시간 저장 실패", err.message, true);
    }
  };

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10, marginBottom: 12 }}>
        <div>
          <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <Clock size={16} color="#3b5bdb" /> 정규 시간표 일과시간 (교시별 시작·종료 시각) 설정
          </h3>
          <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>
            설정한 시간은 시간표 그리드 좌측 교시표와 수업 중 방해 금지 모드에 실시간 반영됩니다.
          </div>
        </div>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <button type="button" className="btn btn-sm" onClick={() => setRows(MIDDLE_PRESET)}>중학교 45분 프리셋</button>
          <button type="button" className="btn btn-sm" onClick={() => setRows(HIGH_PRESET)}>고등학교 50분 프리셋</button>
          <button type="button" className="btn btn-sm btn-primary" onClick={handleSave}>
            <Save size={13} /> 일과시간 서버 저장
          </button>
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(175px, 1fr))", gap: 10 }}>
        {rows.map((r, idx) => (
          <div key={r.period} style={{ padding: 10, borderRadius: 10, border: "1px solid #dbe3f0", background: "#f8fafc" }}>
            <div style={{ fontWeight: 700, fontSize: 12.5, marginBottom: 6 }}>{r.label || `${r.period}교시`}</div>
            <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
              <input className="input" type="time" value={r.startTime} onChange={(e) => updateRow(idx, "startTime", e.target.value)} />
              <span>~</span>
              <input className="input" type="time" value={r.endTime} onChange={(e) => updateRow(idx, "endTime", e.target.value)} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
