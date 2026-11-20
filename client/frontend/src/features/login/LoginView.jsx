import React, { useEffect, useMemo, useState } from "react";
import { KeyRound, RefreshCw, Server, ShieldCheck, UserCheck } from "lucide-react";
import { apiFetch, connectDesktopBridge, setAuthToken } from "../../events/eventStore";

const QUICK_ACCOUNTS = [
  { label: "최고관리자 (admin · 서버 전체관리)", username: "admin", badge: "최고관리자" },
  { label: "이수업 (시간표 담당)", username: "timetable", badge: "수업계" },
  { label: "박평가 (교내시험감독 담당)", username: "internal", badge: "평가계" },
  { label: "최외부 (외부시험감독 담당)", username: "external", badge: "대외계" },
  { label: "정행사 (행사 담당)", username: "event", badge: "행사계" },
  { label: "강국어 (일반 담임교사 1-2)", username: "t06", badge: "담임" },
];

// LoginView는 교무실 공용 서버 연결 및 [과목·성명 간편 선택 / 직접 입력(동명이인 구분)] 로그인을 제공합니다.
export default function LoginView({ initialServerUrl, onLoginSuccess }) {
  const [serverUrl, setServerUrl] = useState(initialServerUrl || "http://127.0.0.1:18090");
  const [loginMode, setLoginMode] = useState("select"); // "select" | "manual"
  const [directory, setDirectory] = useState([]);
  const [dirStatus, setDirStatus] = useState("");
  const [isDemoMode, setIsDemoMode] = useState(false);

  const [selectedSubject, setSelectedSubject] = useState(() => localStorage.getItem("kkomi_last_subject") || "ALL");
  const [selectedUsername, setSelectedUsername] = useState(() => localStorage.getItem("kkomi_last_user") || "admin");

  const [manualName, setManualName] = useState("");
  const [manualSubject, setManualSubject] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const normalizedServer = (srv = serverUrl) => (srv.startsWith("http") ? srv : `http://${srv}`).replace(/\/+$/, "");

  const loadDirectory = async (srv = serverUrl) => {
    const base = normalizedServer(srv);
    try {
      const [list, health] = await Promise.all([
        apiFetch(base, "/api/auth/directory", ""),
        apiFetch(base, "/api/health", "").catch(() => null),
      ]);
      setDirectory(list || []);
      setIsDemoMode(Boolean(health?.isDemoMode));
      setDirStatus(`교사 ${list?.length || 0}명 연결됨`);
      if (list && list.length > 0 && !list.some((t) => t.username === selectedUsername)) {
        setSelectedUsername(list[0].username);
      }
    } catch {
      setDirectory([]);
      setIsDemoMode(false);
      setDirStatus("서버 연결 대기 중");
    }
  };

  useEffect(() => {
    loadDirectory(initialServerUrl || serverUrl);
  }, [initialServerUrl]);

  const subjectOptions = useMemo(() => {
    const s = new Set();
    directory.forEach((t) => {
      if (t.subject) s.add(t.subject);
    });
    return ["ALL", ...Array.from(s)];
  }, [directory]);

  const filteredTeachers = useMemo(() => {
    if (selectedSubject === "ALL") return directory;
    return directory.filter((t) => t.subject === selectedSubject);
  }, [directory, selectedSubject]);

  const performLogin = async (payload) => {
    setError("");
    setLoading(true);
    try {
      const normalized = normalizedServer();
      const user = await apiFetch(normalized, "/api/auth/login", "", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      if (user?.token) {
        setAuthToken(user.token);
      }
      localStorage.setItem("kkomi_last_user", user.username || "");
      localStorage.setItem("kkomi_last_subject", selectedSubject);
      await connectDesktopBridge(normalized, user.id, user.token || "");
      onLoginSuccess(normalized, user);
    } catch (err) {
      setError(err.message || "서버 연결 또는 로그인에 실패했습니다.");
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = (e) => {
    e.preventDefault();
    if (loginMode === "select" && directory.length > 0) {
      const chosen = directory.find((t) => t.username === selectedUsername) || filteredTeachers[0];
      performLogin({
        username: chosen?.username || selectedUsername,
        subject: selectedSubject === "ALL" ? chosen?.subject || "" : selectedSubject,
        password,
      });
      return;
    }
    performLogin({
      username: manualName.trim(),
      name: manualName.trim(),
      subject: manualSubject.trim(),
      password,
    });
  };

  return (
    <div style={{ minHeight: "100vh", display: "grid", placeItems: "center", background: "linear-gradient(135deg, #0f172a 0%, #1e3a8a 55%, #3b5bdb 100%)", padding: 20 }}>
      <div className="card" style={{ width: "min(560px, 95vw)", padding: 28, boxShadow: "0 24px 60px rgba(0,0,0,0.35)" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 14, marginBottom: 16 }}>
          <div className="brand-logo" style={{ width: 48, height: 48, fontSize: 22 }}>🐾</div>
          <div>
            <h1 style={{ margin: 0, fontSize: 22, fontWeight: 800 }}>꼬미시간 (KkomiTimetable)</h1>
            <div style={{ fontSize: 12.5, color: "#64748b", marginTop: 2 }}>
              중·고등학교 학사 시간표 · 내/외부 시험감독 · 행사 통합 솔루션
            </div>
          </div>
        </div>

        <div style={{ display: "flex", gap: 6, marginBottom: 14 }}>
          <button type="button" className={`btn btn-sm ${loginMode === "select" ? "btn-primary" : ""}`} onClick={() => setLoginMode("select")}>
            <UserCheck size={14} /> 간편 선택 로그인 (동명이인 자동 구분)
          </button>
          <button type="button" className={`btn btn-sm ${loginMode === "manual" ? "btn-primary" : ""}`} onClick={() => setLoginMode("manual")}>
            직접 입력 로그인 (과목 · 이름/ID · 비번)
          </button>
        </div>

        <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <div>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 4 }}>
              <label style={{ fontSize: 12, fontWeight: 700 }}>
                <Server size={12} style={{ verticalAlign: "middle" }} /> 교무실 메인 서버 주소 (업무망 LAN)
              </label>
              <span className="badge badge-blue">{dirStatus || "확인 중"}</span>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <input
                className="input"
                value={serverUrl}
                onChange={(e) => setServerUrl(e.target.value)}
                onBlur={() => loadDirectory(serverUrl)}
                placeholder="예: http://192.168.0.10:18090"
              />
              <button type="button" className="btn btn-sm" onClick={() => loadDirectory(serverUrl)} title="교사 목록 새로고침">
                <RefreshCw size={14} />
              </button>
            </div>
          </div>

          {loginMode === "select" && directory.length > 0 ? (
            <div className="grid-2">
              <div>
                <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>1. 담당과목 선택</label>
                <select
                  className="select"
                  value={selectedSubject}
                  onChange={(e) => {
                    const subj = e.target.value;
                    setSelectedSubject(subj);
                    const nextList = subj === "ALL" ? directory : directory.filter((t) => t.subject === subj);
                    if (nextList.length > 0) setSelectedUsername(nextList[0].username);
                  }}
                >
                  {subjectOptions.map((s) => (
                    <option key={s} value={s}>{s === "ALL" ? "전체 교과 보기" : s}</option>
                  ))}
                </select>
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>2. 교사 성명 선택</label>
                <select className="select" value={selectedUsername} onChange={(e) => setSelectedUsername(e.target.value)}>
                  {filteredTeachers.map((t) => (
                    <option key={t.id} value={t.username}>
                      {t.name} ({t.subject}{t.homeroomClass ? ` · ${t.homeroomClass}` : ""} · {t.username})
                    </option>
                  ))}
                </select>
              </div>
            </div>
          ) : (
            <div className="grid-2">
              <div>
                <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>담당과목 (동명이인 구분)</label>
                <input className="input" value={manualSubject} onChange={(e) => setManualSubject(e.target.value)} placeholder="예: 국어 (선택 또는 필수)" />
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>성명 또는 아이디</label>
                <input className="input" value={manualName} onChange={(e) => setManualName(e.target.value)} placeholder="예: 강국어 또는 admin" />
              </div>
            </div>
          )}

          <div>
            <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>
              <KeyRound size={12} style={{ verticalAlign: "middle" }} /> 비밀번호
            </label>
            <input
              className="input"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="비밀번호 입력 (최초 초기 비밀번호: 1234)"
              required
            />
          </div>

          {error && <div className="badge badge-red" style={{ padding: "8px 12px", borderRadius: 8 }}>{error}</div>}

          <button type="submit" className="btn btn-primary" style={{ justifyContent: "center", padding: "12px 16px", fontSize: 14 }} disabled={loading}>
            <ShieldCheck size={17} /> {loading ? "연결 중..." : "업무망 서버 접속 및 로그인"}
          </button>
        </form>

        {isDemoMode && (
          <div style={{ marginTop: 18, borderTop: "1px solid #e2e8f0", paddingTop: 12 }}>
            <div style={{ fontSize: 12, fontWeight: 700, color: "#475569", marginBottom: 8 }}>
              체험용 빠른 계정 전환 (시연 모드 · 초기 비밀번호: 1234)
            </div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
              {QUICK_ACCOUNTS.map((acc) => (
                <button
                  key={acc.username}
                  type="button"
                  className="btn btn-sm"
                  onClick={() => performLogin({ username: acc.username, password: "1234" })}
                >
                  <span className="badge badge-blue">{acc.badge}</span> {acc.label}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
