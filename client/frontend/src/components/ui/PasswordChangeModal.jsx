import React, { useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { KeyRound, ShieldCheck } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

// PasswordChangeModal은 최초 로그인 시 초기 비밀번호("1234") 즉시 변경 안내 및
// 일반 사용자의 상시 비밀번호 변경을 처리하는 Radix Dialog 모달입니다.
export default function PasswordChangeModal({ open, onOpenChange, serverUrl, user, forced, onChanged }) {
  const [currentPassword, setCurrentPassword] = useState(forced ? "1234" : "");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  if (!user) return null;

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError("");
    if (newPassword.trim().length < 4) {
      setError("새 비밀번호는 최소 4자 이상 입력해 주세요.");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("새 비밀번호 확인이 일치하지 않습니다.");
      return;
    }
    if (newPassword === "1234") {
      setError("보안을 위해 초기 비밀번호(1234)가 아닌 다른 비밀번호를 설정해 주세요.");
      return;
    }

    setSaving(true);
    try {
      const updatedUser = await apiFetch(serverUrl, "/api/auth/change-password", user.id, {
        method: "POST",
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      setNewPassword("");
      setConfirmPassword("");
      onChanged(updatedUser);
      onOpenChange(false);
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={(v) => !forced && onOpenChange(v)}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content" style={{ maxWidth: 450 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
            <div className="kpi-icon" style={{ background: "#edf2ff", color: "#3b5bdb" }}>
              <KeyRound size={20} />
            </div>
            <div>
              <Dialog.Title style={{ margin: 0, fontSize: 17, fontWeight: 800 }}>
                {forced ? "최초 로그인 비밀번호 변경 권장" : "비밀번호 변경"}
              </Dialog.Title>
              <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>
                {user.name} 선생님 ({user.username})
              </div>
            </div>
          </div>

          {forced && (
            <div style={{ padding: "10px 12px", borderRadius: 9, background: "#fff9db", color: "#9c4d00", fontSize: 12.5, marginBottom: 12 }}>
              초기 비밀번호(<b>1234</b>)로 로그인하셨습니다. 보안을 위해 지금 본인만의 새 비밀번호로 변경한 뒤 시작해 주세요.
            </div>
          )}

          <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>현재 비밀번호</label>
              <input className="input" type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} required />
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>새 비밀번호 (4자 이상)</label>
              <input className="input" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} required autoFocus />
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>새 비밀번호 확인</label>
              <input className="input" type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} required />
            </div>

            {error && <div className="badge badge-red" style={{ padding: "8px 10px", borderRadius: 8 }}>{error}</div>}

            <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 6 }}>
              {forced ? (
                <button type="button" className="btn btn-sm" onClick={() => onOpenChange(false)}>
                  나중에 변경 (건너뛰기)
                </button>
              ) : (
                <Dialog.Close asChild>
                  <button type="button" className="btn">취소</button>
                </Dialog.Close>
              )}
              <button type="submit" className="btn btn-primary" disabled={saving}>
                <ShieldCheck size={15} /> {saving ? "저장 중..." : "새 비밀번호로 변경"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
