import React from "react";
import { Columns, Search } from "lucide-react";
import AppTooltip from "./AppTooltip";

// TopBar는 상단 빠른 검색(Ctrl+K), 수업 중 방해금지 상태, WebSocket 실시간 상태 및 사이드 위젯 전환 버튼을 제공합니다.
export default function TopBar({ inClassStatus, wsStatus, clockText, onOpenPalette, onEnableDock }) {
  return (
    <header className="topbar no-print">
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <AppTooltip content="교사 현재 위치·학급·행사를 즉시 검색합니다 (Ctrl+K)">
          <button type="button" className="btn btn-sm" onClick={onOpenPalette}>
            <Search size={14} /> 교사 위치·학급·행사 검색 <span className="badge badge-gray">Ctrl+K</span>
          </button>
        </AppTooltip>
        <span className={`badge ${inClassStatus.inClass ? "badge-amber" : "badge-green"}`}>
          {inClassStatus.inClass ? `🔕 수업 중 방해금지 (${inClassStatus.periodLabel})` : "🔔 쉬는시간 실시간 알림"}
        </span>
        <span className={`badge ${wsStatus === "connected" ? "badge-green" : "badge-amber"}`}>
          {wsStatus === "connected" ? "🟢 실시간 연결" : "🟡 재연결 중"}
        </span>
      </div>
      <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
        <span className="badge badge-blue">🕒 {clockText}</span>
        <AppTooltip content="화면 측면에 260px 미니 위젯 모드로 고정합니다">
          <button type="button" className="btn btn-sm" onClick={onEnableDock}>
            <Columns size={14} /> 사이드 위젯 (260px)
          </button>
        </AppTooltip>
      </div>
    </header>
  );
}
