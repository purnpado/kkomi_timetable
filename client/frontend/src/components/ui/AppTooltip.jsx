import React from "react";
import * as Tooltip from "@radix-ui/react-tooltip";

// AppTooltipProvider는 애플리케이션 최상위에서 툴팁 지연 시간을 통합 제어합니다.
export function AppTooltipProvider({ children }) {
  return (
    <Tooltip.Provider delayDuration={300} skipDelayDuration={150}>
      {children}
    </Tooltip.Provider>
  );
}

// AppTooltip은 단축키 및 보조 설명을 Radix Tooltip으로 일관되게 표시합니다.
export default function AppTooltip({ content, side = "bottom", children }) {
  if (!content) return children;
  return (
    <Tooltip.Root>
      <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content className="radix-tooltip-content" side={side} sideOffset={6}>
          {content}
          <Tooltip.Arrow className="radix-tooltip-arrow" />
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}
