import React, { useEffect, useState } from "react";
import { Shield, UserCog } from "lucide-react";
import { apiFetch, ROLE_LABELS } from "../../events/eventStore";

const ALL_ROLES = ["MASTER", "TIMETABLE_ADMIN", "INTERNAL_EXAM_ADMIN", "EXTERNAL_EXAM_ADMIN", "EVENT_ADMIN", "TEACHER"];

// RoleMatrixPanel은 전 교원 × 6종 RBAC 권한 체크 매트릭스와 학년도 업무분장 인계 기능을 제공합니다.
export default function RoleMatrixPanel({ serverUrl, user, facultyTeachers, systemAdmin, roleTargets, onRefresh, onNotify }) {
  const [handoverFrom, setHandoverFrom] = useState("");
  const [handoverTo, setHandoverTo] = useState("");
  const [handoverRole, setHandoverRole] = useState("TIMETABLE_ADMIN");

  useEffect(() => {
    if (roleTargets.length > 0 && !roleTargets.some((t) => t.id === handoverFrom)) {
      setHandoverFrom(roleTargets[0].id);
    }
    if (facultyTeachers.length > 0 && !facultyTeachers.some((t) => t.id === handoverTo)) {
      setHandoverTo((facultyTeachers[1] || facultyTeachers[0]).id);
    }
  }, [roleTargets, facultyTeachers, handoverFrom, handoverTo]);

  const toggleRole = async (teacher, role) => {
    const current = teacher.roles || ["TEACHER"];
    if (teacher.username === "admin" && role === "MASTER" && current.includes("MASTER")) {
      onNotify("변경 불가", "시스템 기본 최고관리자(admin) 계정의 최고관리자 권한은 해제할 수 없습니다.", true);
      return;
    }
    const next = current.includes(role) ? current.filter((r) => r !== role) : [...current, role];
    try {
      await apiFetch(serverUrl, "/api/teachers/roles", user.id, {
        method: "POST",
        body: JSON.stringify({ teacherId: teacher.id, roles: next }),
      });
      const targetLabel = teacher.username === "admin" ? "최고관리자" : `${teacher.name} 선생님`;
      onNotify("권한 변경 완료", `${targetLabel}의 RBAC 업무분장이 업데이트되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("권한 변경 실패", err.message, true);
    }
  };

  const handleHandover = async () => {
    if (!handoverFrom || !handoverTo) return;
    try {
      await apiFetch(serverUrl, "/api/teachers/handover", user.id, {
        method: "POST",
        body: JSON.stringify({ fromTeacherId: handoverFrom, toTeacherId: handoverTo, role: handoverRole }),
      });
      onNotify("업무분장 인계 완료", "선택한 관리자 권한이 후임 교사에게 안전하게 이관되었습니다.");
      onRefresh();
    } catch (err) {
      onNotify("인계 실패", err.message, true);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 10 }}>
          <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
            <UserCog size={16} color="#3b5bdb" /> 전 교원 RBAC 권한 체크 매트릭스
          </h3>
          <span className="badge badge-gray" style={{ fontVariantNumeric: "tabular-nums" }}>총 {roleTargets.length}계정</span>
        </div>
        <div style={{ maxHeight: 360, overflowY: "auto" }}>
          <table className="table">
            <thead>
              <tr>
                <th>성명 · 직위</th>
                {ALL_ROLES.map((r) => <th key={r} style={{ textAlign: "center", fontSize: 11.5 }}>{ROLE_LABELS[r] || r}</th>)}
                <th style={{ textAlign: "center" }}>겸임 수</th>
              </tr>
            </thead>
            <tbody>
              {roleTargets.map((t) => {
                const roles = t.roles || ["TEACHER"];
                const isSys = systemAdmin && t.id === systemAdmin.id;
                return (
                  <tr key={t.id} style={isSys ? { background: "#f8fafc" } : undefined}>
                    <td>
                      <b>{isSys ? "최고관리자 (admin)" : t.name}</b>{" "}
                      <span style={{ fontSize: 11.5, color: "#64748b" }}>({isSys ? "시스템 전용" : `${t.subject || "-"} · ${t.position || "교사"}`})</span>
                    </td>
                    {ALL_ROLES.map((role) => {
                      const active = roles.includes(role);
                      const locked = t.username === "admin" && role === "MASTER";
                      return (
                        <td key={role} style={{ textAlign: "center" }}>
                          <input
                            type="checkbox"
                            checked={active}
                            disabled={locked}
                            aria-label={`${t.name} ${ROLE_LABELS[role] || role}`}
                            onChange={() => toggleRole(t, role)}
                          />
                        </td>
                      );
                    })}
                    <td style={{ textAlign: "center" }}>
                      <span className="badge badge-blue" style={{ fontVariantNumeric: "tabular-nums" }}>
                        <Shield size={11} /> {roles.length}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>학년도 업무분장 권한 승계 (인계)</h3>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))", gap: 8 }}>
          <select className="select" value={handoverFrom} onChange={(e) => setHandoverFrom(e.target.value)}>
            {roleTargets.map((t) => (
              <option key={t.id} value={t.id}>기존: {t.username === "admin" ? "최고관리자 (시스템)" : t.name}</option>
            ))}
          </select>
          <select className="select" value={handoverTo} onChange={(e) => setHandoverTo(e.target.value)}>
            {facultyTeachers.length === 0 ? <option value="">후임 교원 없음</option> : facultyTeachers.map((t) => <option key={t.id} value={t.id}>후임: {t.name}</option>)}
          </select>
          <select className="select" value={handoverRole} onChange={(e) => setHandoverRole(e.target.value)}>
            {ALL_ROLES.filter((r) => r !== "TEACHER").map((r) => <option key={r} value={r}>{ROLE_LABELS[r]}</option>)}
          </select>
          <button type="button" className="btn btn-primary" onClick={handleHandover}>권한 즉시 인계</button>
        </div>
      </div>
    </div>
  );
}
