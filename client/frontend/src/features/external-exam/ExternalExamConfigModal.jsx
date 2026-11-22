import React, { useEffect, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Plus, Save, Settings2, Trash2, X } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

// ExternalExamConfigModal은 학교 기본 외부감독 내규 프리셋·시험 종목 가중치 설정 및 공고 수정을 처리합니다.
export default function ExternalExamConfigModal({
  configOpen,
  onConfigOpenChange,
  editNotice,
  onEditNoticeClose,
  schoolConfig,
  serverUrl,
  user,
  onSaved,
  onNotify,
}) {
  const [cfg, setCfg] = useState(schoolConfig || null);
  const [newCatName, setNewCatName] = useState("");
  const [newCatWeight, setNewCatWeight] = useState(1.0);
  const [noticeForm, setNoticeForm] = useState(null);

  useEffect(() => {
    if (schoolConfig) setCfg(schoolConfig);
  }, [schoolConfig]);

  useEffect(() => {
    if (editNotice) setNoticeForm({ ...editNotice });
    else setNoticeForm(null);
  }, [editNotice]);

  const handleSaveSchoolConfig = async (e) => {
    e.preventDefault();
    if (!cfg) return;
    try {
      await apiFetch(serverUrl, "/api/external-exam/config", user.id, {
        method: "POST",
        body: JSON.stringify(cfg),
      });
      onNotify?.("학교 기본 내규 프리셋 저장", "신규 공고 등록 시 기본 종목·가중치·동점 기준이 자동 적용됩니다.");
      onConfigOpenChange(false);
      onSaved?.();
    } catch (err) {
      onNotify?.("내규 저장 실패", err.message, true);
    }
  };

  const handleSaveEditNotice = async (e) => {
    e.preventDefault();
    if (!noticeForm) return;
    try {
      await apiFetch(serverUrl, "/api/external-exam/notices", user.id, {
        method: "PUT",
        body: JSON.stringify({
          ...noticeForm,
          requiredCount: Number(noticeForm.requiredCount) || 1,
        }),
      });
      onNotify?.("외부 시험 공고 수정 완료", `${noticeForm.title} 공고가 업데이트되었습니다.`);
      onEditNoticeClose();
      onSaved?.(noticeForm.id);
    } catch (err) {
      onNotify?.("공고 수정 실패", err.message, true);
    }
  };

  const categories = cfg?.categories || [];

  return (
    <>
      <Dialog.Root open={configOpen} onOpenChange={onConfigOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="radix-dialog-overlay" />
          <Dialog.Content className="radix-dialog-content" style={{ maxWidth: 660 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
              <Dialog.Title style={{ margin: 0, fontSize: 16.5, fontWeight: 700, display: "flex", alignItems: "center", gap: 6 }}>
                <Settings2 size={17} color="#3b5bdb" /> 학교 외부 시험감독 기본 내규 & 종목 관리
              </Dialog.Title>
              <Dialog.Close asChild>
                <button type="button" className="btn btn-sm"><X size={14} /></button>
              </Dialog.Close>
            </div>
            {cfg && (
              <form onSubmit={handleSaveSchoolConfig} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div>
                  <b style={{ fontSize: 13 }}>등록 시험 종목 및 가중치 (기본 3종 + 자유 추가)</b>
                  <div style={{ display: "flex", gap: 6, marginTop: 6, marginBottom: 8 }}>
                    <input className="input" placeholder="새 종목 (예: 전국연합학력평가)" value={newCatName} onChange={(e) => setNewCatName(e.target.value)} />
                    <input className="input" type="number" step="0.5" min="0.5" max="5" style={{ width: 90 }} value={newCatWeight} onChange={(e) => setNewCatWeight(e.target.value)} />
                    <button
                      type="button"
                      className="btn btn-sm btn-primary"
                      onClick={() => {
                        if (!newCatName.trim()) return;
                        setCfg({ ...cfg, categories: [...categories, { name: newCatName.trim(), weight: Number(newCatWeight) || 1.0 }] });
                        setNewCatName("");
                      }}
                    >
                      <Plus size={13} /> 추가
                    </button>
                  </div>
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                    {categories.map((cat, idx) => (
                      <span key={`${cat.name}-${idx}`} className="badge badge-blue" style={{ padding: "5px 10px", gap: 6 }}>
                        <b>{cat.name}</b> (가중치 {cat.weight}점)
                        {categories.length > 1 && (
                          <button type="button" style={{ border: 0, background: "none", cursor: "pointer" }} onClick={() => setCfg({ ...cfg, categories: categories.filter((_, i) => i !== idx) })}>
                            <Trash2 size={11} color="#e03131" />
                          </button>
                        )}
                      </span>
                    ))}
                  </div>
                </div>

                <div className="grid-2">
                  <select className="select" value={cfg.defaultScoringMode} onChange={(e) => setCfg({ ...cfg, defaultScoringMode: e.target.value })}>
                    <option value="TOTAL_COUNT">기본 점수방식: 전체 단순 횟수</option>
                    <option value="WEIGHTED_SCORE">기본 점수방식: 종목 가중치 점수제</option>
                    <option value="SEPARATE_TRACK">기본 점수방식: 종목별 별도 트랙</option>
                  </select>
                  <select className="select" value={cfg.defaultTieBreaker} onChange={(e) => setCfg({ ...cfg, defaultTieBreaker: e.target.value })}>
                    <option value="NAME_ASC">기본 동점기준: 가나다 순</option>
                    <option value="AGE_YOUNG">기본 동점기준: 연령 낮은순</option>
                    <option value="AGE_OLD">기본 동점기준: 연령 높은순</option>
                    <option value="SALARY_LOW">기본 동점기준: 저호봉 우선</option>
                    <option value="SALARY_HIGH">기본 동점기준: 고호봉 우선</option>
                    <option value="LOTTERY">기본 동점기준: 공정 추첨</option>
                  </select>
                </div>

                <div style={{ display: "flex", gap: 14, flexWrap: "wrap", fontSize: 12.5 }}>
                  <label style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                    <input type="checkbox" checked={Boolean(cfg.defaultTransferGraceYear)} onChange={(e) => setCfg({ ...cfg, defaultTransferGraceYear: e.target.checked })} />
                    신규 공고 전입 1년차 유예 기본 ON
                  </label>
                  <label style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                    <input type="checkbox" checked={Boolean(cfg.defaultExcludeExempt)} onChange={(e) => setCfg({ ...cfg, defaultExcludeExempt: e.target.checked })} />
                    면제 대상 후순위 배제 기본 ON
                  </label>
                </div>

                <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                  <Dialog.Close asChild><button type="button" className="btn">닫기</button></Dialog.Close>
                  <button type="submit" className="btn btn-primary"><Save size={14} /> 학교 내규 프리셋 저장</button>
                </div>
              </form>
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root open={Boolean(noticeForm)} onOpenChange={(o) => !o && onEditNoticeClose()}>
        <Dialog.Portal>
          <Dialog.Overlay className="radix-dialog-overlay" />
          <Dialog.Content className="radix-dialog-content" style={{ maxWidth: 520 }}>
            <Dialog.Title style={{ margin: "0 0 12px", fontSize: 16.5, fontWeight: 700 }}>외부 시험 공고 수정</Dialog.Title>
            {noticeForm && (
              <form onSubmit={handleSaveEditNotice} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                <input className="input" value={noticeForm.title} onChange={(e) => setNoticeForm({ ...noticeForm, title: e.target.value })} placeholder="공고명" required />
                <div className="grid-2">
                  <select className="select" value={noticeForm.examCategory || "검정고시"} onChange={(e) => setNoticeForm({ ...noticeForm, examCategory: e.target.value })}>
                    {categories.map((c) => <option key={c.name} value={c.name}>{c.name}</option>)}
                  </select>
                  <input className="input" type="date" value={noticeForm.examDate} onChange={(e) => setNoticeForm({ ...noticeForm, examDate: e.target.value })} required />
                </div>
                <div className="grid-2">
                  <input className="input" value={noticeForm.location || ""} onChange={(e) => setNoticeForm({ ...noticeForm, location: e.target.value })} placeholder="파견 고사장" />
                  <input className="input" type="number" min={1} max={50} value={noticeForm.requiredCount} onChange={(e) => setNoticeForm({ ...noticeForm, requiredCount: e.target.value })} placeholder="필요 인원" />
                </div>
                <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
                  <button type="button" className="btn" onClick={onEditNoticeClose}>취소</button>
                  <button type="submit" className="btn btn-primary"><Save size={14} /> 공고 저장</button>
                </div>
              </form>
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
