import React from "react";
import { Save, Trash2 } from "lucide-react";

const DAYS = ["", "월", "화", "수", "목", "금"];

// BaseCellEditorCard는 기초 시간표 편집기의 우측 교시 상세 편집 폼(3인 이상 복수 코티칭 포함)을 렌더링합니다.
export default function BaseCellEditorCard({
  className,
  selectedDay,
  selectedPeriod,
  regularTeachers,
  teachersMap,
  subject,
  setSubject,
  subjectCode,
  setSubjectCode,
  teacherId,
  setTeacherId,
  coTeacherId,
  setCoTeacherId,
  coTeacherIds,
  setCoTeacherIds,
  room,
  setRoom,
  electiveGroup,
  setElectiveGroup,
  currentSlot,
  onSaveCell,
  onRequestDelete,
}) {
  const toggleExtraCoTeacher = (id) => {
    if (!id || id === teacherId || id === coTeacherId) return;
    if ((coTeacherIds || []).includes(id)) {
      setCoTeacherIds(coTeacherIds.filter((x) => x !== id));
    } else {
      setCoTeacherIds([...(coTeacherIds || []), id]);
    }
  };

  return (
    <div className="card">
      <h3 style={{ margin: "0 0 12px", fontSize: 15 }}>
        {className}반 {DAYS[selectedDay]}요일 {selectedPeriod}교시 편집
      </h3>
      {regularTeachers.length === 0 ? (
        <div style={{ fontSize: 13, color: "#64748b", padding: "12px 0" }}>
          아직 등록된 교과 교사가 없습니다. 먼저 [교사 명렬표] 탭에서 교사를 등록해 주세요.
        </div>
      ) : (
        <form onSubmit={onSaveCell} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div className="grid-2">
            <div>
              <label style={{ fontSize: 12, fontWeight: 700 }}>과목명</label>
              <input className="input" value={subject} onChange={(e) => setSubject(e.target.value)} required />
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700 }}>나이스 과목코드</label>
              <input className="input" value={subjectCode} onChange={(e) => setSubjectCode(e.target.value)} />
            </div>
          </div>
          <div>
            <label style={{ fontSize: 12, fontWeight: 700 }}>주 담당교사 (중복·순회일 자동 검증)</label>
            <select
              className="select"
              value={teacherId}
              onChange={(e) => {
                setTeacherId(e.target.value);
                if (teachersMap[e.target.value]?.subject) setSubject(teachersMap[e.target.value].subject);
              }}
            >
              {regularTeachers.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name} ({t.subject}{t.homeroomClass ? ` · ${t.homeroomClass}담임` : ""})
                </option>
              ))}
            </select>
          </div>
          <div className="grid-2">
            <div>
              <label style={{ fontSize: 12, fontWeight: 700 }}>코티칭 협력교사 1 (선택)</label>
              <select className="select" value={coTeacherId} onChange={(e) => setCoTeacherId(e.target.value)}>
                <option value="">없음</option>
                {regularTeachers.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>)}
              </select>
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700 }}>교실/특별실</label>
              <input className="input" value={room} onChange={(e) => setRoom(e.target.value)} />
            </div>
          </div>
          <div>
            <label style={{ fontSize: 12, fontWeight: 700 }}>추가 코티칭 협력교사 (3인 이상 복수교사 수업)</label>
            <div style={{ display: "flex", gap: 4, flexWrap: "wrap", marginTop: 4 }}>
              {regularTeachers
                .filter((t) => t.id !== teacherId && t.id !== coTeacherId)
                .map((t) => {
                  const picked = (coTeacherIds || []).includes(t.id);
                  return (
                    <button
                      key={t.id}
                      type="button"
                      className={`btn btn-sm ${picked ? "btn-primary" : ""}`}
                      style={{ fontSize: 11, padding: "2px 7px" }}
                      onClick={() => toggleExtraCoTeacher(t.id)}
                    >
                      {picked ? "✓ " : "+ "}
                      {t.name}
                    </button>
                  );
                })}
            </div>
          </div>
          <div>
            <label style={{ fontSize: 12, fontWeight: 700 }}>고교학점제 선택블록 ID (선택)</label>
            <input className="input" value={electiveGroup} onChange={(e) => setElectiveGroup(e.target.value)} placeholder="예: G2-EL-A (일반수업은 비워둠)" />
          </div>
          <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
            <button type="submit" className="btn btn-primary">
              <Save size={14} /> 교시 저장/반영
            </button>
            {currentSlot && (
              <button type="button" className="btn btn-danger" onClick={() => onRequestDelete(currentSlot)}>
                <Trash2 size={14} /> 삭제
              </button>
            )}
          </div>
        </form>
      )}
    </div>
  );
}
