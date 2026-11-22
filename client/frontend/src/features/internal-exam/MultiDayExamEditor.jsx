import React, { useMemo, useState } from "react";
import { CalendarRange, Clock, Plus, Trash2 } from "lucide-react";

function calcMinutes(start, end) {
  if (!start || !end) return 45;
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  const diff = eh * 60 + em - (sh * 60 + sm);
  return diff > 0 ? diff : 45;
}

function buildWeekdayDates(startISO, dayCount) {
  const base = startISO ? new Date(`${startISO}T00:00:00`) : new Date();
  if (Number.isNaN(base.getTime())) return [];
  const dates = [];
  const cur = new Date(base);
  while (dates.length < dayCount) {
    const wd = cur.getDay();
    if (wd !== 0 && wd !== 6) {
      const y = cur.getFullYear();
      const m = String(cur.getMonth() + 1).padStart(2, "0");
      const d = String(cur.getDate()).padStart(2, "0");
      dates.push(`${y}-${m}-${d}`);
    }
    cur.setDate(cur.getDate() + 1);
  }
  return dates;
}

const DEFAULT_DAY_SLOTS = [
  { period: 1, grade: 1, subject: "국어", startTime: "08:50", endTime: "09:35", durationMinutes: 45, classRooms: ["1-1", "1-2"] },
  { period: 2, grade: 2, subject: "수학", startTime: "09:50", endTime: "10:40", durationMinutes: 50, classRooms: ["2-1", "2-2"] },
  { period: 3, grade: 3, subject: "영어", startTime: "10:55", endTime: "11:35", durationMinutes: 40, classRooms: ["3-1", "3-2"] },
];

// MultiDayExamEditor는 단일일 또는 3~5일 연속 지필고사 시간표·과목·고사실·자습실·출제교사·연강 방지 옵션을 편집합니다.
export default function MultiDayExamEditor({ config, setConfig, specs, setSpecs, roomCatalog, stats = [] }) {
  const [filterDate, setFilterDate] = useState("ALL");

  const uniqueDates = useMemo(() => {
    const set = new Set(config?.examDates || []);
    if (config?.examDate) set.add(config.examDate);
    specs.forEach((sp) => {
      if (sp.examDate) set.add(sp.examDate);
    });
    return Array.from(set).filter(Boolean).sort();
  }, [config?.examDate, config?.examDates, specs]);

  const applyPeriodPreset = (daysCount) => {
    const startISO = config?.examDate || new Date().toISOString().slice(0, 10);
    const dates = buildWeekdayDates(startISO, daysCount);
    if (dates.length === 0) return;

    const template = [];
    const seenPeriods = new Set();
    specs.forEach((sp) => {
      if (!seenPeriods.has(sp.period)) {
        seenPeriods.add(sp.period);
        template.push({
          period: sp.period,
          grade: sp.grade,
          subject: sp.subject,
          startTime: sp.startTime || "08:50",
          endTime: sp.endTime || "09:35",
          durationMinutes: sp.durationMinutes || 45,
          classRooms: Array.isArray(sp.classRooms) ? [...sp.classRooms] : ["1-1", "1-2"],
        });
      }
    });
    const baseTemplate = template.length > 0 ? template : DEFAULT_DAY_SLOTS;
    const subjectsByDay = [
      ["국어", "수학", "영어"],
      ["사회", "과학", "역사"],
      ["한국사", "통합과학", "도덕"],
      ["정보", "기술가정", "한문"],
      ["심화수학", "물리", "화학"],
    ];

    const nextSpecs = [];
    dates.forEach((d, dayIdx) => {
      baseTemplate.forEach((tpl, pIdx) => {
        const subj = daysCount === 1
          ? tpl.subject
          : (subjectsByDay[dayIdx % subjectsByDay.length][pIdx % 3] || tpl.subject);
        nextSpecs.push({ ...tpl, examDate: d, subject: subj, classRooms: [...tpl.classRooms] });
      });
    });

    setConfig((prev) => ({
      ...prev,
      examDate: dates[0],
      examDates: dates,
      avoidConsecutive: daysCount > 1 ? true : prev.avoidConsecutive,
    }));
    setSpecs(nextSpecs);
    setFilterDate("ALL");
  };

  const updateSpec = (targetIdx, field, val) => {
    setSpecs((prev) =>
      prev.map((sp, i) => {
        if (i !== targetIdx) return sp;
        const next = { ...sp, [field]: val };
        if (field === "startTime" || field === "endTime") {
          next.durationMinutes = calcMinutes(next.startTime, next.endTime);
        }
        return next;
      })
    );
  };

  const activeAddDate = filterDate !== "ALL" ? filterDate : config?.examDate || uniqueDates[0] || "";
  const visibleRows = specs
    .map((sp, idx) => ({ sp, idx }))
    .filter(({ sp }) => filterDate === "ALL" || (sp.examDate || config?.examDate) === filterDate);

  return (
    <div style={{ marginTop: 14, padding: 12, borderRadius: 10, background: "#f8fafc", border: "1px solid #e2e8f0" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 8 }}>
        <b style={{ fontSize: 13 }}>
          <Clock size={14} style={{ verticalAlign: "middle" }} /> 다중일(3~5일)·교시·과목별 시험시간 및 고사실 편성
        </b>
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>
          <span style={{ fontSize: 12, color: "#475569", display: "inline-flex", alignItems: "center", gap: 4 }}>
            <CalendarRange size={13} /> 기간 생성:
          </span>
          {[1, 3, 4, 5].map((cnt) => (
            <button key={cnt} type="button" className="btn btn-sm" onClick={() => applyPeriodPreset(cnt)}>
              {cnt}일 편성
            </button>
          ))}
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => {
              const electives = roomCatalog?.electiveRooms || [];
              const firstG = electives[0]?.grade || 2;
              const roomList = electives.length > 0 ? electives.slice(0, 3).map((e) => e.label) : ["세미나실A(심화수학)", "수학전용1실(심화수학)"];
              const subjList = electives.length > 0 ? Array.from(new Set(electives.slice(0, 3).map((e) => e.subject))).join(", ") : "심화수학";
              setSpecs((prev) => [
                ...prev,
                { examDate: activeAddDate, period: prev.length + 1, grade: firstG, subject: subjList, startTime: "13:10", endTime: "14:00", durationMinutes: 50, classRooms: roomList },
              ]);
            }}
          >
            <Plus size={13} /> 선택과목 교시
          </button>
          <button
            type="button"
            className="btn btn-sm"
            onClick={() =>
              setSpecs((prev) => [
                ...prev,
                { examDate: activeAddDate, period: prev.length + 1, grade: 1, subject: "과학", startTime: "11:45", endTime: "12:25", durationMinutes: 40, classRooms: ["1-1", "1-2"] },
              ])
            }
          >
            <Plus size={13} /> 일반 교시
          </button>
        </div>
      </div>

      <div style={{ display: "flex", gap: 14, flexWrap: "wrap", alignItems: "center", marginBottom: 10 }}>
        <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 5 }}>
          시작일:
          <input
            className="input"
            type="date"
            style={{ width: 138, padding: "4px 6px" }}
            value={config?.examDate || ""}
            onChange={(e) => setConfig({ ...config, examDate: e.target.value })}
          />
        </label>
        <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 5 }}>
          <input
            type="checkbox"
            checked={Boolean(config?.avoidConsecutive)}
            onChange={(e) => setConfig({ ...config, avoidConsecutive: e.target.checked })}
          />
          동일 일자 연강(연속 교시) 감독 방지
        </label>
        <label style={{ fontSize: 12.5, display: "flex", alignItems: "center", gap: 6 }}>
          1일 1교사 최대 감독 교시:
          <input
            className="input"
            type="number"
            min={0}
            max={7}
            style={{ width: 64, padding: "4px 6px" }}
            value={config?.maxDailyPerTeacher ?? 0}
            onChange={(e) => setConfig({ ...config, maxDailyPerTeacher: Number(e.target.value) })}
          />
          <span style={{ fontSize: 11.5, color: "#64748b" }}>(0=무제한)</span>
        </label>
      </div>

      {uniqueDates.length > 1 && (
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 8 }}>
          <button type="button" className={`btn btn-sm ${filterDate === "ALL" ? "btn-primary" : ""}`} onClick={() => setFilterDate("ALL")}>
            전체 ({specs.length})
          </button>
          {uniqueDates.map((d, idx) => (
            <button key={d} type="button" className={`btn btn-sm ${filterDate === d ? "btn-primary" : ""}`} onClick={() => setFilterDate(d)}>
              {idx + 1}일차 ({d})
            </button>
          ))}
        </div>
      )}

      <table className="table">
        <thead>
          <tr><th>시험일자</th><th>교시</th><th>학년</th><th>시험과목</th><th>시작</th><th>종료</th><th>분</th><th>고사실(쉼표 구분)</th><th>삭제</th></tr>
        </thead>
        <tbody>
          {visibleRows.map(({ sp, idx }) => (
            <tr key={idx}>
              <td><input className="input" type="date" style={{ width: 130 }} value={sp.examDate || config?.examDate || ""} onChange={(e) => updateSpec(idx, "examDate", e.target.value)} /></td>
              <td><input className="input" type="number" min={1} max={7} style={{ width: 52 }} value={sp.period} onChange={(e) => updateSpec(idx, "period", e.target.value)} /></td>
              <td><input className="input" type="number" min={1} max={3} style={{ width: 52 }} value={sp.grade} onChange={(e) => updateSpec(idx, "grade", e.target.value)} /></td>
              <td><input className="input" style={{ width: 88 }} value={sp.subject} onChange={(e) => updateSpec(idx, "subject", e.target.value)} /></td>
              <td><input className="input" type="time" style={{ width: 110 }} value={sp.startTime || "08:50"} onChange={(e) => updateSpec(idx, "startTime", e.target.value)} /></td>
              <td><input className="input" type="time" style={{ width: 110 }} value={sp.endTime || "09:35"} onChange={(e) => updateSpec(idx, "endTime", e.target.value)} /></td>
              <td><input className="input" type="number" min={10} max={120} style={{ width: 58 }} value={sp.durationMinutes || 45} onChange={(e) => updateSpec(idx, "durationMinutes", e.target.value)} /></td>
              <td>
                <input
                  className="input"
                  value={Array.isArray(sp.classRooms) ? sp.classRooms.join(",") : sp.classRooms}
                  onChange={(e) => updateSpec(idx, "classRooms", e.target.value.split(",").map((s) => s.trim()))}
                />
                <div style={{ display: "flex", gap: 4, marginTop: 4, flexWrap: "wrap" }}>
                  <select
                    className="select"
                    style={{ fontSize: 11, padding: "2px 6px", height: 24, maxWidth: 200 }}
                    value=""
                    onChange={(e) => {
                      const v = e.target.value;
                      if (!v) return;
                      const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                      if (v === "__FILL_GRADE__") {
                        const clsList = roomCatalog?.gradeClassRooms?.[Number(sp.grade)] || [];
                        updateSpec(idx, "classRooms", Array.from(new Set([...cur, ...clsList])));
                      } else {
                        updateSpec(idx, "classRooms", Array.from(new Set([...cur, v])));
                      }
                    }}
                  >
                    <option value="">+ 고사실 추가...</option>
                    <option value="__FILL_GRADE__">{sp.grade}학년 전체 정규반</option>
                    {(roomCatalog?.electiveRooms || []).length > 0 && (
                      <optgroup label="고교선택과목 · 분반">
                        {(roomCatalog.electiveRooms || []).map((el) => (
                          <option key={el.label} value={el.label}>{el.label}</option>
                        ))}
                      </optgroup>
                    )}
                    {(roomCatalog?.specialRooms || []).length > 0 && (
                      <optgroup label="교과교실 · 특별실">
                        {(roomCatalog.specialRooms || []).map((rm) => (
                          <option key={rm} value={rm}>{rm}</option>
                        ))}
                      </optgroup>
                    )}
                    <optgroup label="특수 · 분리 고사실">
                      {(roomCatalog?.isolatedRooms || ["별도고사실(독감·감염병)", "특수학급고사실"]).map((ir) => (
                        <option key={ir} value={ir}>{ir}</option>
                      ))}
                    </optgroup>
                  </select>
                  <button
                    type="button"
                    className="btn btn-sm"
                    style={{ fontSize: 11, padding: "2px 6px", height: 24 }}
                    onClick={() => {
                      const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                      const tag = "별도고사실(독감·감염병)";
                      updateSpec(idx, "classRooms", cur.includes(tag) ? cur.filter((r) => r !== tag) : [...cur, tag]);
                    }}
                  >
                    + 별도고사실
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm"
                    style={{ fontSize: 11, padding: "2px 6px", height: 24 }}
                    onClick={() => {
                      const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                      const multi = ["별도고사실 1실(감염병)", "별도고사실 2실(감염병)", "별도고사실 3실(감염병)"];
                      const hasAll = multi.every((m) => cur.includes(m));
                      updateSpec(idx, "classRooms", hasAll ? cur.filter((r) => !multi.includes(r)) : Array.from(new Set([...cur, ...multi])));
                    }}
                  >
                    + 감염병 1·2·3실
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm"
                    style={{ fontSize: 11, padding: "2px 6px", height: 24 }}
                    onClick={() => {
                      const cur = Array.isArray(sp.classRooms) ? sp.classRooms.filter(Boolean) : [];
                      const tag = "특수학급고사실";
                      updateSpec(idx, "classRooms", cur.includes(tag) ? cur.filter((r) => r !== tag) : [...cur, tag]);
                    }}
                  >
                    + 특수학급고사실
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm"
                    style={{ fontSize: 11, padding: "2px 6px", height: 24 }}
                    onClick={() => {
                      const cur = Array.isArray(sp.studyHallRooms) ? sp.studyHallRooms.filter(Boolean) : [];
                      const tag = (roomCatalog?.studyHallRooms || ["도서실(공강자습)"])[0];
                      updateSpec(idx, "studyHallRooms", cur.includes(tag) ? [] : [tag]);
                    }}
                  >
                    {(sp.studyHallRooms || []).length > 0 ? "✓ 공강자습실" : "+ 공강자습실"}
                  </button>
                </div>
                {config?.subjectExcludeMode === "AUTHORS_ONLY" && stats.length > 0 && (
                  <div style={{ display: "flex", gap: 3, flexWrap: "wrap", marginTop: 4 }}>
                    <span style={{ fontSize: 10.5, color: "#64748b" }}>출제자:</span>
                    {stats.filter((st) => !st.isExempt).map((st) => {
                      const picked = (sp.authorTeacherIds || []).includes(st.teacherId);
                      return (
                        <button
                          key={st.teacherId}
                          type="button"
                          className={`btn btn-sm ${picked ? "btn-primary" : ""}`}
                          style={{ fontSize: 10, padding: "1px 5px", height: 20 }}
                          onClick={() => {
                            const cur = sp.authorTeacherIds || [];
                            updateSpec(idx, "authorTeacherIds", picked ? cur.filter((x) => x !== st.teacherId) : [...cur, st.teacherId]);
                          }}
                        >
                          {st.teacherName}
                        </button>
                      );
                    })}
                  </div>
                )}
              </td>
              <td>
                <button type="button" className="btn btn-sm btn-danger" onClick={() => setSpecs((prev) => prev.filter((_, i) => i !== idx))}>
                  <Trash2 size={12} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
