const els = {
  versionBadge: document.getElementById("versionBadge"),
  statusBadge: document.getElementById("statusBadge"),
  statusDot: document.getElementById("statusDot"),
  statusText: document.getElementById("statusText"),
  initCard: document.getElementById("initCard"),
  elapsedBadge: document.getElementById("elapsedBadge"),
  phaseLabel: document.getElementById("phaseLabel"),
  progressFill: document.getElementById("progressFill"),
  progressPct: document.getElementById("progressPct"),
  errorCard: document.getElementById("errorCard"),
  errorTitle: document.getElementById("errorTitle"),
  errorDetail: document.getElementById("errorDetail"),
  errorHints: document.getElementById("errorHints"),
  dbModeBadge: document.getElementById("dbModeBadge"),
  lanUrlText: document.getElementById("lanUrlText"),
  baseDirText: document.getElementById("baseDirText"),
  connectedCount: document.getElementById("connectedCount"),
  backupCount: document.getElementById("backupCount"),
  logCountBadge: document.getElementById("logCountBadge"),
  logOutput: document.getElementById("logOutput"),
  feedbackBanner: document.getElementById("feedbackBanner"),
  copyLanBtn: document.getElementById("copyLanBtn"),
  openWebBtn: document.getElementById("openWebBtn"),
  backupBtn: document.getElementById("backupBtn"),
  openFolderBtn: document.getElementById("openFolderBtn"),
  errorOpenFolderBtn: document.getElementById("errorOpenFolderBtn"),
  quitBtn: document.getElementById("quitBtn"),
};

let latestLanUrl = "http://127.0.0.1:18090";
let quitArmTimer = null;
let quitArmed = false;

function showFeedback(text) {
  els.feedbackBanner.textContent = text;
  els.feedbackBanner.classList.remove("hidden");
  setTimeout(() => {
    if (els.feedbackBanner.textContent === text) {
      els.feedbackBanner.classList.add("hidden");
    }
  }, 4000);
}

async function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {}
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {}
  document.body.removeChild(ta);
  return ok;
}

function renderStatus(snap) {
  latestLanUrl = snap.lanUrl || "http://127.0.0.1:18090";
  els.versionBadge.textContent = `v${snap.version || "0.2.0"}`;
  els.lanUrlText.textContent = latestLanUrl;
  els.baseDirText.textContent = snap.baseDir ? `기준 폴더: ${snap.baseDir}` : "";
  els.dbModeBadge.textContent = snap.dbMode || "MariaDB 준비 중";
  els.connectedCount.textContent = `${snap.connectedCount || 0}명`;
  els.backupCount.textContent = `${snap.backupCount || 0}개`;

  const isRun = snap.state === "RUNNING";
  const isErr = snap.state === "ERROR";
  els.statusBadge.className = `badge ${isRun ? "badge-green" : isErr ? "badge-red" : "badge-amber"}`;
  els.statusDot.className = `dot ${isRun ? "dot-green" : isErr ? "dot-red" : "dot-amber"}`;
  els.statusText.textContent = isRun ? "서버 켜짐" : isErr ? "실행 오류" : "초기화 중";

  els.initCard.classList.toggle("hidden", isRun || isErr);
  els.elapsedBadge.textContent = `${snap.elapsedSec || 0}초`;
  els.phaseLabel.textContent = snap.stateLabel || "준비 중";
  els.progressFill.style.width = `${snap.progressPct || 15}%`;
  els.progressPct.textContent = `${snap.progressPct || 15}%`;

  els.errorCard.classList.toggle("hidden", !isErr);
  if (isErr) {
    els.errorTitle.textContent = snap.stateLabel || "서버 실행 오류";
    els.errorDetail.textContent = snap.lastError || "";
    els.errorHints.innerHTML = "";
    (snap.errorHints || []).forEach((h) => {
      const li = document.createElement("li");
      li.textContent = h;
      els.errorHints.appendChild(li);
    });
  }

  const logs = Array.isArray(snap.logs) ? snap.logs : [];
  els.logCountBadge.textContent = `${logs.length}줄`;
  els.logOutput.textContent = logs.join("\n");
  els.logOutput.scrollTop = els.logOutput.scrollHeight;
}

async function refreshStatus() {
  try {
    const res = await fetch("/_control/status", { cache: "no-store" });
    if (!res.ok) return;
    const data = await res.json();
    renderStatus(data);
  } catch {}
}

els.copyLanBtn.addEventListener("click", async () => {
  const ok = await copyText(latestLanUrl);
  showFeedback(ok ? `접속 주소를 복사했습니다: ${latestLanUrl}` : "클립보드 복사에 실패했습니다.");
});

els.openWebBtn.addEventListener("click", async () => {
  await fetch("/_control/open-web", { method: "POST" });
  showFeedback("기본 웹 브라우저에서 꼬미시간 관리 화면을 엽니다.");
});

els.backupBtn.addEventListener("click", async () => {
  const res = await fetch("/_control/backup", { method: "POST" });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    showFeedback(data.error || "백업 생성 실패");
    return;
  }
  showFeedback(`암호화 백업 완료: ${data.backupFile}`);
  refreshStatus();
});

const openFolder = async () => {
  await fetch("/_control/open-folder", { method: "POST" });
};
els.openFolderBtn.addEventListener("click", openFolder);
els.errorOpenFolderBtn.addEventListener("click", openFolder);

els.quitBtn.addEventListener("click", async () => {
  if (!quitArmed) {
    quitArmed = true;
    els.quitBtn.textContent = "다시 눌러 종료 확인";
    showFeedback("종료하면 연결된 교사 PC가 끊깁니다. 종료하려면 버튼을 한 번 더 누르세요.");
    clearTimeout(quitArmTimer);
    quitArmTimer = setTimeout(() => {
      quitArmed = false;
      els.quitBtn.textContent = "서버 안전 종료";
    }, 4500);
    return;
  }
  els.quitBtn.disabled = true;
  els.quitBtn.textContent = "안전 종료 중...";
  await fetch("/_control/quit", { method: "POST" });
});

refreshStatus();
setInterval(refreshStatus, 1200);
