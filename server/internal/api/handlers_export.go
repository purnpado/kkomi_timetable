package api

import (
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/exporter"
	"kkomi-timetable/server/internal/modules/auth"
)

func (a *ServerApp) handleExportHWPX(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	cfg, _ := a.Timetable.GetSchoolConfig()
	schoolName := cfg.SchoolName
	if schoolName == "" {
		schoolName = "꼬미중·고등학교"
	}
	kind := r.URL.Query().Get("kind")
	var data exporter.HWPXDocumentData
	fileName := "꼬미시간_보고서.hwpx"

	switch kind {
	case "timetable":
		reqs, _ := a.Timetable.ListRequests()
		var rows [][]string
		for i, rq := range reqs {
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), rq.TargetDate, rq.SourceCell.ClassName,
				fmt.Sprintf("%d교시 %s(%s)", rq.SourceCell.Period, rq.SourceCell.Subject, rq.RequesterName),
				fmt.Sprintf("%s (%s)", rq.Reason, exporter.StatusLabel(rq.Status)),
			})
		}
		data = exporter.HWPXDocumentData{
			SchoolName: schoolName, DocTitle: "일일 결보강 및 수업교환 대장",
			PeriodText: "교무실 공식 기록", Headers: []string{"순번", "적용일", "학급", "수업내역", "사유 및 상태"}, Rows: rows,
		}
		fileName = "결보강_수업교환_대장.hwpx"
	case "settlement":
		report, rErr := a.buildPeriodReportFromReq(r)
		if rErr != nil {
			writeError(w, http.StatusInternalServerError, rErr.Error())
			return
		}
		data = exporter.BuildSettlementHWPXData(report)
		fileName = "교무업무_통합정산_보고서.hwpx"
	case "substitution-cert":
		report, rErr := a.buildPeriodReportFromReq(r)
		if rErr != nil {
			writeError(w, http.StatusInternalServerError, rErr.Error())
			return
		}
		data = exporter.BuildSubstitutionCertHWPXData(report, r.URL.Query().Get("teacherId"))
		fileName = "결보강_수업교환_확인서.hwpx"
	case "internal-exam":
		slots, examCfg, sErr := a.InternalExam.ListSlotsForViewer(isExamAdmin)
		if sErr != nil {
			writeError(w, http.StatusInternalServerError, sErr.Error())
			return
		}
		data = exporter.BuildInternalExamHWPXData(schoolName, examCfg, slots)
		fileName = "교내시험감독_배정표.hwpx"
	default:
		events, _ := a.Calendar.ListEvents()
		var rows [][]string
		for i, ev := range events {
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), ev.EventDate, ev.Department, ev.Title, exporter.EventLinkTypeLabel(ev.LinkType),
			})
		}
		data = exporter.HWPXDocumentData{
			SchoolName: schoolName, DocTitle: "주중·월중 학사일정 및 행사표",
			PeriodText: "전체 확정 및 취합 일정", Headers: []string{"순번", "행사일", "부서", "행사명", "연동속성"}, Rows: rows,
		}
		fileName = "주중월중_학사일정표.hwpx"
	}

	raw, err := exporter.BuildHWPXBytes(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/hwp+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	_, _ = w.Write(raw)
}

func (a *ServerApp) handleExportXLSX(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	kind := r.URL.Query().Get("kind")
	var raw []byte
	var err error
	fileName := "꼬미시간_출력.xlsx"

	switch kind {
	case "teachers":
		raw, err = a.Timetable.ExportTeachersXLSX()
		writeCurriculumXLSX(w, "교사명렬표.xlsx", raw, err)
		return
	case "loads":
		raw, err = a.Timetable.ExportTeachingLoadsXLSX()
		writeCurriculumXLSX(w, "교사별시수표.xlsx", raw, err)
		return
	case "base-template":
		writeBaseTemplateXLSX(w)
		return
	case "external":
		noticeID := r.URL.Query().Get("noticeId")
		cands, notice, cErr := a.ExternalExam.CalculateCandidates(noticeID)
		if cErr != nil {
			writeError(w, http.StatusBadRequest, cErr.Error())
			return
		}
		raw, err = exporter.BuildExternalExamDispatchXLSX(*notice, cands)
		fileName = "외부시험감독_파견명단.xlsx"
	case "settlement":
		report, rErr := a.buildPeriodReportFromReq(r)
		if rErr != nil {
			writeError(w, http.StatusInternalServerError, rErr.Error())
			return
		}
		raw, err = exporter.BuildSettlementReportXLSX(report)
		fileName = "교무업무_통합정산표.xlsx"
	case "internal-exam":
		slots, examCfg, sErr := a.InternalExam.ListSlotsForViewer(isExamAdmin)
		if sErr != nil {
			writeError(w, http.StatusInternalServerError, sErr.Error())
			return
		}
		fairness, _ := a.InternalExam.CalculateFairnessSummary()
		raw, err = exporter.BuildInternalExamScheduleXLSX(examCfg, slots, fairness)
		fileName = "교내시험감독_배정_시수표.xlsx"
	case "calendar":
		events, _ := a.Calendar.ListEvents()
		raw, err = exporter.BuildEventsXLSX(events)
		fileName = "주중월중_학사일정.xlsx"
	default:
		reqs, _ := a.Timetable.ListRequests()
		raw, err = exporter.BuildTimetableChangesXLSX(reqs)
		fileName = "나이스_결보강대장.xlsx"
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	_, _ = w.Write(raw)
}

func (a *ServerApp) handleExportPrint(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	cfg, _ := a.Timetable.GetSchoolConfig()
	titlePrefix := cfg.SchoolName
	if titlePrefix == "" {
		titlePrefix = "꼬미중·고등학교"
	}
	var htmlDoc string
	switch r.URL.Query().Get("kind") {
	case "settlement":
		htmlDoc = a.buildSettlementPrintHTML(r)
	case "internal-exam":
		htmlDoc = a.buildExamPrintHTML(titlePrefix, isExamAdmin)
	default:
		events, _ := a.Calendar.ListEvents()
		var rows [][]string
		for i, ev := range events {
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), ev.EventDate, ev.Department, ev.Title,
				exporter.EventLinkTypeLabel(ev.LinkType), exporter.StatusLabel(ev.Status),
			})
		}
		htmlDoc = exporter.BuildHighContrastPrintHTML(
			fmt.Sprintf("[%s] 교무실 대형 현황판 (주중·월중 행사 & 학사일정)", titlePrefix),
			[]string{"순번", "일자", "부서", "행사명", "연동속성", "상태"}, rows)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlDoc))
}
