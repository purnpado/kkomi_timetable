import React from "react";
import * as Toast from "@radix-ui/react-toast";
import { AlertTriangle, Bell, X } from "lucide-react";

// ToastHub는 우측 하단 Sticky 알림 및 수업 중 방해 금지(쉬는 시간 일괄 브리핑) 큐를 표시합니다.
export default function ToastHub({ toasts, queuedAlerts, inClassStatus, onDismiss, onFlushQueue }) {
  return (
    <Toast.Provider swipeDirection="right">
      {queuedAlerts.length > 0 && (
        <div
          className="no-print"
          style={{
            position: "fixed",
            bottom: 18,
            left: 242,
            zIndex: 75,
            background: "#1e293b",
            color: "#fff",
            padding: "9px 14px",
            borderRadius: 10,
            display: "flex",
            alignItems: "center",
            gap: 10,
            boxShadow: "0 10px 26px rgba(0,0,0,0.2)",
            fontSize: 12.5,
          }}
        >
          <Bell size={15} color="#ffd43b" />
          <span>
            수업 중 방해 금지 ({inClassStatus.periodLabel}) · 대기 알림 <b>{queuedAlerts.length}건</b>
          </span>
          <button className="btn btn-sm btn-primary" onClick={onFlushQueue}>
            쉬는시간 브리핑 즉시 보기
          </button>
        </div>
      )}

      {toasts.map((t) => (
        <Toast.Root
          key={t.id}
          open={true}
          duration={t.urgent ? 12000 : 6000}
          onOpenChange={(open) => !open && onDismiss(t.id)}
          className={`toast-root ${t.urgent ? "urgent" : ""}`}
        >
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
            <Toast.Title style={{ fontWeight: 700, fontSize: 13.5, display: "flex", alignItems: "center", gap: 6 }}>
              {t.urgent ? <AlertTriangle size={14} color="#fca5a5" /> : <Bell size={14} color="#ffd43b" />}
              <span>{t.title}</span>
            </Toast.Title>
            <Toast.Close asChild>
              <button
                onClick={() => onDismiss(t.id)}
                style={{ background: "transparent", border: 0, color: "#cbd5e1", cursor: "pointer", padding: 0 }}
              >
                <X size={15} />
              </button>
            </Toast.Close>
          </div>
          <Toast.Description style={{ fontSize: 12.5, color: "#e2e8f0", marginTop: 4, lineHeight: 1.45 }}>
            {t.message}
          </Toast.Description>
        </Toast.Root>
      ))}
      <Toast.Viewport className="toast-viewport" />
    </Toast.Provider>
  );
}
