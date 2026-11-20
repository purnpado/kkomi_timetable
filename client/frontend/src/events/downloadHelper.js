import { getAuthToken, triggerUnauthorized } from "./eventStore";

function parseFileNameFromDisposition(headerValue, fallbackName) {
  if (!headerValue) return fallbackName;
  const utf8Match = /filename\*=UTF-8''([^;]+)/i.exec(headerValue);
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1].trim());
    } catch {
      return utf8Match[1].trim();
    }
  }
  const plainMatch = /filename="?([^";]+)"?/i.exec(headerValue);
  if (plainMatch?.[1]) {
    return plainMatch[1].trim();
  }
  return fallbackName;
}

// downloadAuthenticatedFile은 Authorization Bearer 토큰을 포함해 서버에서 파일을 내려받아
// 브라우저와 Wails WebView 양쪽 모두에서 안전하게 저장하거나 인쇄 탭을 엽니다.
export async function downloadAuthenticatedFile(serverUrl, path, fallbackName, { openInTab = false } = {}) {
  const base = (serverUrl || "http://127.0.0.1:18090").replace(/\/+$/, "");
  const headers = {};
  const token = getAuthToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(`${base}${path}`, { method: "GET", headers });
  if (res.status === 401) {
    triggerUnauthorized();
  }
  if (!res.ok) {
    let msg = `다운로드 오류 (${res.status})`;
    try {
      const data = await res.json();
      if (data?.error) msg = data.error;
    } catch {
      // ignore non-json error body
    }
    throw new Error(msg);
  }

  const fileName = parseFileNameFromDisposition(res.headers.get("Content-Disposition"), fallbackName);
  const blob = await res.blob();
  const blobUrl = URL.createObjectURL(blob);

  if (openInTab) {
    const win = window.open(blobUrl, "_blank", "noopener,noreferrer");
    if (win) {
      setTimeout(() => URL.revokeObjectURL(blobUrl), 60000);
      return fileName;
    }
  }

  const a = document.createElement("a");
  a.href = blobUrl;
  a.download = fileName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(blobUrl), 5000);
  return fileName;
}
