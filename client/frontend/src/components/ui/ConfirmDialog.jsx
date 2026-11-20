import React from "react";
import * as AlertDialog from "@radix-ui/react-alert-dialog";
import { AlertTriangle } from "lucide-react";

// ConfirmDialog는 초기화·복원·삭제 등 되돌리기 어려운 작업을 수행하기 전
// Radix AlertDialog 기반으로 키보드 포커스 잠금과 명확한 확인 절차를 제공합니다.
export default function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "확인",
  cancelLabel = "취소",
  danger = false,
  busy = false,
  onConfirm,
}) {
  return (
    <AlertDialog.Root open={open} onOpenChange={onOpenChange}>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="radix-alert-overlay" />
        <AlertDialog.Content className="radix-alert-content">
          <AlertDialog.Title className="radix-alert-title">
            {danger && <AlertTriangle size={18} color="#e03131" />}
            <span>{title}</span>
          </AlertDialog.Title>
          <AlertDialog.Description className="radix-alert-description">
            {description}
          </AlertDialog.Description>
          <div className="radix-alert-actions">
            <AlertDialog.Cancel asChild>
              <button type="button" className="btn btn-sm" disabled={busy}>
                {cancelLabel}
              </button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <button
                type="button"
                className={`btn btn-sm ${danger ? "btn-danger" : "btn-primary"}`}
                disabled={busy}
                onClick={() => onConfirm?.()}
              >
                {confirmLabel}
              </button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
