import React from "react";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { ChevronDown } from "lucide-react";

// ActionMenu는 문서 출력·보조 동작들을 Radix DropdownMenu로 깔끔하게 묶어 제공합니다.
export default function ActionMenu({
  triggerLabel,
  triggerIcon: TriggerIcon,
  items = [],
  align = "end",
  buttonClassName = "btn btn-sm",
}) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button type="button" className={buttonClassName}>
          {TriggerIcon && <TriggerIcon size={14} />}
          <span>{triggerLabel}</span>
          <ChevronDown size={13} />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="radix-menu-content" align={align} sideOffset={6}>
          {items.map((item, idx) => {
            if (item.separator) {
              return <DropdownMenu.Separator key={`sep-${idx}`} className="radix-menu-separator" />;
            }
            const Icon = item.icon;
            return (
              <DropdownMenu.Item
                key={item.key || item.label || idx}
                disabled={item.disabled}
                className={`radix-menu-item ${item.danger ? "radix-menu-item-danger" : ""}`}
                onSelect={() => item.onSelect?.()}
              >
                {Icon && <Icon size={14} />}
                <span>{item.label}</span>
              </DropdownMenu.Item>
            );
          })}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
