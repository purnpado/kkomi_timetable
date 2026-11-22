import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  BarChart3, CalendarDays, ClipboardList, Download, Globe, KeyRound, LayoutDashboard,
  LogOut, ServerCog, ShieldCheck, TableProperties,
} from "lucide-react";
import {
  apiFetch, applySelfUpdateBridge, checkInClassNow, clearAuthToken, connectDesktopBridge,
  disconnectDesktopBridge, formatUserDisplayName, getAuthToken, hasRole, isFacultyTeacher, isWailsRuntime,
  resolveServerUrl, ROLE_LABELS, setAuthToken, setUnauthorizedHandler, subscribeDesktopEvents,
  toggleSideDockBridge,
} from "./events/eventStore";
import { AppTooltipProvider } from "./components/ui/AppTooltip";
import TopBar from "./components/ui/TopBar";
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
import ReportsSettlementView from "./features/reports/ReportsSettlementView";
import AdminRemoteView from "./features/admin-remote/AdminRemoteView";

const NAV_ITEMS = [
  { id: "dashboard", label: "오늘의 대시보드", icon: LayoutDashboard },
  { id: "timetable", label: "정규·일일 시간표", icon: TableProperties },
  { id: "internal-exam", label: "교내 시험감독", icon: ShieldCheck },
  { id: "external-exam", label: "외부 시험감독", icon: Globe },
  { id: "calendar", label: "주중·월중 행사", icon: CalendarDays },
  { id: "reports", label: "통계·정산·공문", icon: BarChart3 },
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
  const [wsStatus, setWsStatus] = useState("connected");
  const [updateBanner, setUpdateBanner] = useState(null);

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
  const inClassRef = useRef(inClassStatus);
  inClassRef.current = inClassStatus;
  const prevInClassRef = useRef(false);

  useEffect(() => {
    if (prevInClassRef.current && !inClassStatus.inClass && queuedAlerts.length > 0) {
      setToasts((prev) => [...prev.slice(-2), ...queuedAlerts]);
      setQueuedAlerts([]);
    }
    prevInClassRef.current = inClassStatus.inClass;
  }, [inClassStatus.inClass, queuedAlerts]);

  const pushToast = (title, message, urgent = false) => {
    const item = { id: `t-${Date.now()}-${Math.random()}`, title, message, urgent };
    if (!urgent && inClassRef.current.inClass) {
      setQueuedAlerts((prev) => [...prev, item]);
      return;
    }
    setToasts((prev) => [...prev.slice(-3), item]);
  };

  const resetClientSession = (noticeTitle, noticeMsg) => {
    clearAuthToken();
    disconnectDesktopBridge();
    setUser(null);
    setActiveTab("dashboard");
    setUpdateBanner(null);
    if (noticeTitle) {
      pushToast(noticeTitle, noticeMsg, true);
    }
  };

  useEffect(() => {
    setUnauthorizedHandler(() => {
      resetClientSession("로그인 세션 만료", "인증이 만료되어 로그인 화면으로 이동합니다.");
    });
    return () => setUnauthorizedHandler(null);
  }, []);

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

    const handleIncomingEvent = (evt) => {
      if (evt.type === "SystemReset" || evt.type === "SystemRestored") {
        resetClientSession(evt.title || "시스템 상태 변경", evt.message || "다시 로그인해 주세요.");
        return;
      }
      if (evt.type === "SystemUpdateAvailable") {
        setUpdateBanner(evt.payload || { version: "최신", message: evt.message });
      }
      pushToast(evt.title, evt.message, evt.urgent);
      refreshAll(serverUrl, user);
    };

    const unsubDesktop = subscribeDesktopEvents(handleIncomingEvent, (dock) => setDockMode(dock));

    // Wails 데스크톱 앱에서는 Go netclient가 단일 WebSocket을 관리하므로 브라우저 WS 중복 연결 금지
    if (isWailsRuntime()) {
      setWsStatus("connected");
      return () => unsubDesktop();
    }

    let alive = true;
    let socket = null;
    let retryTimer = null;
    let backoffMs = 2000;

    const connectBrowserWS = () => {
      if (!alive) return;
      const token = getAuthToken();
      const wsBase = serverUrl.replace(/^http/, "ws");
      const wsUrl = token
        ? `${wsBase}/ws?token=${encodeURIComponent(token)}`
        : `${wsBase}/ws?teacherId=${encodeURIComponent(user.id)}`;
      try {
        socket = new WebSocket(wsUrl);
        socket.onopen = () => {
          backoffMs = 2000;
          if (alive) setWsStatus("connected");
        };
        socket.onmessage = (msg) => {
          try {
            handleIncomingEvent(JSON.parse(msg.data));
          } catch { /* ignore */ }
        };
        socket.onclose = () => {
          if (!alive) return;
          setWsStatus("reconnecting");
          retryTimer = setTimeout(connectBrowserWS, backoffMs);
          backoffMs = Math.min(20000, backoffMs * 2);
        };
        socket.onerror = () => {
          if (socket) socket.close();
        };
      } catch {
        if (alive) {
          setWsStatus("reconnecting");
          retryTimer = setTimeout(connectBrowserWS, backoffMs);
          backoffMs = Math.min(20000, backoffMs * 2);
        }
      }
    };

    connectBrowserWS();
    return () => {
      alive = false;
      unsubDesktop();
      if (retryTimer) clearTimeout(retryTimer);
      if (socket) socket.close();
    };
  }, [user?.id, serverUrl]);

  const handleLogout = async () => {
    try {
      await apiFetch(serverUrl, "/api/auth/logout", user?.id || "", { method: "POST" });
    } catch {
      // ignore logout error
    }
    resetClientSession();
  };

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

  const renderMainContent = () => {
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
            <div className="brand-logo"><CalendarDays size={20} color="#ffffff" /></div>
            <div>
              <div style={{ fontWeight: 800, fontSize: 16 }}>꼬미시간</div>
              <div style={{ fontSize: 11, color: "#93c5fd" }}>v{summary?.serverVersion || "0.1.01"} · 교무 통합</div>
            </div>
          </div>

          {NAV_ITEMS.filter((item) => !item.adminOnly || hasRole(user, "MASTER")).map((item) => {
            const Icon = item.icon;
            return (
              <button
                key={item.id}
                type="button"
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
              <div style={{ fontWeight: 700, fontSize: 13 }}>{formatUserDisplayName(user)}</div>
              <div style={{ color: "#94a3b8", fontSize: 11, marginTop: 2 }}>
                {(user.roles || []).map((r) => ROLE_LABELS[r] || r).join(" · ")}
              </div>
            </div>
            <button type="button" className="btn btn-sm" style={{ width: "100%", justifyContent: "center" }} onClick={() => setPwModalOpen(true)}>
              <KeyRound size={13} /> 비밀번호 변경
            </button>
            <button type="button" className="btn btn-sm" style={{ width: "100%", justifyContent: "center" }} onClick={handleLogout}>
              <LogOut size={13} /> 로그아웃 / 계정 전환
            </button>
          </div>
        </aside>

        <div className="main-area">
          <TopBar
            inClassStatus={inClassStatus}
            wsStatus={wsStatus}
            clockText={clockText}
            onOpenPalette={() => setPaletteOpen(true)}
            onEnableDock={() => {
              setDockMode(true);
              toggleSideDockBridge(true);
            }}
          />

          {updateBanner && (
            <div className="admin-summary-bar no-print" style={{ background: "#e6fcf5", borderBottomColor: "#63e6be" }}>
              <div>
                <Download size={15} style={{ verticalAlign: "middle", marginRight: 6 }} />
                <b>새 버전({updateBanner.version || "최신"}) 업데이트가 준비되었습니다.</b>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                {isWailsRuntime() && (
                  <button
                    type="button"
                    className="btn btn-sm btn-green"
                    onClick={async () => {
                      try {
                        await applySelfUpdateBridge();
                      } catch (err) {
                        pushToast("업데이트 적용 안내", err?.message || "업데이트 확인 중 오류", true);
                      }
                    }}
                  >
                    지금 원클릭 패치 적용
                  </button>
                )}
                <button type="button" className="btn btn-sm" onClick={() => setUpdateBanner(null)}>닫기</button>
              </div>
            </div>
          )}

          {isAnyAdmin && (pendingQueue.length > 0 || pendingEventCount > 0) && (
            <div className="admin-summary-bar no-print">
              <div>
                <ClipboardList size={15} style={{ verticalAlign: "middle", marginRight: 6 }} />
                <b>담당자 미처리 승인 대기:</b> 수업교환/결보강 <b>{pendingQueue.length}건</b> · 행사 취합 <b>{pendingEventCount}건</b>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                {pendingQueue.length > 0 && hasRole(user, "TIMETABLE_ADMIN") && (
                  <button type="button" className="btn btn-sm btn-primary" onClick={() => { setApprovalIndex(0); setApprovalOpen(true); }}>
                    결보강 비교 승인 열기
                  </button>
                )}
                {pendingEventCount > 0 && hasRole(user, "EVENT_ADMIN") && (
                  <button type="button" className="btn btn-sm" onClick={() => setActiveTab("calendar")}>행사 취합 확인</button>
                )}
              </div>
            </div>
          )}

          <main className="content-scroll">
            {activeTab === "dashboard" && (
              <DashboardView
                user={user}
                summary={summary}
                teachersCount={teachers.filter(isFacultyTeacher).length}
                cellsCount={cells.length}
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
            {activeTab === "reports" && (
              <ReportsSettlementView serverUrl={serverUrl} user={user} teachers={teachers} onNotify={pushToast} />
            )}
            {activeTab === "admin-remote" && (
              <AdminRemoteView
                serverUrl={serverUrl}
                serverVersion={summary?.serverVersion || ""}
                user={user}
                teachers={teachers}
                onRefresh={() => refreshAll()}
                onResetComplete={() => resetClientSession()}
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
            if (updated?.token) {
              setAuthToken(updated.token);
              connectDesktopBridge(serverUrl, updated.id, updated.token);
            }
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
  };

  return <AppTooltipProvider>{renderMainContent()}</AppTooltipProvider>;
}
