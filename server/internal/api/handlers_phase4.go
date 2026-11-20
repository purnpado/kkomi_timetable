package api

import (
	"fmt"
	"net/http"
	"strconv"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/exporter"
)

func parsePeriodQuery(r *http.Request) (mode string, year, month int, startDate, endDate string) {
	q := r.URL.Query()
	mode = q.Get("mode")
	year, _ = strconv.Atoi(q.Get("year"))
	month, _ = strconv.Atoi(q.Get("month"))
	startDate = q.Get("startDate")
	endDate = q.Get("endDate")
	return mode, year, month, startDate, endDate
}

func (a *ServerApp) buildPeriodReportFromReq(r *http.Request) (protocol.PeriodStatsReport, error) {
	mode, year, month, startDate, endDate := parsePeriodQuery(r)
	return a.Stats.BuildPeriodReport(mode, year, month, startDate, endDate)
}

func (a *ServerApp) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	report, err := a.buildPeriodReportFromReq(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *ServerApp) buildSettlementPrintHTML(r *http.Request) string {
	report, _ := a.buildPeriodReportFromReq(r)
	var rows [][]string
	for i, st := range report.TeacherStats {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%s (%s)", st.TeacherName, st.Subject),
			fmt.Sprintf("교환 %d / 보강 %d (요청 %d)", st.SwapCount, st.SubstituteGivenCount, st.SubstituteTakenCount),
			fmt.Sprintf("%d (정%d·부%d·복%d)", st.InternalTotal, st.InternalMainCount, st.InternalSubCount, st.InternalCorrCount),
			fmt.Sprintf("외부 %d / 행사 %d", st.ExternalExamCount, st.EventLeadCount),
			fmt.Sprintf("%d회", st.TotalActivity),
		})
	}
	return exporter.BuildHighContrastPrintHTML(
		fmt.Sprintf("[%s] 교무업무 통합 정산표 (%s)", report.SchoolName, report.PeriodLabel),
		[]string{"순번", "성명(교과)", "수업교환/보강", "교내감독", "외부/행사", "총실적"},
		rows,
	)
}

func (a *ServerApp) buildExamPrintHTML(schoolName string, isExamAdmin bool) string {
	slots, cfg, _ := a.InternalExam.ListSlotsForViewer(isExamAdmin)
	roleName := func(r string) string {
		switch r {
		case "MAIN":
			return "정감독"
		case "SUB":
			return "부감독"
		case "CORRIDOR":
			return "복도감독"
		case "HONORARY":
			return "학부모명예"
		default:
			return r
		}
	}
	var rows [][]string
	for i, sl := range slots {
		tInfo := fmt.Sprintf("%s %d교시", sl.ExamDate, sl.Period)
		if sl.StartTime != "" {
			tInfo = fmt.Sprintf("%s %d교시 (%s~%s)", sl.ExamDate, sl.Period, sl.StartTime, sl.EndTime)
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			tInfo,
			fmt.Sprintf("%d학년 %s", sl.Grade, sl.Subject),
			roleName(sl.SlotRole),
			sl.RoomName,
			sl.TeacherName,
		})
	}
	return exporter.BuildHighContrastPrintHTML(
		fmt.Sprintf("[%s] %s 감독 배정표", schoolName, cfg.Title),
		[]string{"순번", "시험일시", "학년·과목", "역할", "고사장/복도", "감독교사"},
		rows,
	)
}
