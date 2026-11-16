import React, { useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { ArrowRight, CheckCircle2, XCircle } from "lucide-react";

const DAYS = ["", "월", "화", "수", "목", "금"];
const REJECT_CHIPS = ["동시간대 시간표 충돌", "학년 행사 중복", "담임교사 협의 필요", "연강 제한 초과"];

// ApprovalModal은 시간표 담당교사가 대기열(STAGING) 요청의 변경 전/후 미니 그리드를 비교하고
// [최종 선택(서버 반영)] 또는 연속 승인(승인 후 다음 건 열기)을 수행하는 Radix Dialog 모달입니다.
export default function ApprovalModal({ open, onOpenChange, queue, currentIndex, onCommit }) {
  const [rejectReason, setRejectReason] = useState("");
  if (!queue || queue.length === 0) return null;
  const item = queue[Math.min(currentIndex, queue.length - 1)];
  if (!item) return null;

  const src = item.sourceCell || {};
  const tgt = item.targetCell || {};

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
            <Dialog.Title style={{ margin: 0, fontSize: 17, fontWeight: 800 }}>
              수업 변경·결보강 전/후 비교 승인 ({currentIndex + 1} / {queue.length})
            </Dialog.Title>
            <span className="badge badge-amber">대기열(STAGING) · 담당자 최종 선택 전</span>
          </div>

          <div style={{ fontSize: 13, color: "#475569", marginBottom: 14 }}>
            적용일 <b>{item.targetDate}</b> · 요청자 <b>{item.requesterName}</b> · 유형 <b>{item.requestType}</b> · 사유:{" "}
            {item.reason}
          </div>

          <div className="grid-2" style={{ marginBottom: 16 }}>
            <div className="card" style={{ background: "#f8fafc" }}>
              <div style={{ fontWeight: 700, fontSize: 12.5, color: "#64748b", marginBottom: 8 }}>변경 전 (원 시간표)</div>
              <div style={{ padding: 12, borderRadius: 8, background: "#fff", border: "1px solid #cbd5e1" }}>
                <div style={{ fontWeight: 700 }}>{src.className}반 {DAYS[src.dayOfWeek] || ""} {src.period}교시</div>
                <div style={{ fontSize: 14, marginTop: 4 }}>{src.subject} ({item.requesterName})</div>
                <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>교실: {src.room || "-"}</div>
              </div>
            </div>

            <div className="card" style={{ background: "#edf2ff", borderColor: "#91a7ff" }}>
              <div style={{ fontWeight: 700, fontSize: 12.5, color: "#364fc7", marginBottom: 8 }}>
                <ArrowRight size={13} style={{ verticalAlign: "middle" }} /> 변경 후 (서버 반영 예정)
              </div>
              <div style={{ padding: 12, borderRadius: 8, background: "#fff", border: "1px solid #748ffc" }}>
                <div style={{ fontWeight: 700 }}>
                  {tgt.className || src.className}반 {DAYS[tgt.dayOfWeek || src.dayOfWeek] || ""} {tgt.period || src.period}교시
                </div>
                <div style={{ fontSize: 14, marginTop: 4 }}>
                  {tgt.subject || src.subject} ({item.partnerName || "보강 미지정"})
                </div>
                <div style={{ fontSize: 12, color: "#64748b", marginTop: 2 }}>교실: {tgt.room || src.room || "-"}</div>
              </div>
            </div>
          </div>

          <div style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 12, fontWeight: 700, marginBottom: 6 }}>반려 시 사유 칩 선택 (또는 직접 입력)</div>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 8 }}>
              {REJECT_CHIPS.map((chip) => (
                <button
                  key={chip}
                  type="button"
                  className="btn btn-sm"
                  style={rejectReason === chip ? { borderColor: "#e03131", color: "#e03131", background: "#fff5f5" } : {}}
                  onClick={() => setRejectReason(chip)}
                >
                  {chip}
                </button>
              ))}
            </div>
            <input
              className="input"
              placeholder="반려 사유를 입력하거나 위 칩을 선택하세요"
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
            />
          </div>

          <div style={{ display: "flex", justifyContent: "space-between", gap: 8, flexWrap: "wrap" }}>
            <button
              className="btn btn-danger"
              onClick={() => {
                onCommit(item.id, false, rejectReason || "담당교사 반려", false);
                setRejectReason("");
              }}
            >
              <XCircle size={15} /> 반려
            </button>

            <div style={{ display: "flex", gap: 8 }}>
              <Dialog.Close asChild>
                <button className="btn">닫기</button>
              </Dialog.Close>
              <button className="btn btn-green" onClick={() => onCommit(item.id, true, "", false)}>
                <CheckCircle2 size={15} /> 최종 선택(서버 반영)
              </button>
              {queue.length > 1 && (
                <button className="btn btn-primary" onClick={() => onCommit(item.id, true, "", true)}>
                  승인 후 다음 건 열기
                </button>
              )}
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
