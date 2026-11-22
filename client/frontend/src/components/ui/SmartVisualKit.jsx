import React, { useState } from "react";
import * as Collapsible from "@radix-ui/react-collapsible";
import * as Popover from "@radix-ui/react-popover";
import * as Progress from "@radix-ui/react-progress";
import * as Switch from "@radix-ui/react-switch";
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, HelpCircle, Info } from "lucide-react";

// StatusDot는 4가지 의미 색상(좋음·주의·나쁨·안내) 또는 회색 상태 도트를 표시합니다.
export function StatusDot({ tone = "gray", label }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
      <span className={`status-dot status-dot-${tone}`} aria-hidden="true" />
      {label && <span>{label}</span>}
    </span>
  );
}

// HelpTip은 @radix-ui/react-popover 기반으로 클릭·키보드·호버 접근성을 모두 지원하는 도움말 팝오버입니다.
export function HelpTip({ label = "도움말", text }) {
  const [pinned, setPinned] = useState(false);
  const [hovered, setHovered] = useState(false);
  if (!text) return null;
  const open = pinned || hovered;
  return (
    <Popover.Root open={pinned} onOpenChange={setPinned}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="help-tip-btn no-print"
          aria-label={`${label} 도움말`}
          title={`${label} 도움말`}
          onMouseEnter={() => setHovered(true)}
          onMouseLeave={() => setHovered(false)}
        >
          <HelpCircle size={13} />
        </button>
      </Popover.Trigger>
      {open && (
        <Popover.Portal forceMount>
          <Popover.Content
            className="radix-popover-content no-print"
            sideOffset={6}
            onPointerDownOutside={() => { setPinned(false); setHovered(false); }}
            onEscapeKeyDown={() => { setPinned(false); setHovered(false); }}
          >
            <div style={{ fontWeight: 700, marginBottom: 4, color: "#0f172a" }}>{label}</div>
            <div style={{ color: "#334155", lineHeight: 1.5 }}>{text}</div>
          </Popover.Content>
        </Popover.Portal>
      )}
    </Popover.Root>
  );
}

// FoldCard는 @radix-ui/react-collapsible 기반 접이식 카드·입력 폼 컨테이너입니다.
export function FoldCard({ title, subtitle, badge, defaultOpen = false, actions, children }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="card">
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
        <Collapsible.Trigger asChild>
          <button type="button" className="fold-card-trigger">
            {open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
            <span>{title}</span>
            {badge && <span className="badge badge-blue" style={{ fontVariantNumeric: "tabular-nums" }}>{badge}</span>}
          </button>
        </Collapsible.Trigger>
        {actions && <div className="no-print" style={{ display: "flex", gap: 6, flexWrap: "wrap", alignItems: "center" }}>{actions}</div>}
      </div>
      {subtitle && <div style={{ fontSize: 12, color: "#64748b", marginTop: 4, marginLeft: 22 }}>{subtitle}</div>}
      <Collapsible.Content style={{ marginTop: 12 }}>
        {children}
      </Collapsible.Content>
    </Collapsible.Root>
  );
}

// SwitchToggle은 @radix-ui/react-switch 기반의 접근성 토글 스위치입니다.
export function SwitchToggle({ checked, onCheckedChange, label, disabled = false }) {
  return (
    <label style={{ display: "inline-flex", alignItems: "center", gap: 8, cursor: disabled ? "not-allowed" : "pointer", fontSize: 12.5, fontWeight: 700 }}>
      <Switch.Root className="radix-switch-root" checked={Boolean(checked)} onCheckedChange={onCheckedChange} disabled={disabled}>
        <Switch.Thumb className="radix-switch-thumb" />
      </Switch.Root>
      {label && <span>{label}</span>}
    </label>
  );
}

// ProgressBar는 @radix-ui/react-progress 기반 시수·모집률·진행률 게이지 바입니다.
export function ProgressBar({ value = 0, max = 100, tone = "blue", label, showPct = true, height = 8 }) {
  const safeMax = Math.max(1, Number(max) || 1);
  const rawPct = Math.round((Number(value || 0) / safeMax) * 100);
  const pct = Math.max(0, Math.min(100, rawPct));
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      {(label || showPct) && (
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", fontSize: 11.5, color: "#475569", fontVariantNumeric: "tabular-nums" }}>
          <span>{label || ""}</span>
          {showPct && <b style={{ color: "#0f172a" }}>{rawPct}%</b>}
        </div>
      )}
      <Progress.Root className="radix-progress-root" value={pct} max={100} style={{ height }}>
        <Progress.Indicator
          className={`radix-progress-indicator progress-tone-${tone}`}
          style={{ transform: `translateX(-${100 - pct}%)` }}
        />
      </Progress.Root>
    </div>
  );
}

// AlertBox는 흩어진 경고·주의 안내를 한 상자에 모아 표시합니다. 항목이 없으면 렌더링하지 않습니다.
export function AlertBox({ tone = "amber", title, items = [] }) {
  if (!items || items.length === 0) return null;
  const Icon = tone === "green" ? CheckCircle2 : tone === "blue" ? Info : AlertTriangle;
  return (
    <div className={`alert-box alert-box-${tone}`}>
      <div style={{ display: "flex", alignItems: "center", gap: 6, fontWeight: 800, fontSize: 13 }}>
        <Icon size={15} />
        <span>{title}</span>
        <span className={`badge badge-${tone}`} style={{ fontVariantNumeric: "tabular-nums" }}>{items.length}건</span>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 }}>
        {items.map((item, idx) => (
          <div key={idx} style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
            {item.badge && <span className={`badge badge-${item.tone || tone}`}>{item.badge}</span>}
            <span>{item.message || String(item)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
