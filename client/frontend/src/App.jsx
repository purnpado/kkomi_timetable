import React, { useEffect, useMemo, useState } from "react";
import {
  CalendarDays, ClipboardList, Columns, Globe, KeyRound, LayoutDashboard,
  LogOut, Search, ServerCog, ShieldCheck, TableProperties,
} from "lucide-react";
import {
  apiFetch, checkInClassNow, hasRole, resolveServerUrl,
  ROLE_LABELS, subscribeDesktopEvents, toggleSideDockBridge,
} from "./events/eventStore";
import ToastHub from "./components/ui/ToastHub";
import ApprovalModal from "./components/ui/ApprovalModal";
import CommandPalette from "./components/ui/CommandPalette";
import SideDockWidget from "./components/ui/SideDockWidget";
import PasswordChangeModal from "./components/ui/PasswordChangeModal";
import LoginView from "./features/login/LoginView";
import DashboardView from "./features/dashboard/DashboardView";
import TimetableView from "./features/timetable/TimetableView";
import InternalExamView from "./features/internal-exam/InternalExamView";
import ExternalExamView from "./features/external-exam/ExternalExamView";
import CalendarView from "./features/calendar/CalendarView";
import AdminRemoteView from "./features/admin-remote/AdminRemoteView";

const NAV_ITEMS = [
  { id: "dashboard", label: "오늘의 대시보드", icon: LayoutDashboard },
  { id: "timetable", label: "정규·일일 시간표", icon: TableProperties },
  { id: "internal-exam", label: "교내 시험감독", icon: ShieldCheck },
  { id: "external-exam", label: "외부 시험감독", icon: Globe },
  { id: "calendar", label: "주중·월중 행사", icon: CalendarDays },
  { id: "admin-remote", label: "시스템·초기화·권한", icon: ServerCog, adminOnly: true },
];

export default function App() {
  const [serverUrl, setServerUrl] = useState("http://127.0.0.1:18090");
  const [user, setUser] = useState(null);
  const [activeTab, setActiveTab] = useState("dashboard");
  const [dockMode, setDockMode] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [approvalOpen, setApprovalOpen] = useState(false);
  const [approvalIndex, setApprovalIndex] = useState(0);
  const [pwModalOpen, setPwModalOpen] = useState(false);
  const [clockText, setClockText] = useState(() => new Date().toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" }));

  const [summary, setSummary] = useState(null);
  const [teachers, setTeachers] = useState([]);
  const [cells, setCells] = useState([]);
  const [requests, setRequests] = useState([]);
  const [events, setEvents] = useState([]);

  const [toasts, setToasts] = useState([]);
  const [queuedAlerts, setQueuedAlerts] = useState([]);

  useEffect(() => {
    resolveServerUrl().then(setServerUrl);
    const timer = setInterval(() => {
      setClockText(new Date().toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" }));
    }, 15000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    const onKey = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const inClassStatus = useMemo(() => checkInClassNow(summary?.bellSchedule || []), [summary?.bellSchedule, clockText]);

  const pushToast = (title, message, urgent = false) => {
    const item = { id: `t-${Date.now()}-${Math.random()}`, title, message, urgent };
    if (!urgent && inClassStatus.inClass) {
      setQueuedAlerts((prev) => [...prev, item]);
      return;
    }
    setToasts((prev) => [...prev.slice(-3), item]);
  };

  const refreshAll = async (srv = serverUrl, usr = user) => {
    if (!usr) return;
    try {
      const [sum, tList, cList, rList, evList] = await Promise.all([
        apiFetch(srv, "/api/dashboard", usr.id),
        apiFetch(srv, "/api/teachers", usr.id),
        apiFetch(srv, "/api/timetable/cells", usr.id),
        apiFetch(srv, "/api/timetable/requests", usr.id),
        apiFetch(srv, "/api/calendar/events", usr.id),
      ]);
      setSummary(sum);
      setTeachers(tList || []);
      setCells(cList || []);
      setRequests(rList || []);
      setEvents(evList || []);
    } catch (err) {
      pushToast("데이터 동기화 오류", err.message, true);
    }
  };

  useEffect(() => {
    if (!user) return;
    refreshAll(serverUrl, user);

    const unsubDesktop = subscribeDesktopEvents(
      (evt) => {
        pushToast(evt.title, evt.message, evt.urgent);
        refreshAll(serverUrl, user);
      },
      (dock) => setDockMode(dock)
    );

    const wsUrl = serverUrl.replace(/^http/, "ws") + `/ws?teacherId=${user.id}`;
    let socket;
    try {
      socket = new WebSocket(wsUrl);
      socket.onmessage = (msg) => {
        try {
          const evt = JSON.parse(msg.data);
          pushToast(evt.title, evt.message, evt.urgent);
          refreshAll(serverUrl, user);
        } catch { /* ignore */ }
      };
    } catch { /* ignore */ }

    return () => {
      unsubDesktop();
      if (socket) socket.close();
    };
  }, [user?.id, serverUrl]);

  const handleCommitApproval = async (requestId, approve, rejectReason, openNext) => {
    try {
      await apiFetch(serverUrl, "/api/timetable/commit", user.id, {
        method: "POST",
        body: JSON.stringify({ requestId, approve, rejectReason }),
      });
      pushToast(approve ? "서버 최종 반영 완료" : "요청 반려", "변경 내역이 마스터 DB에 처리되었습니다.");
      await refreshAll();
      if (!openNext) setApprovalOpen(false);
    } catch (err) {
      pushToast("승인 처리 실패", err.message, true);
    }
  };

  if (!user) {
    return (
      <LoginView
        initialServerUrl={serverUrl}
        onLoginSuccess={(srv, usr) => {
          setServerUrl(srv);
          setUser(usr);
          if (usr.mustChangePassword) setPwModalOpen(true);
        }}
      />
    );
  }

  if (dockMode) {
    return (
      <SideDockWidget
        user={user}
        summary={summary}
        onRestoreFull={() => {
          setDockMode(false);
          toggleSideDockBridge(false);
        }}
      />
    );
  }

  const pendingQueue = requests.filter((r) => r.status === "STAGING");
  const pendingEventCount = events.filter((e) => e.status === "STAGING").length;
  const isAnyAdmin = hasRole(user, "TIMETABLE_ADMIN", "INTERNAL_EXAM_ADMIN", "EXTERNAL_EXAM_ADMIN", "EVENT_ADMIN");

  return (
    <div className="app-shell">
      <aside className="sidebar no-print">
        <div className="brand-box">
          <div className="brand-logo">🐾</div>
          <div>
            <div style={{ fontWeight: 800, fontSize: 16 }}>꼬미시간</div>
            <div style={{ fontSize: 11, color: "#93c5fd" }}>v{summary?.serverVersion || "0.1.0"} · 교무 통합</div>
          </div>
        </div>

        {NAV_ITEMS.filter((item) => !item.adminOnly || hasRole(user, "MASTER")).map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.id}
              className={`nav-btn ${activeTab === item.id ? "active" : ""}`}
              onClick={() => setActiveTab(item.id)}
            >
              <span style={{ display: "flex", alignItems: "center", gap: 9 }}>
                <Icon size={16} /> {item.label}
              </span>
              {item.id === "timetable" && pendingQueue.length > 0 && <span className="badge badge-amber">{pendingQueue.length}</span>}
              {item.id === "calendar" && pendingEventCount > 0 && <span className="badge badge-amber">{pendingEventCount}</span>}
            </button>
          );
        })}

        <div style={{ marginTop: "auto", padding: "12px 6px 4px", borderTop: "1px solid rgba(255,255,255,0.12)", fontSize: 12, display: "flex", flexDirection: "column", gap: 6 }}>
          <div>
            <div style={{ fontWeight: 700, fontSize: 13 }}>{user.name} 선생님</div>
            <div style={{ color: "#94a3b8", fontSize: 11, marginTop: 2 }}>
              {(user.roles || []).map((r) => ROLE_LABELS[r] || r).join(" · ")}
            </div>
          </div>
          <button className="btn btn-sm" style={{ width: "100%", justifyContent: "center" }} onClick={() => setPwModalOpen(true)}>
            <KeyRound size={13} /> 비밀번호 변경
          </button>
          <button className="btn btn-sm" style={{ width: "100%", justifyContent: "center" }} onClick={() => setUser(null)}>
            <LogOut size={13} /> 로그아웃 / 계정 전환
          </button>
        </div>
      </aside>

      <div className="main-area">
        <header className="topbar no-print">
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <button className="btn btn-sm" onClick={() => setPaletteOpen(true)}>
              <Search size={14} /> 교사 위치·학급·행사 검색 <span className="badge badge-gray">Ctrl+K</span>
            </button>
            <span className={`badge ${inClassStatus.inClass ? "badge-amber" : "badge-green"}`}>
              {inClassStatus.inClass ? `🔕 수업 중 방해금지 (${inClassStatus.periodLabel})` : "🔔 쉬는시간 실시간 알림"}
            </span>
          </div>
          <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
            <span className="badge badge-blue">🕒 {clockText}</span>
            <button
              className="btn btn-sm"
              onClick={() => {
                setDockMode(true);
                toggleSideDockBridge(true);
              }}
            >
              <Columns size={14} /> 사이드 위젯 (260px)
            </button>
          </div>
        </header>

        {isAnyAdmin && (pendingQueue.length > 0 || pendingEventCount > 0) && (
          <div className="admin-summary-bar no-print">
            <div>
              <ClipboardList size={15} style={{ verticalAlign: "middle", marginRight: 6 }} />
              <b>담당자 미처리 승인 대기:</b> 수업교환/결보강 <b>{pendingQueue.length}건</b> · 행사 취합 <b>{pendingEventCount}건</b>
            </div>
            <div style={{ display: "flex", gap: 8 }}>
              {pendingQueue.length > 0 && hasRole(user, "TIMETABLE_ADMIN") && (
                <button className="btn btn-sm btn-primary" onClick={() => { setApprovalIndex(0); setApprovalOpen(true); }}>
                  결보강 비교 승인 열기
                </button>
              )}
              {pendingEventCount > 0 && hasRole(user, "EVENT_ADMIN") && (
                <button className="btn btn-sm" onClick={() => setActiveTab("calendar")}>행사 취합 확인</button>
              )}
            </div>
          </div>
        )}

        <main className="content-scroll">
          {activeTab === "dashboard" && (
            <DashboardView
              user={user}
              summary={summary}
              onNavigate={setActiveTab}
              onOpenApproval={() => { setApprovalIndex(0); setApprovalOpen(true); }}
            />
          )}
          {activeTab === "timetable" && (
            <TimetableView
              serverUrl={serverUrl}
              user={user}
              cells={cells}
              teachers={teachers}
              requests={requests}
              bellSchedule={summary?.bellSchedule || []}
              onRefresh={() => refreshAll()}
              onOpenApproval={() => { setApprovalIndex(0); setApprovalOpen(true); }}
              onNotify={pushToast}
            />
          )}
          {activeTab === "internal-exam" && (
            <InternalExamView serverUrl={serverUrl} user={user} onNotify={pushToast} />
          )}
          {activeTab === "external-exam" && (
            <ExternalExamView serverUrl={serverUrl} user={user} onNotify={pushToast} onRefresh={() => refreshAll()} />
          )}
          {activeTab === "calendar" && (
            <CalendarView serverUrl={serverUrl} user={user} events={events} teachers={teachers} onRefresh={() => refreshAll()} onNotify={pushToast} />
          )}
          {activeTab === "admin-remote" && (
            <AdminRemoteView
              serverUrl={serverUrl}
              user={user}
              teachers={teachers}
              onRefresh={() => refreshAll()}
              onResetComplete={() => {
                setUser(null);
                setActiveTab("dashboard");
              }}
              onNotify={pushToast}
            />
          )}
        </main>
      </div>

      <PasswordChangeModal
        open={pwModalOpen}
        onOpenChange={setPwModalOpen}
        serverUrl={serverUrl}
        user={user}
        forced={Boolean(user?.mustChangePassword)}
        onChanged={(updated) => {
          setUser(updated);
          pushToast("비밀번호 변경 완료", "새 비밀번호가 안전하게 저장되었습니다.");
        }}
      />
      <ApprovalModal
        open={approvalOpen}
        onOpenChange={setApprovalOpen}
        queue={pendingQueue}
        currentIndex={approvalIndex}
        onCommit={handleCommitApproval}
      />
      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        teachers={teachers}
        cells={cells}
        events={events}
        onNavigate={setActiveTab}
      />
      <ToastHub
        toasts={toasts}
        queuedAlerts={queuedAlerts}
        inClassStatus={inClassStatus}
        onDismiss={(id) => setToasts((prev) => prev.filter((t) => t.id !== id))}
        onFlushQueue={() => {
          setToasts((prev) => [...prev, ...queuedAlerts]);
          setQueuedAlerts([]);
        }}
      />
    </div>
  );
}
