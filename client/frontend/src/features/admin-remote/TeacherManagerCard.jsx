import React, { useMemo, useState } from "react";
import { Download, KeyRound, Pencil, ShieldAlert, Trash2, Upload, UserPlus, X } from "lucide-react";
import {
  apiFetch,
  effectiveTeacherType,
  getTeacherTypeMeta,
  hasRegularClasses,
  hasRole,
  isFacultyTeacher,
  isSubstituteExcluded,
  isSystemAdminAccount,
  TEACHER_TYPES,
} from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import AppTooltip from "../../components/ui/AppTooltip";

const DEFAULT_POSITIONS = new Set(TEACHER_TYPES.map((m) => m.defaultPosition));
const DEFAULT_SUBJECTS = new Set(TEACHER_TYPES.map((m) => m.defaultSubject));

// TeacherManagerCard는 최고관리자 또는 시간표 담당교사가 클라이언트에서 신규 교사 등록·수정,
// 비교과 유형 및 결보강 대타 제외 설정, 명렬표 엑셀 다운로드/일괄 업로드를 수행하는 관리 카드입니다.
export default function TeacherManagerCard({ serverUrl, user, teachers, onRefresh, onNotify, showSystemAdminSection = false }) {
  const [editingId, setEditingId] = useState("");
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [teacherType, setTeacherType] = useState("SUBJECT");
  const [hasClasses, setHasClasses] = useState(true);
  const [substituteExempt, setSubstituteExempt] = useState(false);
  const [subject, setSubject] = useState("국어");
  const [position, setPosition] = useState("평교사");
  const [homeroomClass, setHomeroomClass] = useState("");
  const [defaultRoom, setDefaultRoom] = useState("");
  const [transferDate, setTransferDate] = useState("2026-03-01");
  const [itinerantDaysText, setItinerantDaysText] = useState("");
  const [confirmTarget, setConfirmTarget] = useState(null);

  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const systemAdmin = useMemo(() => (teachers || []).find(isSystemAdminAccount), [teachers]);

  const resetForm = () => {
    setEditingId("");
    setName("");
    setUsername("");
    setTeacherType("SUBJECT");
    setHasClasses(true);
    setSubstituteExempt(false);
    setSubject("국어");
    setPosition("평교사");
    setHomeroomClass("");
    setDefaultRoom("");
    setTransferDate("2026-03-01");
    setItinerantDaysText("");
  };

  const handleTeacherTypeChange = (nextType) => {
    const meta = getTeacherTypeMeta(nextType);
    setTeacherType(nextType);
    setHasClasses(meta.defaultHasClasses);
    setSubstituteExempt(meta.defaultSubstituteExempt);
    if (!editingId) {
      if (!position || DEFAULT_POSITIONS.has(position)) setPosition(meta.defaultPosition);
      if (!subject || DEFAULT_SUBJECTS.has(subject)) setSubject(meta.defaultSubject);
    }
  };

  const handleStartEdit = (t) => {
    const effType = effectiveTeacherType(t);
    setEditingId(t.id);
    setName(t.name || "");
    setUsername(t.username || "");
    setTeacherType(effType);
    setHasClasses(hasRegularClasses(t));
    setSubstituteExempt(isSubstituteExcluded(t));
    setSubject(t.subject || "");
    setPosition(t.position || "평교사");
    setHomeroomClass(t.homeroomClass || "");
    setDefaultRoom(t.defaultRoom || "");
    setTransferDate(t.transferDate || "2026-03-01");
    setItinerantDaysText((t.itinerantDays || []).join(","));
  };

  const handleSaveTeacher = async (e) => {
    e.preventDefault();
    if (!name.trim() || !username.trim()) return;
    const itinerantDays = itinerantDaysText
      .split(",")
      .map((s) => Number(s.trim()))
      .filter((n) => n >= 1 && n <= 5);
    const prevTeacher = editingId ? facultyTeachers.find((t) => t.id === editingId) : null;
    try {
      await apiFetch(serverUrl, "/api/teachers", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacher: {
            ...(prevTeacher || {}),
            id: editingId || `t-${username.trim()}`,
            username: username.trim(),
            name: name.trim(),
            teacherType,
            hasClasses,
            substituteExempt,
            position: position.trim() || getTeacherTypeMeta(teacherType).defaultPosition,
            subject: subject.trim() || getTeacherTypeMeta(teacherType).defaultSubject,
            defaultRoom: defaultRoom.trim(),
            homeroomClass: homeroomClass.trim(),
            transferDate,
            itinerantDays,
            roles: prevTeacher?.roles || ["TEACHER"],
          },
          password: editingId ? "" : "1234",
        }),
      });
      onNotify(
        editingId ? "교원 정보 수정 완료" : "교사 명렬표 반영 완료",
        editingId
          ? `${name} 선생님의 교원 유형 및 기초 정보가 갱신되었습니다.`
          : `${name} 선생님이 등록되었습니다 (최초 로그인 시 기본 비밀번호 변경 안내).`
      );
      resetForm();
      onRefresh();
    } catch (err) {
      onNotify("교사 저장 실패", err.message, true);
    }
  };

  const handleImportTeachersXLSX = async (file) => {
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    try {
      const res = await apiFetch(serverUrl, "/api/teachers/import-xlsx", user.id, { method: "POST", body: form });
      onNotify("교사 명렬표 엑셀 업로드 완료", `총 ${res.importedCount}명의 교원 정보가 반영되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("명렬표 업로드 실패", err.message, true);
    }
  };

  const handleDeleteTeacher = async (tid, tname) => {
    try {
      await apiFetch(serverUrl, `/api/teachers?id=${encodeURIComponent(tid)}`, user.id, { method: "DELETE" });
      if (editingId === tid) resetForm();
      onNotify("교사 삭제 완료", `${tname} 선생님이 명렬표에서 삭제되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("교사 삭제 실패", err.message, true);
    }
  };

  const handleResetPassword = async (tid, tname) => {
    try {
      await apiFetch(serverUrl, "/api/auth/reset-password", user.id, {
        method: "POST",
        body: JSON.stringify({ teacherId: tid }),
      });
      onNotify("비밀번호 초기화 완료", `${tname} 계정의 비밀번호가 초기값(1234)으로 재설정되었습니다.`);
    } catch (err) {
      onNotify("비밀번호 초기화 실패", err.message, true);
    }
  };

  return (
    <div className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
        <h3 style={{ margin: 0, fontSize: 15 }}>
          <UserPlus size={15} style={{ verticalAlign: "middle" }} /> 교사·비교과·교과교실 기초 명렬표 ({facultyTeachers.length}명)
        </h3>
        <div style={{ display: "flex", gap: 8 }}>
          <button
            type="button"
            className="btn btn-sm"
            onClick={() =>
              downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=teachers", "교사명렬표.xlsx")
                .catch((err) => onNotify("명렬표 다운로드 실패", err.message, true))
            }
          >
            <Download size={13} /> 교사 명렬표 엑셀 (.xlsx)
          </button>
          <label className="btn btn-sm btn-primary" style={{ cursor: "pointer" }}>
            <Upload size={13} /> 명렬표 엑셀 일괄 업로드
            <input type="file" accept=".xlsx" style={{ display: "none" }} onChange={(e) => handleImportTeachersXLSX(e.target.files?.[0])} />
          </label>
        </div>
      </div>

      <form onSubmit={handleSaveTeacher} style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 14, padding: 10, borderRadius: 10, background: "#f8fafc", border: "1px solid #e2e8f0" }}>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(125px, 1fr))", gap: 8 }}>
          <input className="input" placeholder="성명 (예: 박새별)" value={name} onChange={(e) => setName(e.target.value)} required />
          <input className="input" placeholder="아이디 (예: teacher01)" value={username} onChange={(e) => setUsername(e.target.value)} disabled={Boolean(editingId)} required />
          <select className="select" value={teacherType} onChange={(e) => handleTeacherTypeChange(e.target.value)}>
            {TEACHER_TYPES.map((tp) => (
              <option key={tp.value} value={tp.value}>{tp.label}</option>
            ))}
          </select>
          <input className="input" placeholder="담당과목 (예: 수학/보건)" value={subject} onChange={(e) => setSubject(e.target.value)} required />
          <input className="input" placeholder="직위 (예: 평교사)" value={position} onChange={(e) => setPosition(e.target.value)} />
          <input className="input" placeholder="담임학급 (예: 1-3)" value={homeroomClass} onChange={(e) => setHomeroomClass(e.target.value)} />
          <input className="input" placeholder="전용교과교실" value={defaultRoom} onChange={(e) => setDefaultRoom(e.target.value)} />
          <input className="input" type="date" value={transferDate} onChange={(e) => setTransferDate(e.target.value)} title="현임교 전입일" />
          <input className="input" placeholder="순회요일 (예: 1,3,5)" value={itinerantDaysText} onChange={(e) => setItinerantDaysText(e.target.value)} />
        </div>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap", fontSize: 12.5 }}>
            <label style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
              <input type="checkbox" checked={hasClasses} onChange={(e) => setHasClasses(e.target.checked)} />
              정규 수업 담당 (시간표 자동 편성 포함)
            </label>
            <label style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
              <input type="checkbox" checked={substituteExempt} onChange={(e) => setSubstituteExempt(e.target.checked)} />
              결보강 대타 후보에서 제외
            </label>
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            {editingId && (
              <button type="button" className="btn btn-sm" onClick={resetForm}>
                <X size={13} /> 편집 취소
              </button>
            )}
            <button type="submit" className="btn btn-sm btn-primary">
              {editingId ? "교원 정보 수정 저장" : "교사 등록/갱신"}
            </button>
          </div>
        </div>
      </form>

      <div style={{ maxHeight: 260, overflowY: "auto" }}>
        <table className="table">
          <thead>
            <tr><th>성명 (아이디)</th><th>구분 · 과목</th><th>담임 / 전용실</th><th>수업 · 대타</th><th>전입일 · 순회</th><th>관리</th></tr>
          </thead>
          <tbody>
            {facultyTeachers.map((t) => {
              const effType = effectiveTeacherType(t);
              const meta = getTeacherTypeMeta(effType);
              const clsOn = hasRegularClasses(t);
              const subEx = isSubstituteExcluded(t);
              return (
                <tr key={t.id}>
                  <td><b>{t.name}</b> <span style={{ fontSize: 11.5, color: "#64748b" }}>({t.username})</span></td>
                  <td>
                    <span className={`badge ${effType === "SUBJECT" ? "badge-blue" : effType === "SPECIAL" ? "badge-amber" : "badge-gray"}`} style={{ marginRight: 5 }}>
                      {meta.shortLabel}
                    </span>
                    {t.subject}
                  </td>
                  <td>{t.homeroomClass || "-"} / {t.defaultRoom || "학급교실"}</td>
                  <td>
                    <span className={`badge ${clsOn ? "badge-green" : "badge-gray"}`} style={{ marginRight: 4 }}>
                      {clsOn ? "수업있음" : "수업없음"}
                    </span>
                    {subEx && <span className="badge badge-red">대타제외</span>}
                  </td>
                  <td>{t.transferDate || "-"} · {t.itinerantDays?.length ? t.itinerantDays.join(",") : "전일"}</td>
                  <td style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                    <button type="button" className="btn btn-sm" onClick={() => handleStartEdit(t)}>
                      <Pencil size={12} /> 수정
                    </button>
                    {hasRole(user, "MASTER") && (
                      <AppTooltip content="비밀번호를 초기값(1234)으로 재설정합니다">
                        <button type="button" className="btn btn-sm" onClick={() => setConfirmTarget({ type: "reset-pw", id: t.id, name: t.name })}>
                          <KeyRound size={12} /> 비번초기화
                        </button>
                      </AppTooltip>
                    )}
                    {t.id !== user.id && hasRole(user, "MASTER") && (
                      <button type="button" className="btn btn-sm btn-danger" onClick={() => setConfirmTarget({ type: "delete", id: t.id, name: t.name })}>
                        <Trash2 size={12} /> 삭제
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {showSystemAdminSection && systemAdmin && hasRole(user, "MASTER") && (
        <div style={{ marginTop: 12, padding: "10px 12px", borderRadius: 10, background: "#f8fafc", border: "1px dashed #cbd5e1", display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
          <div style={{ fontSize: 12.5, color: "#475569", display: "flex", alignItems: "center", gap: 6 }}>
            <ShieldAlert size={15} color="#3b5bdb" />
            <span>
              <b>시스템 전용 계정:</b> 최고관리자 (<code>{systemAdmin.username}</code>) — 교원 명렬표·시간표·결보강·시험감독 자동 배정에서 완전 분리됩니다.
            </span>
          </div>
          <button type="button" className="btn btn-sm" onClick={() => setConfirmTarget({ type: "reset-pw", id: systemAdmin.id, name: "최고관리자" })}>
            <KeyRound size={12} /> 시스템 계정 비번 초기화
          </button>
        </div>
      )}

      <ConfirmDialog
        open={Boolean(confirmTarget)}
        onOpenChange={(open) => !open && setConfirmTarget(null)}
        danger={confirmTarget?.type === "delete"}
        title={confirmTarget?.type === "delete" ? "교사 계정 삭제 확인" : "비밀번호 초기화 확인"}
        description={
          confirmTarget?.type === "delete"
            ? `${confirmTarget?.name} 선생님을 교사 명렬표에서 삭제하시겠습니까?\n기존 시간표/시험감독 배정에 영향이 있을 수 있습니다.`
            : `${confirmTarget?.name} 계정의 비밀번호를 초기값(1234)으로 재설정하시겠습니까?\n기존 로그인 세션이 만료되며 다음 로그인 시 변경 안내가 표시됩니다.`
        }
        confirmLabel={confirmTarget?.type === "delete" ? "삭제 실행" : "비밀번호 초기화"}
        onConfirm={async () => {
          const target = confirmTarget;
          setConfirmTarget(null);
          if (!target) return;
          if (target.type === "delete") {
            await handleDeleteTeacher(target.id, target.name);
          } else {
            await handleResetPassword(target.id, target.name);
          }
        }}
      />
    </div>
  );
}
