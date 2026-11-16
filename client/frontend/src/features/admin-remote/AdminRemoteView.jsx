import React, { useEffect, useState } from "react";
import { AlertTriangle, CloudDownload, Database, RefreshCw, Shield, UploadCloud, UserCog } from "lucide-react";
import { apiFetch, applySelfUpdateBridge, ROLE_LABELS } from "../../events/eventStore";
import TeacherManagerCard from "./TeacherManagerCard";

const ALL_ROLES = ["MASTER", "TIMETABLE_ADMIN", "INTERNAL_EXAM_ADMIN", "EXTERNAL_EXAM_ADMIN", "EVENT_ADMIN", "TEACHER"];

// AdminRemoteView는 시스템 총괄(마스터)의 교사 관리, RBAC 권한 배정,
// 전체 시스템 초기화, 원격 DB 암호화 백업 및 GitHub 릴리즈 기반 무중단 자가 업데이트를 담당합니다.
export default function AdminRemoteView({ serverUrl, user, teachers, onRefresh, onResetComplete, onNotify }) {
  const [backups, setBackups] = useState([]);
  const [selectedTeacherId, setSelectedTeacherId] = useState(teachers?.[0]?.id || "t01");
  const [handoverFrom, setHandoverFrom] = useState("t02");
  const [handoverTo, setHandoverTo] = useState("t06");
  const [handoverRole, setHandoverRole] = useState("TIMETABLE_ADMIN");
  const [confirmResetMode, setConfirmResetMode] = useState("");
  const [ghRelease, setGhRelease] = useState(null);
  const [ghBusy, setGhBusy] = useState(false);

  const loadBackups = async () => {
    try {
      const list = await apiFetch(serverUrl, "/api/system/backups", user.id);
      setBackups(list || []);
    } catch {
      setBackups([]);
    }
  };

  useEffect(() => { loadBackups(); }, [user?.id]);

  const selectedTeacher = (teachers || []).find((t) => t.id === selectedTeacherId);

  const toggleRole = async (role) => {
    if (!selectedTeacher) return;
    const current = selectedTeacher.roles || ["TEACHER"];
    const next = current.includes(role) ? current.filter((r) => r !== role) : [...current, role];
    try {
      await apiFetch(serverUrl, "/api/teachers/roles", user.id, {
        method: "POST",
        body: JSON.stringify({ teacherId: selectedTeacher.id, roles: next }),
      });
      onNotify("권한 변경 완료", `${selectedTeacher.name} 선생님의 RBAC 업무분장이 업데이트되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("권한 변경 실패", err.message, true);
    }
  };

  const handleHandover = async () => {
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

  const handleCreateBackup = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/system/backups", user.id, { method: "POST" });
      onNotify("AES-256 암호화 백업 완료", `백업 파일 생성: ${res.backupFile}`);
      loadBackups();
    } catch (err) {
      onNotify("백업 실패", err.message, true);
    }
  };

  const handleSystemReset = async (mode) => {
    try {
      const res = await apiFetch(serverUrl, "/api/system/reset", user.id, {
        method: "POST",
        body: JSON.stringify({ mode }),
      });
      setConfirmResetMode("");
      loadBackups();
      onNotify("시스템 초기화 완료", `초기화 전 자동 백업이 저장되었습니다 (${res.backupFile || "완료"}).`, true);
      if (mode === "FULL_FACTORY_RESET" && onResetComplete) {
        onResetComplete();
      } else {
        onRefresh();
      }
    } catch (err) {
      onNotify("초기화 실패", err.message, true);
    }
  };

  const handleUploadBinary = async (endpoint, file) => {
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    form.append("version", "0.1.01");
    try {
      const res = await apiFetch(serverUrl, endpoint, user.id, { method: "POST", body: form });
      onNotify("바이너리 원격 업데이트 전달", res.status || "패치가 등록되었습니다.", true);
    } catch (err) {
      onNotify("업로드 실패", err.message, true);
    }
  };

  const handleCheckGitHub = async () => {
    setGhBusy(true);
    try {
      const info = await apiFetch(serverUrl, "/api/system/github-release", user.id);
      setGhRelease(info);
      onNotify(
        "GitHub 릴리즈 확인 완료",
        info.hasUpdate ? `새 버전 ${info.tagName} 출시됨 (현재 v${info.currentVersion})` : `최신 버전(${info.tagName})을 사용 중입니다.`
      );
    } catch (err) {
      onNotify("GitHub 릴리즈 조회 실패", err.message, true);
    } finally {
      setGhBusy(false);
    }
  };

  const handleApplyGitHub = async (target) => {
    setGhBusy(true);
    try {
      const res = await apiFetch(serverUrl, "/api/system/github-update", user.id, {
        method: "POST",
        body: JSON.stringify({ target }),
      });
      onNotify("GitHub 자동 업데이트 적용", res.status || "업데이트가 반영되었습니다.", true);
    } catch (err) {
      onNotify("GitHub 자동 업데이트 실패", err.message, true);
    } finally {
      setGhBusy(false);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <TeacherManagerCard serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} />

      <div className="grid-2">
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
            <UserCog size={15} style={{ verticalAlign: "middle" }} /> 교사별 RBAC 권한 다중 겸임 설정
          </h3>
          <select className="select" value={selectedTeacherId} onChange={(e) => setSelectedTeacherId(e.target.value)} style={{ marginBottom: 12 }}>
            {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => (
              <option key={t.id} value={t.id}>{t.name} ({t.username} · {t.position})</option>
            ))}
          </select>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {ALL_ROLES.map((role) => {
              const active = selectedTeacher?.roles?.includes(role);
              return (
                <button key={role} className={`btn btn-sm ${active ? "btn-primary" : ""}`} onClick={() => toggleRole(role)}>
                  <Shield size={12} /> {ROLE_LABELS[role] || role}
                </button>
              );
            })}
          </div>
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>학년도 업무분장 권한 승계 (인계)</h3>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div className="grid-2">
              <select className="select" value={handoverFrom} onChange={(e) => setHandoverFrom(e.target.value)}>
                {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => <option key={t.id} value={t.id}>기존: {t.name}</option>)}
              </select>
              <select className="select" value={handoverTo} onChange={(e) => setHandoverTo(e.target.value)}>
                {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => <option key={t.id} value={t.id}>후임: {t.name}</option>)}
              </select>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              <select className="select" value={handoverRole} onChange={(e) => setHandoverRole(e.target.value)}>
                {ALL_ROLES.filter((r) => r !== "TEACHER").map((r) => <option key={r} value={r}>{ROLE_LABELS[r]}</option>)}
              </select>
              <button className="btn btn-primary" onClick={handleHandover}>권한 즉시 인계</button>
            </div>
          </div>
        </div>
      </div>

      <div className="card" style={{ borderColor: "#ffa8a8", background: "#fff8f8" }}>
        <h3 style={{ margin: "0 0 8px", fontSize: 15, color: "#c92a2a" }}>
          <AlertTriangle size={16} style={{ verticalAlign: "middle" }} /> 시스템 데이터 전체 초기화 (자동 암호화 백업 후 실행)
        </h3>
        <div style={{ fontSize: 12.5, color: "#475569", marginBottom: 12 }}>
          모든 초기화 작업은 실행 직전 <code>backups/</code> 폴더에 AES-256 암호화 백업본을 자동 생성한 뒤 안전하게 실행됩니다.
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <button className="btn btn-sm" onClick={() => setConfirmResetMode("ACADEMIC_TERM_RESET")}>
            1. 학기 초기화 (교사 유지 · 시간표/시험/행사 비우기)
          </button>
          <button className="btn btn-sm btn-danger" onClick={() => setConfirmResetMode("FULL_FACTORY_RESET")}>
            2. 전체 완전 초기화 (admin / 1234 계정만 남기고 모두 삭제)
          </button>
          <button className="btn btn-sm" onClick={() => setConfirmResetMode("DEMO_SEED_RESET")}>
            3. 시연용 샘플 데이터로 복원
          </button>
        </div>
        {confirmResetMode && (
          <div style={{ marginTop: 12, padding: 12, borderRadius: 10, background: "#fff", border: "1.5px solid #e03131", display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
            <span style={{ fontSize: 13, fontWeight: 700, color: "#c92a2a" }}>
              선택한 초기화({confirmResetMode})를 정말 실행하시겠습니까?
            </span>
            <div style={{ display: "flex", gap: 8 }}>
              <button className="btn btn-sm" onClick={() => setConfirmResetMode("")}>취소</button>
              <button className="btn btn-sm btn-danger" onClick={() => handleSystemReset(confirmResetMode)}>
                예, 지금 초기화 실행
              </button>
            </div>
          </div>
        )}
      </div>

      <div className="grid-2">
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>
              <Database size={15} style={{ verticalAlign: "middle" }} /> 원격 DB AES-256 암호화 백업
            </h3>
            <button className="btn btn-sm btn-green" onClick={handleCreateBackup}>지금 암호화 백업 생성</button>
          </div>
          {backups.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>생성된 백업 파일이 아직 없습니다.</div>
          ) : (
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12.5 }}>
              {backups.slice(0, 8).map((b) => <li key={b}>{b}</li>)}
            </ul>
          )}
        </div>

        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
            <UploadCloud size={15} style={{ verticalAlign: "middle" }} /> GitHub 릴리즈 & 무중단 자가 업데이트
          </h3>
          <div style={{ display: "flex", flexDirection: "column", gap: 10, fontSize: 12.5 }}>
            <div style={{ padding: 10, borderRadius: 8, background: "#f8fafc", border: "1px solid #cbd5e1", display: "flex", flexDirection: "column", gap: 8 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 6 }}>
                <span><b>저장소:</b> github.com/purnpado/kkomi_timetable (현재 v0.1.01)</span>
                {ghRelease && (
                  <span className={`badge ${ghRelease.hasUpdate ? "badge-amber" : "badge-green"}`}>
                    최신: {ghRelease.tagName} {ghRelease.hasUpdate ? "(업데이트 가능)" : "(최신 상태)"}
                  </span>
                )}
              </div>
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                <button type="button" className="btn btn-sm btn-primary" disabled={ghBusy} onClick={handleCheckGitHub}>
                  <CloudDownload size={13} /> GitHub 최신 릴리즈 확인
                </button>
                <button type="button" className="btn btn-sm btn-green" disabled={ghBusy} onClick={() => handleApplyGitHub("client")}>
                  GitHub에서 클라이언트 패치 배포
                </button>
                <button type="button" className="btn btn-sm" disabled={ghBusy} onClick={() => handleApplyGitHub("server")}>
                  GitHub에서 서버 무중단 업데이트
                </button>
              </div>
            </div>
            <div>
              <b>수동 1. 메인 서버 교체 (`꼬미시간서버.exe`)</b>: 업로드 시 자동 DB 백업 후 재시작
              <input type="file" style={{ display: "block", marginTop: 4 }} onChange={(e) => handleUploadBinary("/api/system/server-update", e.target.files?.[0])} />
            </div>
            <div>
              <b>수동 2. 교사용 클라이언트 패치 배포 (`꼬미시간.exe`)</b>: 전 교사 PC 자동 패치 알림
              <input type="file" style={{ display: "block", marginTop: 4 }} onChange={(e) => handleUploadBinary("/api/system/client-patch", e.target.files?.[0])} />
            </div>
            <button
              className="btn btn-sm"
              style={{ alignSelf: "flex-start" }}
              onClick={async () => {
                try {
                  await applySelfUpdateBridge();
                } catch (err) {
                  onNotify("클라이언트 패치 확인", err?.message || "서버 및 GitHub에 새 패치가 없습니다.");
                }
              }}
            >
              <RefreshCw size={13} /> 내 클라이언트 최신 패치 확인 및 적용 (서버/GitHub)
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
