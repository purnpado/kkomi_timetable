import React, { useState } from "react";
import { KeyRound, Server, ShieldCheck } from "lucide-react";
import { apiFetch, connectDesktopBridge } from "../../events/eventStore";

const QUICK_ACCOUNTS = [
  { label: "김교무 (시스템총괄·시간표)", username: "master", badge: "마스터" },
  { label: "이수업 (시간표 담당)", username: "timetable", badge: "수업계" },
  { label: "박평가 (교내시험감독 담당)", username: "internal", badge: "평가계" },
  { label: "최외부 (외부시험감독 담당)", username: "external", badge: "대외계" },
  { label: "정행사 (행사 담당)", username: "event", badge: "행사계" },
  { label: "강국어 (일반 담임교사 1-2)", username: "t06", badge: "담임" },
];

// LoginView는 교무실 공용 서버 IP 연결 및 교사 로그인 / 역할별 빠른 체험 전환을 제공합니다.
export default function LoginView({ initialServerUrl, onLoginSuccess }) {
  const [serverUrl, setServerUrl] = useState(initialServerUrl || "http://127.0.0.1:18090");
  const [username, setUsername] = useState("master");
  const [password, setPassword] = useState("1234");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const performLogin = async (u, p) => {
    setError("");
    setLoading(true);
    try {
      const normalized = serverUrl.startsWith("http") ? serverUrl : `http://${serverUrl}`;
      const user = await apiFetch(normalized, "/api/auth/login", "", {
        method: "POST",
        body: JSON.stringify({ username: u, password: p }),
      });
      await connectDesktopBridge(normalized, user.id);
      onLoginSuccess(normalized, user);
    } catch (err) {
      setError(err.message || "서버 연결 또는 로그인에 실패했습니다.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{ minHeight: "100vh", display: "grid", placeItems: "center", background: "linear-gradient(135deg, #eef2ff 0%, #f8fafc 100%)", padding: 20 }}>
      <div className="card" style={{ width: "min(520px, 95vw)", padding: 28 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 18 }}>
          <div className="brand-logo" style={{ width: 44, height: 44, fontSize: 20 }}>🐾</div>
          <div>
            <h1 style={{ margin: 0, fontSize: 21, fontWeight: 800 }}>꼬미시간 (KkomiTimetable)</h1>
            <div style={{ fontSize: 12.5, color: "#64748b" }}>중·고등학교 학사 시간표 · 내/외부 시험감독 · 행사 통합 솔루션</div>
          </div>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            performLogin(username, password);
          }}
          style={{ display: "flex", flexDirection: "column", gap: 12 }}
        >
          <div>
            <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>
              <Server size={12} style={{ verticalAlign: "middle" }} /> 교무실 메인 서버 주소 (업무망 LAN)
            </label>
            <input className="input" value={serverUrl} onChange={(e) => setServerUrl(e.target.value)} placeholder="예: http://192.168.0.10:18090" />
          </div>

          <div className="grid-2">
            <div>
              <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>교사 아이디</label>
              <input className="input" value={username} onChange={(e) => setUsername(e.target.value)} />
            </div>
            <div>
              <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>
                <KeyRound size={12} style={{ verticalAlign: "middle" }} /> 비밀번호
              </label>
              <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </div>
          </div>

          {error && <div className="badge badge-red" style={{ padding: "8px 12px", borderRadius: 8 }}>{error}</div>}

          <button type="submit" className="btn btn-primary" style={{ justifyContent: "center", padding: "11px 16px" }} disabled={loading}>
            <ShieldCheck size={16} /> {loading ? "연결 중..." : "업무망 서버 접속 및 로그인"}
          </button>
        </form>

        <div style={{ marginTop: 20, borderTop: "1px solid #e2e8f0", paddingTop: 14 }}>
          <div style={{ fontSize: 12, fontWeight: 700, color: "#475569", marginBottom: 8 }}>
            역할별 빠른 전환 (초기 비밀번호: 1234)
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {QUICK_ACCOUNTS.map((acc) => (
              <button
                key={acc.username}
                type="button"
                className="btn btn-sm"
                onClick={() => {
                  setUsername(acc.username);
                  setPassword("1234");
                  performLogin(acc.username, "1234");
                }}
              >
                <span className="badge badge-blue">{acc.badge}</span> {acc.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
