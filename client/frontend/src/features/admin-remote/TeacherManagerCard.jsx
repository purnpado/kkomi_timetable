import React, { useState } from "react";
import { Trash2, UserPlus } from "lucide-react";
import { apiFetch } from "../../events/eventStore";

// TeacherManagerCard는 마스터 또는 시간표 담당교사가 서버에 직접 접속하지 않고도
// 클라이언트에서 신규 교사를 등록하거나 담임/과목/전입일/순회요일을 수정하는 관리 카드입니다.
export default function TeacherManagerCard({ serverUrl, user, teachers, onRefresh, onNotify }) {
  const [name, setName] = useState("");
  const [username, setUsername] = useState("");
  const [subject, setSubject] = useState("국어");
  const [position, setPosition] = useState("평교사");
  const [homeroomClass, setHomeroomClass] = useState("");
  const [transferDate, setTransferDate] = useState("2026-03-01");
  const [itinerantDaysText, setItinerantDaysText] = useState("");

  const handleSaveTeacher = async (e) => {
    e.preventDefault();
    if (!name.trim() || !username.trim()) return;
    const itinerantDays = itinerantDaysText
      .split(",")
      .map((s) => Number(s.trim()))
      .filter((n) => n >= 1 && n <= 5);
    try {
      await apiFetch(serverUrl, "/api/teachers", user.id, {
        method: "POST",
        body: JSON.stringify({
          teacher: {
            id: `t-${username.trim()}`,
            username: username.trim(),
            name: name.trim(),
            position: position.trim() || "평교사",
            subject: subject.trim() || "국어",
            homeroomClass: homeroomClass.trim(),
            transferDate,
            itinerantDays,
            roles: ["TEACHER"],
          },
          password: "1234",
        }),
      });
      onNotify("교사 명렬표 반영 완료", `${name} 선생님이 등록(초기 비밀번호: 1234)되었습니다.`);
      setName("");
      setUsername("");
      setHomeroomClass("");
      setItinerantDaysText("");
      onRefresh();
    } catch (err) {
      onNotify("교사 등록 실패", err.message, true);
    }
  };

  const handleDeleteTeacher = async (tid, tname) => {
    try {
      await apiFetch(serverUrl, `/api/teachers?id=${encodeURIComponent(tid)}`, user.id, { method: "DELETE" });
      onNotify("교사 삭제 완료", `${tname} 선생님이 명렬표에서 삭제되었습니다.`);
      onRefresh();
    } catch (err) {
      onNotify("교사 삭제 실패", err.message, true);
    }
  };

  return (
    <div className="card">
      <h3 style={{ margin: "0 0 10px", fontSize: 15 }}>
        <UserPlus size={15} style={{ verticalAlign: "middle" }} /> 교사·학급 기초 명렬표 등록 및 관리 (원격)
      </h3>
      <form onSubmit={handleSaveTeacher} style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))", gap: 8, marginBottom: 14 }}>
        <input className="input" placeholder="성명 (예: 박새별)" value={name} onChange={(e) => setName(e.target.value)} required />
        <input className="input" placeholder="아이디 (예: t21)" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <input className="input" placeholder="담당과목 (예: 수학)" value={subject} onChange={(e) => setSubject(e.target.value)} required />
        <input className="input" placeholder="직위 (예: 평교사)" value={position} onChange={(e) => setPosition(e.target.value)} />
        <input className="input" placeholder="담임학급 (예: 1-3)" value={homeroomClass} onChange={(e) => setHomeroomClass(e.target.value)} />
        <input className="input" type="date" value={transferDate} onChange={(e) => setTransferDate(e.target.value)} title="현임교 전입일" />
        <input className="input" placeholder="순회요일 (예: 1,3,5)" value={itinerantDaysText} onChange={(e) => setItinerantDaysText(e.target.value)} />
        <button type="submit" className="btn btn-primary">교사 등록/갱신</button>
      </form>

      <div style={{ maxHeight: 230, overflowY: "auto" }}>
        <table className="table">
          <thead>
            <tr><th>성명 (아이디)</th><th>과목</th><th>담임</th><th>전입일</th><th>순회요일</th><th>관리</th></tr>
          </thead>
          <tbody>
            {(teachers || []).filter((t) => !t.isParentHonorary).map((t) => (
              <tr key={t.id}>
                <td><b>{t.name}</b> ({t.username})</td>
                <td>{t.subject}</td>
                <td>{t.homeroomClass || "-"}</td>
                <td>{t.transferDate || "-"}</td>
                <td>{t.itinerantDays?.length ? t.itinerantDays.join(",") : "전일"}</td>
                <td>
                  {t.id !== user.id && (
                    <button className="btn btn-sm btn-danger" onClick={() => handleDeleteTeacher(t.id, t.name)}>
                      <Trash2 size={12} /> 삭제
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
