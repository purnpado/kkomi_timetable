import React, { useEffect, useMemo, useState } from "react";
import { Building2, CheckCircle2, Download, Plus, Save, Sparkles, Trash2, Upload } from "lucide-react";
import { apiFetch, hasRegularClasses, isFacultyTeacher } from "../../events/eventStore";
import { downloadAuthenticatedFile } from "../../events/downloadHelper";
import { FoldCard, HelpTip, ProgressBar, SwitchToggle } from "../../components/ui/SmartVisualKit";
import CurriculumDiagnosticsBox from "./CurriculumDiagnosticsBox";

const DAY_LABELS = [
  { key: 1, label: "월" },
  { key: 2, label: "화" },
  { key: 3, label: "수" },
  { key: 4, label: "목" },
  { key: 5, label: "금" },
];

// CurriculumLoadManager는 컴시간(주간시간표) 구조에 맞춰
// [학급수·요일별 기준시수·교과교실 설정], [교사별 시수표 매트릭스], [엑셀 일괄 입출력], [시수 기반 자동 편성]을 통합 제공합니다.
export default function CurriculumLoadManager({ serverUrl, user, teachers, onRefresh, onNotify }) {
  const [config, setConfig] = useState(null);
  const [loads, setLoads] = useState([]);
  const [summary, setSummary] = useState(null);
  const [newRoomName, setNewRoomName] = useState("");
  const [newRoomSubj, setNewRoomSubj] = useState("과학");
  const [newRoomMax, setNewRoomMax] = useState(1);
  const [clearBeforeGen, setClearBeforeGen] = useState(true);
  const [autoGenReport, setAutoGenReport] = useState(null);

  const facultyTeachers = useMemo(() => (teachers || []).filter(isFacultyTeacher), [teachers]);
  const regularTeachers = useMemo(() => {
    const list = facultyTeachers.filter(hasRegularClasses);
    return list.length > 0 ? list : facultyTeachers;
  }, [facultyTeachers]);

  const [formGrade, setFormGrade] = useState(1);
  const [formTeacherId, setFormTeacherId] = useState(regularTeachers[0]?.id || "");
  const [formCoTeacherIds, setFormCoTeacherIds] = useState([]);
  const [formSubject, setFormSubject] = useState("국어");
  const [formRoom, setFormRoom] = useState("");
  const [formBlock, setFormBlock] = useState(1);
  const [formSimGroup, setFormSimGroup] = useState("");
  const [formClassHours, setFormClassHours] = useState({});

  useEffect(() => {
    if (regularTeachers.length > 0 && !regularTeachers.some((t) => t.id === formTeacherId)) {
      setFormTeacherId(regularTeachers[0].id);
    }
  }, [regularTeachers, formTeacherId]);

  const fetchCurriculum = async () => {
    try {
      const data = await apiFetch(serverUrl, "/api/timetable/loads", user.id);
      setLoads(data.loads || []);
      setSummary(data.summary || null);
      setConfig(data.config || null);
    } catch (err) {
      onNotify("시수표 조회 실패", err.message, true);
    }
  };

  useEffect(() => {
    fetchCurriculum();
  }, [serverUrl, user?.id]);

  const gradeClasses = useMemo(() => {
    const cnt = config?.gradeClassCounts?.[formGrade] || 2;
    return Array.from({ length: cnt }, (_, i) => `${formGrade}-${i + 1}`);
  }, [config, formGrade]);

  const handleSaveConfig = async () => {
    if (!config) return;
    try {
      const saved = await apiFetch(serverUrl, "/api/timetable/config", user.id, {
        method: "POST",
        body: JSON.stringify(config),
      });
      setConfig(saved);
      await fetchCurriculum();
      onNotify("학교 기본사항 저장 완료", "학년별 학급수, 요일별 시수 및 교과교실 설정이 반영되었습니다.");
    } catch (err) {
      onNotify("설정 저장 실패", err.message, true);
    }
  };

  const handleSaveLoad = async (e) => {
    e.preventDefault();
    try {
      const cleanHours = {};
      gradeClasses.forEach((cls) => {
        const val = Number(formClassHours[cls] || 0);
        if (val > 0) cleanHours[cls] = val;
      });
      if (Object.keys(cleanHours).length === 0) {
        onNotify("입력 확인", "최소 1개 반 이상 주당 시수를 입력해 주세요.", true);
        return;
      }
      await apiFetch(serverUrl, "/api/timetable/loads", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacherId: formTeacherId,
          coTeacherId: formCoTeacherIds[0] || "",
          coTeacherIds: formCoTeacherIds,
          grade: Number(formGrade),
          subject: formSubject.trim(),
          dedicatedRoom: formRoom.trim(),
          blockHours: Number(formBlock) || 1,
          simultaneousGroup: formSimGroup.trim(),
          classHours: cleanHours,
        }),
      });
      setFormClassHours({});
      setFormCoTeacherIds([]);
      await fetchCurriculum();
      onNotify("시수표 반영 완료", `${formGrade}학년 ${formSubject} 시수가 등록되었습니다.`);
    } catch (err) {
      onNotify("시수 저장 실패", err.message, true);
    }
  };

  const handleDeleteLoad = async (id) => {
    try {
      await apiFetch(serverUrl, `/api/timetable/loads?id=${encodeURIComponent(id)}`, user.id, { method: "DELETE" });
      await fetchCurriculum();
    } catch (err) {
      onNotify("삭제 실패", err.message, true);
    }
  };

  const handleImportLoadsXLSX = async (file) => {
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/import-loads-xlsx", user.id, { method: "POST", body: form });
      await fetchCurriculum();
      onNotify("시수표 엑셀 업로드 완료", `총 ${res.importedCount}개 과목 시수 행이 반영되었습니다.`);
    } catch (err) {
      onNotify("시수표 업로드 실패", err.message, true);
    }
  };

  const handleGenerateFromLoads = async () => {
    try {
      const res = await apiFetch(serverUrl, "/api/timetable/auto-generate", user.id, {
        method: "POST",
        body: JSON.stringify({ useCurriculum: true, clearExisting: clearBeforeGen }),
      });
      setAutoGenReport(res);
      onNotify(
        "시수표 기반 자동 편성 완료",
        `총 ${res.createdCount}개 교시 배정 · FET 연쇄 복구 ${res.ejectionRepairedCount || 0}건 · 미배정 ${res.unplacedCount || 0}건`
      );
      onRefresh();
    } catch (err) {
      onNotify("자동 편성 실패", err.message, true);
    }
  };

  if (!config) return <div className="card">학교 기본사항 및 시수표 데이터를 불러오는 중입니다...</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div className="grid-2">
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15 }}>1. 학년별 학급수 & 요일별 기준 시수</h3>
            <button className="btn btn-sm btn-primary" onClick={handleSaveConfig}>
              <Save size={13} /> 기본사항 저장
            </button>
          </div>
          <table className="table">
            <thead>
              <tr><th>학년</th><th>실제 학급수</th><th>가상(선택)반</th>{DAY_LABELS.map((d) => <th key={d.key}>{d.label}</th>)}<th>주 계</th></tr>
            </thead>
            <tbody>
              {[1, 2, 3].map((g) => {
                const limits = config.dailyPeriodLimits?.[g] || { 1: 6, 2: 6, 3: 6, 4: 6, 5: 6 };
                const rowSum = DAY_LABELS.reduce((acc, d) => acc + Number(limits[d.key] || 0), 0);
                return (
                  <tr key={g}>
                    <td><b>{g}학년</b></td>
                    <td>
                      <input
                        className="input"
                        type="number"
                        min={1}
                        max={15}
                        style={{ width: 54, padding: "4px 6px" }}
                        value={config.gradeClassCounts?.[g] || 2}
                        onChange={(e) => setConfig({ ...config, gradeClassCounts: { ...config.gradeClassCounts, [g]: Number(e.target.value) } })}
                      />
                    </td>
                    <td>
                      <input
                        className="input"
                        type="number"
                        min={0}
                        max={10}
                        style={{ width: 54, padding: "4px 6px" }}
                        value={config.virtualClassCounts?.[g] || 0}
                        onChange={(e) => setConfig({ ...config, virtualClassCounts: { ...config.virtualClassCounts, [g]: Number(e.target.value) } })}
                      />
                    </td>
                    {DAY_LABELS.map((d) => (
                      <td key={d.key}>
                        <input
                          className="input"
                          type="number"
                          min={0}
                          max={8}
                          style={{ width: 46, padding: "4px 5px" }}
                          value={limits[d.key] ?? 6}
                          onChange={(e) =>
                            setConfig({
                              ...config,
                              dailyPeriodLimits: { ...config.dailyPeriodLimits, [g]: { ...limits, [d.key]: Number(e.target.value) } },
                            })
                          }
                        />
                      </td>
                    ))}
                    <td><span className="badge badge-blue">{rowSum}시간</span></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 15, display: "flex", alignItems: "center", gap: 6 }}>
              <Building2 size={15} /> 2. 교과교실제 · 특별실(동시수용)
              <HelpTip
                label="교과교실제 안내"
                text="교과교실제를 켜면 체육관·과학실 등 특별실 동시 수용 한도까지 시간표 중복을 차단하고, 끄면 일반 학급교실 기준으로 자유롭게 편성합니다."
              />
            </h3>
            <SwitchToggle
              checked={config.subjectClassroomMode !== false}
              onCheckedChange={(checked) => setConfig({ ...config, subjectClassroomMode: checked })}
              label="교과교실제 활성화"
            />
          </div>
          <div style={{ display: "flex", gap: 6, marginBottom: 10 }}>
            <input className="input" placeholder="강의실명 (예: 체육관)" value={newRoomName} onChange={(e) => setNewRoomName(e.target.value)} />
            <input className="input" style={{ width: 90 }} placeholder="담당과목" value={newRoomSubj} onChange={(e) => setNewRoomSubj(e.target.value)} />
            <input className="input" type="number" min={1} max={10} style={{ width: 72 }} title="동시 수용 학급수" value={newRoomMax} onChange={(e) => setNewRoomMax(e.target.value)} placeholder="동시수" />
            <button
              className="btn btn-sm btn-primary"
              onClick={() => {
                if (!newRoomName.trim()) return;
                const next = [
                  ...(config.specialRooms || []),
                  { id: `rm-${Date.now()}`, name: newRoomName.trim(), subject: newRoomSubj.trim(), capacity: 35, maxConcurrent: Number(newRoomMax) || 1 },
                ];
                setConfig({ ...config, specialRooms: next });
                setNewRoomName("");
                setNewRoomMax(1);
              }}
            >
              <Plus size={13} /> 추가
            </button>
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {(config.specialRooms || []).map((rm) => (
              <span key={rm.id} className="badge badge-gray" style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "5px 10px" }}>
                <b>{rm.name}</b> ({rm.subject || "공용"} · 동시{rm.maxConcurrent || 1}반)
                <button
                  type="button"
                  style={{ border: "none", background: "none", cursor: "pointer", color: "#e03131", fontWeight: 700 }}
                  onClick={() => setConfig({ ...config, specialRooms: config.specialRooms.filter((r) => r.id !== rm.id) })}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
          <div style={{ fontSize: 11.5, color: "#64748b", marginTop: 10 }}>
            ※ 동시 수용 한도(예: 체육관 2반)까지 허용하며, 교과교실제 Off 시 전용실 중복 차단 없이 학급교실 기준으로 편성됩니다.
          </div>
        </div>
      </div>

      {summary && (
        <div className="card">
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 8 }}>
            <div style={{ fontWeight: 700, fontSize: 14 }}>
              <CheckCircle2 size={15} style={{ verticalAlign: "middle", color: "#087f5b" }} /> 학급별 기준 시수 vs 시수표 배당 일치성 검사 (전체 {summary.totalAssigned} / {summary.totalRequired}시간)
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() =>
                  downloadAuthenticatedFile(serverUrl, "/api/export/xlsx?kind=loads", "교사별시수표.xlsx")
                    .catch((err) => onNotify("시수표 다운로드 실패", err.message, true))
                }
              >
                <Download size={13} /> 교사별 시수표 엑셀 (.xlsx)
              </button>
              <label className="btn btn-sm" style={{ cursor: "pointer" }}>
                <Upload size={13} /> 시수표 엑셀 일괄 업로드
                <input type="file" accept=".xlsx" style={{ display: "none" }} onChange={(e) => handleImportLoadsXLSX(e.target.files?.[0])} />
              </label>
              <label style={{ fontSize: 12, display: "flex", alignItems: "center", gap: 4 }}>
                <input type="checkbox" checked={clearBeforeGen} onChange={(e) => setClearBeforeGen(e.target.checked)} />
                기존(비고정) 초기화
              </label>
              <button className="btn btn-sm btn-green" onClick={handleGenerateFromLoads}>
                <Sparkles size={13} /> 시수표 기반 자동 시간표 편성
              </button>
            </div>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(175px, 1fr))", gap: 8 }}>
            {(summary.classStatuses || []).map((cs) => {
              const tone = cs.status === "MATCH" ? "green" : cs.status === "SHORT" ? "amber" : "red";
              const statusText = cs.status === "MATCH" ? "일치" : cs.status === "SHORT" ? "부족" : "초과";
              return (
                <div key={cs.className} style={{ padding: 9, borderRadius: 10, border: "1px solid var(--border)", background: "var(--surface-alt)" }}>
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 4 }}>
                    <b style={{ fontSize: 12.5 }}>{cs.className}반</b>
                    <span className={`badge badge-${tone}`} style={{ fontVariantNumeric: "tabular-nums" }}>
                      {cs.assignedHours}/{cs.requiredHours}시간 ({statusText})
                    </span>
                  </div>
                  <ProgressBar value={cs.assignedHours} max={cs.requiredHours} tone={tone} showPct={false} height={6} />
                </div>
              );
            })}
          </div>
        </div>
      )}

      <CurriculumDiagnosticsBox report={autoGenReport} />

      <FoldCard title="3. 교사별 · 학급별 시수표 등록부 추가" badge={`총 ${loads.length}건`} defaultOpen={false}>
        <form onSubmit={handleSaveLoad} style={{ display: "flex", flexDirection: "column", gap: 10, padding: 12, background: "#f8fafc", borderRadius: 10 }}>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: 8 }}>
            <select className="select" value={formGrade} onChange={(e) => setFormGrade(Number(e.target.value))}>
              {[1, 2, 3].map((g) => <option key={g} value={g}>{g}학년</option>)}
            </select>
            <select
              className="select"
              value={formTeacherId}
              onChange={(e) => {
                setFormTeacherId(e.target.value);
                const tc = regularTeachers.find((t) => t.id === e.target.value);
                if (tc) {
                  setFormSubject(tc.subject || "");
                  setFormRoom(tc.defaultRoom || "");
                }
              }}
            >
              {regularTeachers.map((t) => <option key={t.id} value={t.id}>{t.name} ({t.subject})</option>)}
            </select>
            <input className="input" placeholder="과목명" value={formSubject} onChange={(e) => setFormSubject(e.target.value)} required />
            <select className="select" value={formRoom} onChange={(e) => setFormRoom(e.target.value)}>
              <option value="">일반 학급교실</option>
              {(config.specialRooms || []).map((rm) => <option key={rm.id} value={rm.name}>{rm.name}</option>)}
            </select>
            <input className="input" type="number" min={1} max={4} title="연속수업 시수(1=단시간, 2=2연강)" value={formBlock} onChange={(e) => setFormBlock(e.target.value)} placeholder="연속시수" />
            <input className="input" value={formSimGroup} onChange={(e) => setFormSimGroup(e.target.value)} placeholder="동시수업 그룹(선택)" />
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
            <b style={{ fontSize: 12 }}>복수 코티칭 협력교사 (선택):</b>
            {regularTeachers
              .filter((t) => t.id !== formTeacherId)
              .map((t) => {
                const picked = formCoTeacherIds.includes(t.id);
                return (
                  <button
                    key={t.id}
                    type="button"
                    className={`btn btn-sm ${picked ? "btn-primary" : ""}`}
                    style={{ fontSize: 11, padding: "2px 7px" }}
                    onClick={() =>
                      setFormCoTeacherIds(picked ? formCoTeacherIds.filter((x) => x !== t.id) : [...formCoTeacherIds, t.id])
                    }
                  >
                    {picked ? "선택 · " : "+ "}
                    {t.name}
                  </button>
                );
              })}
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <b style={{ fontSize: 12 }}>학급별 주당 시수 입력:</b>
            {gradeClasses.map((cls) => (
              <label key={cls} style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12 }}>
                {cls}:
                <input
                  className="input"
                  type="number"
                  min={0}
                  max={10}
                  style={{ width: 52, padding: "4px 6px" }}
                  value={formClassHours[cls] || ""}
                  onChange={(e) => setFormClassHours({ ...formClassHours, [cls]: e.target.value })}
                />
              </label>
            ))}
            <button type="submit" className="btn btn-sm btn-primary" style={{ marginLeft: "auto" }}>
              <Plus size={13} /> 시수표 행 저장
            </button>
          </div>
        </form>
      </FoldCard>

      <div className="card">
        <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>4. 등록된 과목별 시수 현황</h3>
        <div style={{ maxHeight: 280, overflowY: "auto" }}>
          <table className="table">
            <thead>
              <tr><th>학년</th><th>교사</th><th>과목</th><th>교과교실(특별실)</th><th>연속/동시</th><th>학급별 배당 시수</th><th>계</th><th>삭제</th></tr>
            </thead>
            <tbody>
              {loads.map((ld) => (
                <tr key={ld.id}>
                  <td>{ld.grade}학년</td>
                  <td><b>{ld.teacherName}</b></td>
                  <td>{ld.subject} ({ld.shortSubject})</td>
                  <td>{ld.dedicatedRoom || "학급교실"}</td>
                  <td>{ld.blockHours >= 2 ? `${ld.blockHours}연강` : "단시수"}{ld.simultaneousGroup ? ` · ${ld.simultaneousGroup}` : ""}</td>
                  <td style={{ fontSize: 12 }}>
                    {Object.entries(ld.classHours || {}).sort().map(([c, h]) => `${c}(${h})`).join(", ")}
                  </td>
                  <td><b>{ld.totalHours}</b></td>
                  <td>
                    <button className="btn btn-sm btn-danger" onClick={() => handleDeleteLoad(ld.id)}>
                      <Trash2 size={12} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
