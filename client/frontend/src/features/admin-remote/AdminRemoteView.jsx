import React, { useEffect, useMemo, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { AlertTriangle, CloudDownload, Database, RefreshCw, RotateCcw, Shield, UploadCloud, UserCog, Users } from "lucide-react";
import { apiFetch, applySelfUpdateBridge, isFacultyTeacher, isSystemAdminAccount } from "../../events/eventStore";
import ConfirmDialog from "../../components/ui/ConfirmDialog";
import { StatusDot } from "../../components/ui/SmartVisualKit";
import TeacherManagerCard from "./TeacherManagerCard";
import RoleMatrixPanel from "./RoleMatrixPanel";

// AdminRemoteView는 최고관리자(MASTER)의 교사 관리, RBAC 권한 매트릭스·인계,
// 전체 시스템 초기화, 원격 DB 암호화 백업·복원 및 GitHub 릴리즈 기반 무중단 자가 업데이트를 담당합니다.
export default function AdminRemoteView({ serverUrl, serverVersion = "", user, teachers, onRefresh, onResetComplete, onNotify }) {
  const [adminTab, setAdminTab] = useState("teachers");
  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const systemAdmin = useMemo(() => (teachers || []).find(isSystemAdminAccount), [teachers]);
  const roleTargets = useMemo(() => (systemAdmin ? [...facultyTeachers, systemAdmin] : facultyTeachers), [facultyTeachers, systemAdmin]);
  const [backups, setBackups] = useState([]);
  const [confirmRestoreFile, setConfirmRestoreFile] = useState("");
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

  const handleCreateBackup = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/system/backups", user.id, { method: "POST" });
      onNotify("AES-256 암호화 백업 완료", `백업 파일 생성: ${res.backupFile}`);
      loadBackups();
    } catch (err) {
      onNotify("백업 실패", err.message, true);
    }
  };

  const handleRestoreBackup = async (fileName) => {
    try {
      const res = await apiFetch(serverUrl, "/api/system/backups/restore", user.id, {
        method: "POST",
        body: JSON.stringify({ fileName }),
      });
      setConfirmRestoreFile("");
      onNotify("암호화 백업 복원 완료", `${res.fileName}에서 ${res.restoredCount}건이 복원되었습니다. 보안을 위해 다시 로그인해 주세요.`, true);
      if (onResetComplete) onResetComplete();
    } catch (err) {
      onNotify("백업 복원 실패", err.message, true);
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
    form.append("version", serverVersion || "0.1.01");
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

  const resetTitle = confirmResetMode === "FULL_FACTORY_RESET" ? "전체 초기 설정 복원 확인" : "신학기 데이터 초기화 확인";
  const resetDescription =
    confirmResetMode === "FULL_FACTORY_RESET"
      ? "최고관리자(admin) 계정만 남기고 모든 교사, 시간표, 시험감독, 행사 데이터를 삭제합니다.\n실행 직전 backups/ 폴더에 AES-256 암호화 백업이 자동 생성되며 기존 로그인 세션은 종료됩니다."
      : "교사 명렬표 및 학교 기본설정은 유지하고, 시간표·시수·시험감독·행사 데이터만 비웁니다.\n실행 직전 backups/ 폴더에 AES-256 암호화 백업이 자동 생성됩니다.";

  return (
    <Tabs.Root value={adminTab} onValueChange={setAdminTab} style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="card" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <h2 style={{ margin: 0, fontSize: 17 }}>시스템 · 교사 · 권한 · 백업 제어</h2>
          <span className="badge badge-blue" style={{ fontVariantNumeric: "tabular-nums" }}>교원 {facultyTeachers.length}명</span>
          <span className="badge badge-gray" style={{ fontVariantNumeric: "tabular-nums" }}>암호화 백업 {backups.length}개</span>
        </div>
        <Tabs.List className="radix-tabs-list" aria-label="시스템 관리 영역">
          <Tabs.Trigger value="teachers" className="radix-tabs-trigger"><Users size={14} /> 교사 명렬표</Tabs.Trigger>
          <Tabs.Trigger value="roles" className="radix-tabs-trigger"><UserCog size={14} /> 권한 매트릭스·인계</Tabs.Trigger>
          <Tabs.Trigger value="backup" className="radix-tabs-trigger"><Database size={14} /> 백업·초기화</Tabs.Trigger>
          <Tabs.Trigger value="update" className="radix-tabs-trigger"><UploadCloud size={14} /> 업데이트·배포</Tabs.Trigger>
        </Tabs.List>
      </div>

      <Tabs.Content value="teachers">
        <TeacherManagerCard serverUrl={serverUrl} user={user} teachers={teachers} onRefresh={onRefresh} onNotify={onNotify} showSystemAdminSection />
      </Tabs.Content>

      <Tabs.Content value="roles">
        <RoleMatrixPanel
          serverUrl={serverUrl}
          user={user}
          facultyTeachers={facultyTeachers}
          systemAdmin={systemAdmin}
          roleTargets={roleTargets}
          onRefresh={onRefresh}
          onNotify={onNotify}
        />
      </Tabs.Content>

      <Tabs.Content value="backup" style={{ display: "flex", flexDirection: "column", gap: 16 }}>
        <div className="card" style={{ borderColor: "#ffa8a8", background: "#fff8f8" }}>
          <h3 style={{ margin: "0 0 8px", fontSize: 15, color: "#c92a2a" }}>
            <AlertTriangle size={16} style={{ verticalAlign: "middle" }} /> 시스템 데이터 초기화 (자동 암호화 백업 후 실행)
          </h3>
          <div style={{ fontSize: 12.5, color: "#475569", marginBottom: 12 }}>
            모든 초기화 작업은 실행 직전 <code>backups/</code> 폴더에 AES-256 암호화 백업본을 자동 생성한 뒤 안전하게 실행됩니다.
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <button type="button" className="btn btn-sm" onClick={() => setConfirmResetMode("ACADEMIC_TERM_RESET")}>
              1. 신학기 초기화 (교사 명렬표 유지 · 시간표/시험/행사 비우기)
            </button>
            <button type="button" className="btn btn-sm btn-danger" onClick={() => setConfirmResetMode("FULL_FACTORY_RESET")}>
              2. 전체 초기 설정 복원 (최고관리자 계정만 남기고 모두 삭제)
            </button>
          </div>
        </div>

        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>
              <Database size={15} style={{ verticalAlign: "middle" }} /> 원격 DB AES-256 암호화 백업
            </h3>
            <button type="button" className="btn btn-sm btn-green" onClick={handleCreateBackup}>지금 암호화 백업 생성</button>
          </div>
          {backups.length === 0 ? (
            <div style={{ fontSize: 13, color: "#64748b" }}>생성된 백업 파일이 아직 없습니다.</div>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {backups.slice(0, 10).map((b) => (
                <div key={b} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8, padding: "6px 8px", borderRadius: 8, background: "#f8fafc", border: "1px solid #e2e8f0", fontSize: 12 }}>
                  <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{b}</span>
                  <button type="button" className="btn btn-sm" style={{ flexShrink: 0 }} onClick={() => setConfirmRestoreFile(b)}>
                    <RotateCcw size={12} /> 복원
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </Tabs.Content>

      <Tabs.Content value="update">
        <div className="card">
          <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
            <UploadCloud size={15} style={{ verticalAlign: "middle" }} /> GitHub 릴리즈 & 무중단 자가 업데이트
          </h3>
          <div style={{ display: "flex", flexDirection: "column", gap: 10, fontSize: 12.5 }}>
            <div style={{ padding: 10, borderRadius: 8, background: "#f8fafc", border: "1px solid #cbd5e1", display: "flex", flexDirection: "column", gap: 8 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 6 }}>
                <span><b>저장소:</b> github.com/purnpado/kkomi_timetable {serverVersion ? `(현재 v${serverVersion})` : ""}</span>
                {ghRelease && (
                  <span className={`badge ${ghRelease.hasUpdate ? "badge-amber" : "badge-green"}`}>
                    <StatusDot tone={ghRelease.hasUpdate ? "amber" : "green"} label={`최신: ${ghRelease.tagName} ${ghRelease.hasUpdate ? "(업데이트 가능)" : "(최신 상태)"}`} />
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
              <b>수동 1. 메인 서버 교체 (<code>꼬미시간서버.exe</code>)</b>: 업로드 시 자동 DB 백업 후 재시작
              <input type="file" style={{ display: "block", marginTop: 4 }} onChange={(e) => handleUploadBinary("/api/system/server-update", e.target.files?.[0])} />
            </div>
            <div>
              <b>수동 2. 교사용 클라이언트 패치 배포 (<code>꼬미시간.exe</code>)</b>: 전 교사 PC 자동 패치 알림
              <input type="file" style={{ display: "block", marginTop: 4 }} onChange={(e) => handleUploadBinary("/api/system/client-patch", e.target.files?.[0])} />
            </div>
            <button
              type="button"
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
      </Tabs.Content>

      <ConfirmDialog
        open={Boolean(confirmResetMode)}
        onOpenChange={(open) => !open && setConfirmResetMode("")}
        danger
        title={resetTitle}
        description={resetDescription}
        confirmLabel="예, 초기화 실행"
        onConfirm={() => handleSystemReset(confirmResetMode)}
      />

      <ConfirmDialog
        open={Boolean(confirmRestoreFile)}
        onOpenChange={(open) => !open && setConfirmRestoreFile("")}
        danger
        title="암호화 백업 복원 확인"
        description={`선택한 백업 파일(${confirmRestoreFile})로 전체 데이터를 복원하시겠습니까?\n복원 직후 보안을 위해 모든 사용자의 로그인 세션이 종료됩니다.`}
        confirmLabel="복원 실행"
        onConfirm={() => handleRestoreBackup(confirmRestoreFile)}
      />
    </Tabs.Root>
  );
}
