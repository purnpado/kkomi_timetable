import { Call, Events } from "@wailsio/runtime";

const BRIDGE_SERVICE = "kkomi-timetable/client/internal/bridge.AppBridge";

// Wails 데스크톱 앱 또는 웹 브라우저 폴백 환경에 맞게 서버 기본 주소를 결정합니다.
export async function resolveServerUrl() {
  const saved = localStorage.getItem("kkomi_server_url");
  if (saved) return saved;
  try {
    const url = await Call.ByName(`${BRIDGE_SERVICE}.GetServerURL`);
    if (url) return url;
  } catch {
    // 브라우저로 서버에 직접 접속한 경우 동일 Origin 사용
  }
  if (window.location.port === "5173") return "http://127.0.0.1:18090";
  if (window.location.protocol.startsWith("http")) return window.location.origin;
  return "http://127.0.0.1:18090";
}

export async function connectDesktopBridge(serverUrl, teacherId) {
  localStorage.setItem("kkomi_server_url", serverUrl);
  try {
    await Call.ByName(`${BRIDGE_SERVICE}.ConnectServer`, serverUrl, teacherId);
  } catch {
    // 브라우저 환경에서는 직접 WebSocket 연결
  }
}

export async function toggleSideDockBridge(enable) {
  try {
    await Call.ByName(`${BRIDGE_SERVICE}.ToggleSideDockMode`, enable);
  } catch {
    // 웹 미리보기에서도 상태 전환 허용
  }
}

export async function applySelfUpdateBridge() {
  return Call.ByName(`${BRIDGE_SERVICE}.ApplySelfUpdate`);
}

export async function printDesktopWindow() {
  try {
    await Call.ByName(`${BRIDGE_SERVICE}.PrintWindow`);
  } catch {
    window.print();
  }
}

export function subscribeDesktopEvents(onDomainEvent, onDockChange) {
  const cleanups = [];
  try {
    cleanups.push(
      Events.On("kkomi:domain-event", (ev) => {
        if (ev?.data) onDomainEvent(ev.data);
      })
    );
    cleanups.push(
      Events.On("kkomi:dock-mode", (ev) => {
        if (typeof ev?.data === "boolean") onDockChange(ev.data);
      })
    );
  } catch {
    // 웹 브라우저 모드에서는 아래 WebSocket 구독 사용
  }
  return () => cleanups.forEach((fn) => typeof fn === "function" && fn());
}

// apiFetch는 X-Teacher-ID 인증 헤더를 자동 포함해 서버 API를 호출합니다.
export async function apiFetch(serverUrl, path, teacherId, options = {}) {
  const base = (serverUrl || "http://127.0.0.1:18090").replace(/\/+$/, "");
  const headers = {
    ...(options.headers || {}),
  };
  if (!(options.body instanceof FormData) && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  if (teacherId) headers["X-Teacher-ID"] = teacherId;

  const res = await fetch(`${base}${path}`, { ...options, headers });
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!res.ok) {
    throw new Error(data?.error || `요청 오류 (${res.status})`);
  }
  return data;
}

// 현재 시각이 시종시간표 기준 수업 중인지 판별합니다. (수업 중 방해 금지 모드)
export function checkInClassNow(bellSchedule = []) {
  const now = new Date();
  const hhmm = `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
  for (const b of bellSchedule) {
    if (hhmm >= b.startTime && hhmm <= b.endTime) {
      return { inClass: true, periodLabel: b.label };
    }
  }
  return { inClass: false, periodLabel: "쉬는 시간 / 일과 전후" };
}

export const ROLE_LABELS = {
  MASTER: "최고관리자",
  TIMETABLE_ADMIN: "시간표 담당",
  INTERNAL_EXAM_ADMIN: "교내감독 담당",
  EXTERNAL_EXAM_ADMIN: "외부감독 담당",
  EVENT_ADMIN: "행사 담당",
  TEACHER: "일반 교사",
};

export function formatUserDisplayName(user) {
  if (!user) return "";
  if (user.username === "admin" || user.name === "최고관리자") return "최고관리자";
  return `${user.name} 선생님`;
}

export function hasRole(user, ...roles) {
  if (!user || !Array.isArray(user.roles)) return false;
  if (user.roles.includes("MASTER")) return true;
  return roles.some((r) => user.roles.includes(r));
}
