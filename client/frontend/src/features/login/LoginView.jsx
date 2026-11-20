import React, { useEffect, useMemo, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { Info, KeyRound, RefreshCw, Server, ShieldCheck, UserCheck } from "lucide-react";
import { apiFetch, connectDesktopBridge, setAuthToken } from "../../events/eventStore";
import AppTooltip from "../../components/ui/AppTooltip";

// LoginView는 교무실 공용 서버 연결 및 [과목·성명 간편 선택 / 직접 입력(동명이인 구분)] 로그인을 제공합니다.
export default function LoginView({ initialServerUrl, onLoginSuccess }) {
  const [serverUrl, setServerUrl] = useState(initialServerUrl || "http://127.0.0.1:18090");
  const [loginMode, setLoginMode] = useState("select"); // "select" | "manual"
  const [directory, setDirectory] = useState([]);
  const [dirStatus, setDirStatus] = useState("");

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
      const list = await apiFetch(base, "/api/auth/directory", "");
      setDirectory(list || []);
      setDirStatus(`교사 ${list?.length || 0}명 연결됨`);
      if (list && list.length > 0 && !list.some((t) => t.username === selectedUsername)) {
        setSelectedUsername(list[0].username);
      }
    } catch {
      setDirectory([]);
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

  const isFreshSetup = directory.length === 1 && directory[0]?.username === "admin";

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

        <Tabs.Root value={loginMode} onValueChange={setLoginMode} style={{ marginBottom: 14 }}>
          <Tabs.List className="radix-tabs-list" aria-label="로그인 방식 선택" style={{ width: "100%" }}>
            <Tabs.Trigger value="select" className="radix-tabs-trigger" style={{ flex: 1, justifyContent: "center" }}>
              <UserCheck size={14} /> 간편 선택 로그인
            </Tabs.Trigger>
            <Tabs.Trigger value="manual" className="radix-tabs-trigger" style={{ flex: 1, justifyContent: "center" }}>
              직접 입력 로그인 (과목 · 성명/ID)
            </Tabs.Trigger>
          </Tabs.List>
        </Tabs.Root>

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
              <AppTooltip content="서버 연결 및 교사 목록 새로고침">
                <button type="button" className="btn btn-sm" onClick={() => loadDirectory(serverUrl)} aria-label="교사 목록 새로고침">
                  <RefreshCw size={14} />
                </button>
              </AppTooltip>
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
                <input className="input" value={manualSubject} onChange={(e) => setManualSubject(e.target.value)} placeholder="예: 국어 (선택 입력)" />
              </div>
              <div>
                <label style={{ fontSize: 12, fontWeight: 700, display: "block", marginBottom: 4 }}>성명 또는 아이디</label>
                <input className="input" value={manualName} onChange={(e) => setManualName(e.target.value)} placeholder="예: 홍길동 또는 admin" />
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
              placeholder="비밀번호 입력"
              required
            />
          </div>

          {error && <div className="badge badge-red" style={{ padding: "8px 12px", borderRadius: 8 }}>{error}</div>}

          <button type="submit" className="btn btn-primary" style={{ justifyContent: "center", padding: "12px 16px", fontSize: 14 }} disabled={loading}>
            <ShieldCheck size={17} /> {loading ? "연결 중..." : "업무망 서버 접속 및 로그인"}
          </button>
        </form>

        {isFreshSetup && (
          <div style={{ marginTop: 14, padding: "10px 12px", borderRadius: 10, background: "#f8fafc", border: "1px solid #cbd5e1", fontSize: 12, color: "#475569", display: "flex", gap: 8, alignItems: "flex-start" }}>
            <Info size={15} color="#3b5bdb" style={{ flexShrink: 0, marginTop: 1 }} />
            <span>최초 설치 상태입니다. 기본 최고관리자(<b>admin</b>) 계정으로 로그인한 뒤 비밀번호를 변경하고 교사 명렬표를 등록해 주세요.</span>
          </div>
        )}
      </div>
    </div>
  );
}
