import React, { useEffect, useRef, useState } from "react";
import { Copy, Download } from "lucide-react";

const DAYS = ["월", "화", "수", "목", "금"];
const PERIODS = [1, 2, 3, 4, 5, 6, 7];

function subjectColor(subject = "") {
  if (subject.includes("국어")) return { bg: "#fff5f5", bar: "#fa5252" };
  if (subject.includes("수학")) return { bg: "#edf2ff", bar: "#4c6ef5" };
  if (subject.includes("영어")) return { bg: "#e6fcf5", bar: "#12b886" };
  if (subject.includes("과학")) return { bg: "#f3f0ff", bar: "#7950f2" };
  if (subject.includes("사회") || subject.includes("역사")) return { bg: "#fff9db", bar: "#fab005" };
  if (subject.includes("정보")) return { bg: "#e3fafc", bar: "#15aabf" };
  return { bg: "#f1f3f5", bar: "#868e96" };
}

function formatHeaderMD(weekStart, dayIdx) {
  if (!weekStart) return "";
  const d = new Date(`${weekStart}T00:00:00`);
  if (Number.isNaN(d.getTime())) return "";
  d.setDate(d.getDate() + dayIdx);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

// TimetableCanvas는 HTML5 Canvas 60fps 기반 HiDPI 시간표 그리드입니다.
// 과목별 컬러 코딩, 교시별 일과시간·주차별 날짜 표시, 드래그 고스트 및 신호등 가이드를 제공합니다.
export default function TimetableCanvas({ cells, teachersMap, bellSchedule = [], filterClass, weekStart = "", onEvaluateDrop, onRequestCommit }) {
  const canvasRef = useRef(null);
  const [hoverInfo, setHoverInfo] = useState(null);
  const [dragState, setDragState] = useState(null); // { sourceCell, x, y, targetDay, targetPeriod, evalResult }
  const [copyMsg, setCopyMsg] = useState("");

  const visibleCells = (cells || []).filter((c) => !filterClass || filterClass === "ALL" || c.className === filterClass);

  const cellAt = (day, period) => visibleCells.find((c) => c.dayOfWeek === day && c.period === period);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const width = 860;
    const height = 470;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;

    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);

    const headerW = 96;
    const headerH = 38;
    const colW = (width - headerW) / 5;
    const rowH = (height - headerH) / 7;

    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, width, height);

    // 요일 헤더
    DAYS.forEach((d, idx) => {
      const x = headerW + idx * colW;
      ctx.fillStyle = "#e8edff";
      ctx.fillRect(x, 0, colW, headerH);
      ctx.strokeStyle = "#cbd5e1";
      ctx.strokeRect(x, 0, colW, headerH);
      ctx.fillStyle = "#1e293b";
      ctx.font = "bold 13px Pretendard, sans-serif";
      ctx.textAlign = "center";
      const md = weekStart ? ` (${formatHeaderMD(weekStart, idx)})` : "";
      ctx.fillText(`${d}요일${md}`, x + colW / 2, 24);
    });

    // 교시 헤더 (일과시간 함께 표시) 및 격자 셀
    PERIODS.forEach((p, rIdx) => {
      const y = headerH + rIdx * rowH;
      const bell = (bellSchedule || []).find((b) => b.period === p);
      ctx.fillStyle = "#f1f5f9";
      ctx.fillRect(0, y, headerW, rowH);
      ctx.strokeStyle = "#cbd5e1";
      ctx.strokeRect(0, y, headerW, rowH);
      ctx.fillStyle = "#0f172a";
      ctx.font = "bold 12px Pretendard, sans-serif";
      ctx.textAlign = "center";
      ctx.fillText(`${p}교시`, headerW / 2, y + rowH / 2 - (bell ? 3 : -4));
      if (bell) {
        ctx.fillStyle = "#64748b";
        ctx.font = "10px Pretendard, sans-serif";
        ctx.fillText(`${bell.startTime}~${bell.endTime}`, headerW / 2, y + rowH / 2 + 12);
      }

      DAYS.forEach((_, dIdx) => {
        const day = dIdx + 1;
        const x = headerW + dIdx * colW;
        const cell = cellAt(day, p);
        const isHovered = hoverInfo && hoverInfo.day === day && hoverInfo.period === p;
        const sameTeacherHover = hoverInfo?.cell && cell && cell.teacherId === hoverInfo.cell.teacherId;
        const sc = cell ? subjectColor(cell.subject) : null;

        let bg = "#ffffff";
        if (cell?.blockedBy) bg = "#fff1f2";
        else if (cell?.changeNote) bg = "#fff9db";
        else if (cell?.electiveGroup) bg = "#f3f0ff";
        else if (sameTeacherHover) bg = "#d0ebff";
        else if (sc) bg = sc.bg;

        if (dragState && dragState.targetDay === day && dragState.targetPeriod === p && dragState.evalResult) {
          if (dragState.evalResult.signal === "GREEN") bg = "#d3f9d8";
          else if (dragState.evalResult.signal === "YELLOW") bg = "#fff3bf";
          else if (dragState.evalResult.signal === "RED") bg = "#ffe3e3";
        }

        ctx.fillStyle = bg;
        ctx.fillRect(x + 1, y + 1, colW - 2, rowH - 2);
        if (sc && !cell?.blockedBy) {
          ctx.fillStyle = sc.bar;
          ctx.fillRect(x + 1, y + 1, 4, rowH - 2);
        }
        ctx.strokeStyle = isHovered ? "#3b5bdb" : "#e2e8f0";
        ctx.lineWidth = isHovered ? 2 : 1;
        ctx.strokeRect(x, y, colW, rowH);
        ctx.lineWidth = 1;

        if (cell) {
          const tName = teachersMap[cell.teacherId]?.name || cell.teacherId;
          ctx.textAlign = "left";
          ctx.fillStyle = cell.blockedBy ? "#c92a2a" : "#0f172a";
          ctx.font = "bold 12.5px Pretendard, sans-serif";
          ctx.fillText(`${cell.subject}`, x + 10, y + 20);
          ctx.fillStyle = "#475569";
          ctx.font = "11px Pretendard, sans-serif";
          const isSpecialRoom = cell.room && cell.room !== `${cell.className}교실`;
          const subLine = cell.blockedBy
            ? `행사블록: ${cell.blockedBy}`
            : `${cell.className} · ${tName}${isSpecialRoom ? ` (${cell.room})` : ""}`;
          ctx.fillText(subLine.slice(0, 21), x + 10, y + 37);
          if (cell.changeNote) {
            ctx.fillStyle = "#b86e00";
            ctx.font = "bold 10px Pretendard, sans-serif";
            ctx.fillText(cell.changeNote.slice(0, 24), x + 10, y + 52);
          } else if (cell.coTeacherId || cell.electiveGroup) {
            ctx.fillStyle = "#5f3dc4";
            ctx.font = "10.5px Pretendard, sans-serif";
            ctx.fillText(cell.electiveGroup ? `선택블록: ${cell.electiveGroup}` : "코티칭 수업", x + 10, y + 52);
          }
        }
      });
    });

    // 드래그 반투명 고스트 미리보기
    if (dragState?.sourceCell) {
      ctx.save();
      ctx.globalAlpha = 0.85;
      ctx.fillStyle = "#3b5bdb";
      ctx.fillRect(dragState.x - 62, dragState.y - 24, 124, 48);
      ctx.fillStyle = "#ffffff";
      ctx.font = "bold 12px Pretendard, sans-serif";
      ctx.textAlign = "center";
      ctx.fillText(`${dragState.sourceCell.subject} (${dragState.sourceCell.className})`, dragState.x, dragState.y + 4);
      ctx.restore();
    }
  }, [visibleCells, hoverInfo, dragState, teachersMap, bellSchedule, weekStart]);

  const coordToSlot = (clientX, clientY) => {
    const rect = canvasRef.current.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    const headerW = 96;
    const headerH = 38;
    const colW = (860 - headerW) / 5;
    const rowH = (470 - headerH) / 7;
    if (x < headerW || y < headerH) return { x, y, day: 0, period: 0 };
    const day = Math.min(5, Math.max(1, Math.floor((x - headerW) / colW) + 1));
    const period = Math.min(7, Math.max(1, Math.floor((y - headerH) / rowH) + 1));
    return { x, y, day, period };
  };

  const handleMouseDown = (e) => {
    const { x, y, day, period } = coordToSlot(e.clientX, e.clientY);
    const src = cellAt(day, period);
    if (!src) return;
    setDragState({ sourceCell: src, x, y, targetDay: day, targetPeriod: period, evalResult: null });
  };

  const handleMouseMove = async (e) => {
    const { x, y, day, period } = coordToSlot(e.clientX, e.clientY);
    if (!dragState) {
      setHoverInfo({ day, period, cell: cellAt(day, period) || null });
      return;
    }
    const changedSlot = day !== dragState.targetDay || period !== dragState.targetPeriod;
    let nextEval = dragState.evalResult;
    if (changedSlot && day > 0 && period > 0 && onEvaluateDrop) {
      nextEval = await onEvaluateDrop(dragState.sourceCell, day, period);
    }
    setDragState((prev) => (prev ? { ...prev, x, y, targetDay: day, targetPeriod: period, evalResult: nextEval } : null));
  };

  const handleMouseUp = async () => {
    if (dragState?.sourceCell && dragState.targetDay > 0 && dragState.targetPeriod > 0) {
      const samePos = dragState.sourceCell.dayOfWeek === dragState.targetDay && dragState.sourceCell.period === dragState.targetPeriod;
      if (!samePos && dragState.evalResult && dragState.evalResult.signal !== "RED") {
        const target = cellAt(dragState.targetDay, dragState.targetPeriod);
        await onRequestCommit(dragState.sourceCell, target, dragState.targetDay, dragState.targetPeriod, dragState.evalResult);
      }
    }
    setDragState(null);
  };

  const copyCanvasPNG = () => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    canvas.toBlob(async (blob) => {
      if (!blob) return;
      try {
        await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
        setCopyMsg("주간 시간표 이미지가 클립보드에 복사되었습니다!");
      } catch {
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `꼬미시간_시간표_${filterClass || "전체"}.png`;
        a.click();
        URL.revokeObjectURL(url);
        setCopyMsg("PNG 이미지 파일로 저장되었습니다!");
      }
      setTimeout(() => setCopyMsg(""), 3500);
    });
  };

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8, flexWrap: "wrap", gap: 8 }}>
        <div style={{ display: "flex", gap: 8, alignItems: "center", fontSize: 12 }}>
          <span className="badge badge-green">🟢 이동/맞교환 가능</span>
          <span className="badge badge-amber">🟡 삼각·사각 연쇄교환 가능</span>
          <span className="badge badge-red">🔴 충돌 불가(사유 툴팁)</span>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {copyMsg && <span className="badge badge-blue">{copyMsg}</span>}
          <button className="btn btn-sm" onClick={copyCanvasPNG}>
            <Copy size={13} /> 학급 주간 시간표 PNG 복사
          </button>
          <button className="btn btn-sm" onClick={copyCanvasPNG}>
            <Download size={13} /> PNG 저장
          </button>
        </div>
      </div>

      <div style={{ position: "relative", overflowX: "auto", border: "1px solid #cbd5e1", borderRadius: 10, background: "#fff" }}>
        <canvas
          ref={canvasRef}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={() => {
            setHoverInfo(null);
            setDragState(null);
          }}
          style={{ display: "block", cursor: dragState ? "grabbing" : "grab" }}
        />
        {dragState?.evalResult && (
          <div
            style={{
              margin: 8,
              padding: "8px 12px",
              borderRadius: 8,
              fontSize: 12.5,
              fontWeight: 700,
              background: dragState.evalResult.signal === "GREEN" ? "#e6fcf5" : dragState.evalResult.signal === "YELLOW" ? "#fff9db" : "#fff5f5",
              color: dragState.evalResult.signal === "GREEN" ? "#087f5b" : dragState.evalResult.signal === "YELLOW" ? "#b86e00" : "#c92a2a",
            }}
          >
            <div>[{dragState.evalResult.signal}] {dragState.evalResult.reason}</div>
            {dragState.evalResult.chainSteps?.length > 1 && (
              <div style={{ marginTop: 4, fontSize: 11.5, fontWeight: 500 }}>
                {dragState.evalResult.chainSteps.map((s) => s.description).join("  ➜  ")}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
