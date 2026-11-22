export const LINK_META = {
  SIMPLE_NOTICE: { label: "단순안내", badgeClass: "badge-gray" },
  CLASS_BLOCK: { label: "수업블록", badgeClass: "badge-blue" },
  TEACHER_TRIP: { label: "인솔출장", badgeClass: "badge-amber" },
  BELL_CHANGE: { label: "시종변경", badgeClass: "badge-amber" },
  HOLIDAY: { label: "공휴일/휴업일", badgeClass: "badge-red" },
};

export const HOLIDAY_KIND_LABELS = {
  PUBLIC_HOLIDAY: "법정 공휴일",
  DISCRETIONARY: "재량휴업일",
  FOUNDATION: "개교기념일",
  ETC: "기타 휴업일",
};

export const QUICK_LOCATIONS = ["강당", "시청각실", "운동장", "도서실", "다목적실"];

export const DEFAULT_BELL_PRESETS = [
  {
    key: "SHORT_40",
    name: "40분 단축수업",
    description: "1~7교시 40분 단축 운영(오전 1~4교시 11:50 종료, 5~7교시 12:40~15:00)",
    periods: [
      { period: 1, label: "1교시", startTime: "08:40", endTime: "09:20" },
      { period: 2, label: "2교시", startTime: "09:30", endTime: "10:10" },
      { period: 3, label: "3교시", startTime: "10:20", endTime: "11:00" },
      { period: 4, label: "4교시", startTime: "11:10", endTime: "11:50" },
      { period: 5, label: "5교시", startTime: "12:40", endTime: "13:20" },
      { period: 6, label: "6교시", startTime: "13:30", endTime: "14:10" },
      { period: 7, label: "7교시", startTime: "14:20", endTime: "15:00" },
    ],
  },
  {
    key: "SHORT_45",
    name: "45분 단축수업",
    description: "1~7교시 45분 단축 운영(오전 1~4교시 12:10 종료, 5~7교시 13:00~15:35)",
    periods: [
      { period: 1, label: "1교시", startTime: "08:40", endTime: "09:25" },
      { period: 2, label: "2교시", startTime: "09:35", endTime: "10:20" },
      { period: 3, label: "3교시", startTime: "10:30", endTime: "11:15" },
      { period: 4, label: "4교시", startTime: "11:25", endTime: "12:10" },
      { period: 5, label: "5교시", startTime: "13:00", endTime: "13:45" },
      { period: 6, label: "6교시", startTime: "13:55", endTime: "14:40" },
      { period: 7, label: "7교시", startTime: "14:50", endTime: "15:35" },
    ],
  },
  {
    key: "MORNING_EVENT",
    name: "오전 행사 후 5교시 수업",
    description: "오전 1~4교시 행사 진행 후 13:10부터 5~7교시 운영",
    periods: [
      { period: 1, label: "1교시(오전행사)", startTime: "08:40", endTime: "09:20" },
      { period: 2, label: "2교시(오전행사)", startTime: "09:30", endTime: "10:10" },
      { period: 3, label: "3교시(오전행사)", startTime: "10:20", endTime: "11:00" },
      { period: 4, label: "4교시(오전행사)", startTime: "11:10", endTime: "12:10" },
      { period: 5, label: "5교시", startTime: "13:10", endTime: "14:00" },
      { period: 6, label: "6교시", startTime: "14:10", endTime: "15:00" },
      { period: 7, label: "7교시", startTime: "15:10", endTime: "16:00" },
    ],
  },
];

export function formatLocalYMD(dateObj) {
  const y = dateObj.getFullYear();
  const m = String(dateObj.getMonth() + 1).padStart(2, "0");
  const d = String(dateObj.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

export function parseLocalYMD(dateStr) {
  const parts = String(dateStr || "").split("-").map(Number);
  if (parts.length !== 3 || !parts[0] || !parts[1] || !parts[2]) return new Date();
  return new Date(parts[0], parts[1] - 1, parts[2]);
}

export function eventEndDate(ev) {
  const start = String(ev?.eventDate || "").trim();
  const end = String(ev?.endDate || "").trim();
  if (!end || end < start) return start;
  return end;
}

export function eventCoversDate(ev, dateStr) {
  const start = String(ev?.eventDate || "").trim();
  if (!start || !dateStr) return false;
  return dateStr >= start && dateStr <= eventEndDate(ev);
}

export function formatEventDateRange(ev) {
  const start = String(ev?.eventDate || "").trim();
  const end = eventEndDate(ev);
  if (!end || end === start) return start;
  return `${start} ~ ${end}`;
}

export function formatEventPeriods(ev) {
  const sp = Number(ev?.startPeriod || 0);
  const ep = Number(ev?.endPeriod || 0);
  if (sp === 0) return "종일";
  if (ep > sp) return `${sp}~${ep}교시`;
  return `${sp}교시`;
}

export function formatEventTargets(ev) {
  const grades = Array.isArray(ev?.targetGrades) ? ev.targetGrades.filter((g) => g > 0) : [];
  const classes = Array.isArray(ev?.targetClasses) ? ev.targetClasses.filter(Boolean) : [];
  const parts = [];
  if (grades.length >= 3 && grades.includes(1) && grades.includes(2) && grades.includes(3)) {
    parts.push("전교(1·2·3학년)");
  } else if (grades.length > 0) {
    parts.push(`${grades.slice().sort((a, b) => a - b).join("·")}학년`);
  }
  if (classes.length > 0) parts.push(classes.join(", "));
  if (parts.length === 0) return ev?.linkType === "HOLIDAY" ? "전교" : "전체/미지정";
  return parts.join(" · ");
}

export function formatTeacherNames(ev, teachers) {
  const ids = Array.isArray(ev?.leadTeachers) ? ev.leadTeachers.filter(Boolean) : [];
  if (ids.length === 0) return "-";
  const tMap = new Map((teachers || []).map((t) => [t.id, t.name]));
  return ids.map((id) => tMap.get(id) || id).join(", ");
}

export function buildMonthMatrix(year, month) {
  const firstDay = new Date(year, month - 1, 1);
  const startOffset = firstDay.getDay();
  const cursor = new Date(year, month - 1, 1 - startOffset);
  const weeks = [];
  for (let w = 0; w < 6; w += 1) {
    const row = [];
    for (let d = 0; d < 7; d += 1) {
      row.push({
        dateStr: formatLocalYMD(cursor),
        day: cursor.getDate(),
        inMonth: cursor.getMonth() === month - 1,
        weekday: cursor.getDay(),
      });
      cursor.setDate(cursor.getDate() + 1);
    }
    weeks.push(row);
  }
  return weeks;
}

export function buildWeekDates(baseDateStr) {
  const dt = parseLocalYMD(baseDateStr);
  const wd = dt.getDay();
  const diffToMon = wd === 0 ? -6 : 1 - wd;
  const mon = new Date(dt.getFullYear(), dt.getMonth(), dt.getDate() + diffToMon);
  const names = ["월", "화", "수", "목", "금"];
  const list = [];
  for (let i = 0; i < 5; i += 1) {
    const cur = new Date(mon.getFullYear(), mon.getMonth(), mon.getDate() + i);
    const ymd = formatLocalYMD(cur);
    list.push({
      dateStr: ymd,
      weekday: i + 1,
      weekdayLabel: names[i],
      shortDate: ymd.slice(5),
    });
  }
  return list;
}
