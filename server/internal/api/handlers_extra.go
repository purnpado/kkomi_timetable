package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/exporter"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/ws"
)

func (a *ServerApp) handleInternalExamSlots(w http.ResponseWriter, r *http.Request) {
	actor := a.currentActor(r)
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	slots, cfg, err := a.InternalExam.ListSlotsForViewer(isExamAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config": cfg,
		"slots":  slots,
		"specs":  a.InternalExam.ListSpecs(),
	})
}

func (a *ServerApp) handleGenerateInternalExam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin); !ok {
		return
	}
	var body struct {
		Config protocol.ExamConfig            `json:"config"`
		Specs  []internal_exam.ExamPeriodSpec `json:"specs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slots, err := a.InternalExam.GenerateAndSave(body.Config, body.Specs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, slots)
}

func (a *ServerApp) handleUnsealInternalExam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin)
	if !ok {
		return
	}
	cfg, err := a.InternalExam.UnsealExam("", actor.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (a *ServerApp) handleRecommendSubstitute(w http.ResponseWriter, r *http.Request) {
	slotID := r.URL.Query().Get("slotId")
	recs, err := a.InternalExam.RecommendSubstitutes(slotID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for i := range recs {
		recs[i].PasswordHash = ""
	}
	writeJSON(w, http.StatusOK, recs)
}

func (a *ServerApp) handleApplySubstitute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin)
	if !ok {
		return
	}
	var body struct {
		SlotID       string `json:"slotId"`
		NewTeacherID string `json:"newTeacherId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.InternalExam.ApplySubstitute(body.SlotID, body.NewTeacherID, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleExternalExamNotices(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		notices, err := a.ExternalExam.ListNotices()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, notices)
		return
	}
	if r.Method == http.MethodPost {
		if _, ok := a.requireRole(w, r, protocol.RoleExternalExamAdmin); !ok {
			return
		}
		var notice protocol.ExternalExamNotice
		if err := json.NewDecoder(r.Body).Decode(&notice); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.ExternalExam.CreateNotice(notice)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleExternalExamVolunteer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor := a.currentActor(r)
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "교사 인증 정보가 필요합니다")
		return
	}
	var body struct {
		NoticeID string `json:"noticeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.ExternalExam.ToggleVolunteer(body.NoticeID, actor.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleExternalExamRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleExternalExamAdmin); !ok {
		return
	}
	var body struct {
		NoticeID      string                          `json:"noticeId"`
		PresetMode    string                          `json:"presetMode"`
		PriorityOrder []protocol.ExternalExamCriteria `json:"priorityOrder"`
		ExcludeExempt bool                            `json:"excludeExempt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.ExternalExam.UpdateRules(body.NoticeID, body.PresetMode, body.PriorityOrder, body.ExcludeExempt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleExternalExamCandidates(w http.ResponseWriter, r *http.Request) {
	noticeID := r.URL.Query().Get("noticeId")
	cands, notice, err := a.ExternalExam.CalculateCandidates(noticeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for i := range cands {
		cands[i].Teacher.PasswordHash = ""
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"notice":     notice,
		"candidates": cands,
	})
}

func (a *ServerApp) handleExternalExamConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleExternalExamAdmin)
	if !ok {
		return
	}
	var body struct {
		NoticeID    string   `json:"noticeId"`
		SelectedIDs []string `json:"selectedIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	confirmed, err := a.ExternalExam.ConfirmDispatch(body.NoticeID, body.SelectedIDs, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, confirmed)
}

func (a *ServerApp) handleCalendarEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list, err := a.Calendar.ListEvents()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	if r.Method == http.MethodPost {
		var ev protocol.SchoolEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Calendar.ProposeEvent(ev)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleCommitCalendarEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleEventAdmin)
	if !ok {
		return
	}
	var body struct {
		EventID string `json:"eventId"`
		Approve bool   `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	committed, err := a.Calendar.CommitEvent(body.EventID, actor.Name, body.Approve)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, committed)
}

func (a *ServerApp) handleExportHWPX(w http.ResponseWriter, r *http.Request) {
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
				fmt.Sprintf("%s (%s)", rq.Reason, rq.Status),
			})
		}
		data = exporter.HWPXDocumentData{
			SchoolName: "꼬미중·고등학교", DocTitle: "일일 결보강 및 수업교환 대장",
			PeriodText: "교무실 공식 기록", Headers: []string{"순번", "적용일", "학급", "수업내역", "사유 및 상태"}, Rows: rows,
		}
		fileName = "결보강_수업교환_대장.hwpx"
	default:
		events, _ := a.Calendar.ListEvents()
		var rows [][]string
		for i, ev := range events {
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1), ev.EventDate, ev.Department, ev.Title, string(ev.LinkType),
			})
		}
		data = exporter.HWPXDocumentData{
			SchoolName: "꼬미중·고등학교", DocTitle: "주중·월중 학사일정 및 행사표",
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
	events, _ := a.Calendar.ListEvents()
	var rows [][]string
	for i, ev := range events {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1), ev.EventDate, ev.Department, ev.Title, string(ev.LinkType), string(ev.Status),
		})
	}
	htmlDoc := exporter.BuildHighContrastPrintHTML("교무실 대형 현황판 (주중·월중 행사 & 학사일정)",
		[]string{"순번", "일자", "부서", "행사명", "연동속성", "상태"}, rows)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlDoc))
}

func (a *ServerApp) handleSystemBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	if r.Method == http.MethodGet {
		list, err := a.Store.ListBackups()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	if r.Method == http.MethodPost {
		path, err := a.Store.ExportEncryptedBackup("manual")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"backupFile": path})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleClientPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		patchPath, origName := ws.LatestClientPatchPath()
		if patchPath == "" {
			writeError(w, http.StatusNotFound, "등록된 클라이언트 패치 파일이 없습니다")
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(origName)))
		http.ServeFile(w, r, patchPath)
		return
	}
	if r.Method == http.MethodPost {
		if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
			return
		}
		if err := r.ParseMultipartForm(128 << 20); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "업로드 파일(file)이 필요합니다")
			return
		}
		defer file.Close()
		ver := r.FormValue("version")
		if ver == "" {
			ver = "0.1.1"
		}
		saved, err := ws.SaveClientPatch(ver, hdr.Filename, file, a.Bus)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"savedPath": saved, "version": ver})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleServerSelfUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "새 서버 바이너리(file)가 필요합니다")
		return
	}
	defer file.Close()
	if err := ws.SelfReplaceServerAndRestart(file, a.Store, a.OnShutdown); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "DB 암호화 백업 완료. 2초 내 서버 무중단 재시작을 진행합니다."})
}
