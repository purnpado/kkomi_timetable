import { Call, Events } from "@wailsio/runtime";

const BRIDGE_SERVICE = "kkomi-timetable/client/internal/bridge.AppBridge";

let authToken = sessionStorage.getItem("kkomi_auth_token") || "";
let desktopBridgeDetected = typeof window !== "undefined" && Boolean(window._wails);
let unauthorizedHandler = null;

export function getAuthToken() {
  return authToken;
}

export function setAuthToken(token) {
  authToken = token || "";
  if (authToken) {
    sessionStorage.setItem("kkomi_auth_token", authToken);
  } else {
    sessionStorage.removeItem("kkomi_auth_token");
  }
}

export function clearAuthToken() {
  authToken = "";
  sessionStorage.removeItem("kkomi_auth_token");
}

export function setUnauthorizedHandler(fn) {
  unauthorizedHandler = typeof fn === "function" ? fn : null;
}

export function triggerUnauthorized() {
  if (unauthorizedHandler) unauthorizedHandler();
}

export function isWailsRuntime() {
  return Boolean(desktopBridgeDetected || (typeof window !== "undefined" && window._wails));
}

// Wails 데스크톱 앱 또는 웹 브라우저 폴백 환경에 맞게 서버 기본 주소를 결정합니다.
export async function resolveServerUrl() {
  const saved = localStorage.getItem("kkomi_server_url");
  try {
    const url = await Call.ByName(`${BRIDGE_SERVICE}.GetServerURL`);
    desktopBridgeDetected = true;
    if (saved) return saved;
    if (url) return url;
  } catch {
    // 브라우저로 서버에 직접 접속한 경우 동일 Origin 사용
  }
  if (saved) return saved;
  if (window.location.port === "5173") return "http://127.0.0.1:18090";
  if (window.location.protocol.startsWith("http")) return window.location.origin;
  return "http://127.0.0.1:18090";
}

export async function connectDesktopBridge(serverUrl, teacherId, token = authToken) {
  localStorage.setItem("kkomi_server_url", serverUrl);
  try {
    await Call.ByName(`${BRIDGE_SERVICE}.ConnectServer`, serverUrl, teacherId, token || "");
    desktopBridgeDetected = true;
  } catch {
    // 브라우저 환경에서는 직접 WebSocket 연결
  }
}

export async function disconnectDesktopBridge() {
  try {
    await Call.ByName(`${BRIDGE_SERVICE}.Disconnect`);
  } catch {
    // ignore in web browser mode
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
    // 웹 브라우저 모드에서는 직접 WebSocket 구독 사용
  }
  return () => cleanups.forEach((fn) => typeof fn === "function" && fn());
}

// apiFetch는 Authorization Bearer 세션 토큰을 자동 포함해 서버 API를 호출합니다.
export async function apiFetch(serverUrl, path, teacherId, options = {}) {
  const base = (serverUrl || "http://127.0.0.1:18090").replace(/\/+$/, "");
  const headers = {
    ...(options.headers || {}),
  };
  if (!(options.body instanceof FormData) && !headers["Content-Type"]) {
    headers["Content-Type"] = "application/json";
  }
  if (authToken) {
    headers.Authorization = `Bearer ${authToken}`;
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
  if (res.status === 401 && !path.startsWith("/api/auth/login") && unauthorizedHandler) {
    unauthorizedHandler();
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

export const TEACHER_TYPES = [
  { value: "SUBJECT", label: "교과 교사", shortLabel: "교과", defaultHasClasses: true, defaultSubstituteExempt: false, defaultPosition: "평교사", defaultSubject: "국어" },
  { value: "SPECIAL", label: "특수 교사", shortLabel: "특수", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "특수교사", defaultSubject: "특수학급" },
  { value: "HEALTH", label: "보건 교사", shortLabel: "보건", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "보건교사", defaultSubject: "보건" },
  { value: "COUNSELOR", label: "전문상담 교사", shortLabel: "상담", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "전문상담교사", defaultSubject: "전문상담" },
  { value: "LIBRARIAN", label: "사서 교사", shortLabel: "사서", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "사서교사", defaultSubject: "사서" },
  { value: "NUTRITION", label: "영양 교사", shortLabel: "영양", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "영양교사", defaultSubject: "영양" },
  { value: "OTHER", label: "기타 비교과", shortLabel: "기타", defaultHasClasses: false, defaultSubstituteExempt: true, defaultPosition: "비교과 교사", defaultSubject: "비교과" },
];

export function getTeacherTypeMeta(type) {
  return TEACHER_TYPES.find((item) => item.value === type) || TEACHER_TYPES[0];
}

export function isSystemAdminAccount(t) {
  if (!t) return false;
  const uname = String(t.username || "").trim().toLowerCase();
  return uname === "admin" && (t.name === "최고관리자" || t.name === "시스템 관리자" || t.subject === "시스템" || t.subject === "행정/총괄");
}

export function isFacultyTeacher(t) {
  return Boolean(t) && !t.isParentHonorary && !isSystemAdminAccount(t);
}

export function effectiveTeacherType(t) {
  if (!t) return "SUBJECT";
  const explicit = String(t.teacherType || "").trim().toUpperCase();
  if (TEACHER_TYPES.some((item) => item.value === explicit)) return explicit;
  if (isSystemAdminAccount(t) || t.isParentHonorary) return "OTHER";
  const joined = `${t.position || ""} ${t.subject || ""}`.toLowerCase();
  if (joined.includes("특수")) return "SPECIAL";
  if (joined.includes("보건")) return "HEALTH";
  if (joined.includes("상담") || joined.includes("위클래스") || joined.includes("wee")) return "COUNSELOR";
  if (joined.includes("사서") || joined.includes("도서관")) return "LIBRARIAN";
  if (joined.includes("영양")) return "NUTRITION";
  return "SUBJECT";
}

export function hasRegularClasses(t) {
  if (!isFacultyTeacher(t)) return false;
  if (typeof t.hasClasses === "boolean") return t.hasClasses;
  return effectiveTeacherType(t) === "SUBJECT";
}

export function isSubstituteExcluded(t) {
  if (!isFacultyTeacher(t)) return true;
  if (typeof t.substituteExempt === "boolean") return t.substituteExempt;
  return effectiveTeacherType(t) !== "SUBJECT";
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
