package api

import (
	"encoding/json"
	"net/http"
	"time"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/ws"
)

func (a *ServerApp) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"version":        ws.ServerVersion,
		"serverIp":       ws.LocalIPAddress(),
		"databaseMode":   a.Store.ModeLabel(),
		"connectedUsers": a.Hub.ConnectedCount(),
		"time":           time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (a *ServerApp) handleAuthDirectory(w http.ResponseWriter, r *http.Request) {
	dir, err := a.Auth.ListPublicDirectory()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dir)
}

func (a *ServerApp) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	var body struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Subject  string `json:"subject"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "잘못된 로그인 요청 형식입니다")
		return
	}
	t, err := a.Auth.AuthenticateLogin(body.Username, body.Name, body.Subject, body.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (a *ServerApp) handleTeachers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		teachers, err := a.Auth.ListTeachers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range teachers {
			teachers[i].PasswordHash = ""
		}
		writeJSON(w, http.StatusOK, teachers)
		return
	case http.MethodPost:
		if _, ok := a.requireRole(w, r, protocol.RoleMaster, protocol.RoleTimetableAdmin); !ok {
			return
		}
		var body struct {
			Teacher  protocol.Teacher `json:"teacher"`
			Password string           `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Auth.SaveTeacher(body.Teacher, body.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	case http.MethodDelete:
		if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
			return
		}
		tid := r.URL.Query().Get("id")
		if err := a.Auth.DeleteTeacher(tid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": tid})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleUpdateRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	var body struct {
		TeacherID string          `json:"teacherId"`
		Roles     []protocol.Role `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.Auth.UpdateRoles(body.TeacherID, body.Roles)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleRoleHandover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	var body struct {
		FromTeacherID string        `json:"fromTeacherId"`
		ToTeacherID   string        `json:"toTeacherId"`
		Role          protocol.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Auth.TransferRoleHandover(body.FromTeacherID, body.ToTeacherID, body.Role); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "업무분장 인계 완료"})
}

func (a *ServerApp) handleDashboard(w http.ResponseWriter, r *http.Request) {
	actor := a.currentActor(r)
	teacherID := ""
	homeroom := ""
	isExamAdmin := false
	if actor != nil {
		teacherID = actor.ID
		homeroom = actor.HomeroomClass
		isExamAdmin = auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	wd := int(now.Weekday())
	if wd == 0 || wd == 6 {
		wd = 1 // 주말 시연 시 월요일 시간표 미리보기 제공
	}

	cells, _ := a.Timetable.ListCells()
	var myLessons, homeroomLessons []protocol.TimetableCell
	for _, c := range cells {
		if c.DayOfWeek == wd && (c.TeacherID == teacherID || c.CoTeacherID == teacherID) {
			myLessons = append(myLessons, c)
		}
		if c.DayOfWeek == wd && homeroom != "" && c.ClassName == homeroom {
			homeroomLessons = append(homeroomLessons, c)
		}
	}

	events, _ := a.Calendar.ListEvents()
	var todayEvents, weekEvents, pendingEvents []protocol.SchoolEvent
	for _, ev := range events {
		if ev.Status == protocol.StatusStaging {
			pendingEvents = append(pendingEvents, ev)
			continue
		}
		if ev.Status == protocol.StatusApproved {
			if ev.EventDate == today {
				todayEvents = append(todayEvents, ev)
			}
			weekEvents = append(weekEvents, ev)
		}
	}

	slots, _, _ := a.InternalExam.ListSlotsForViewer(isExamAdmin)
	var myExamSlots []protocol.ExamSlot
	for _, sl := range slots {
		if sl.TeacherID == teacherID || teacherID == "" {
			myExamSlots = append(myExamSlots, sl)
		}
	}

	reqs, _ := a.Timetable.ListRequests()
	var pendingApprovals []protocol.TimetableChangeRequest
	for _, rq := range reqs {
		if rq.Status == protocol.StatusStaging {
			pendingApprovals = append(pendingApprovals, rq)
		}
	}

	extNotices, _ := a.ExternalExam.ListNotices()
	var openExternal []protocol.ExternalExamNotice
	for _, ex := range extNotices {
		if !ex.IsConfirmed {
			openExternal = append(openExternal, ex)
		}
	}
	bells, _ := a.Calendar.ListBellSchedule()

	summary := protocol.DashboardSummary{
		ServerVersion:     ws.ServerVersion,
		ServerIP:          ws.LocalIPAddress(),
		DatabaseMode:      a.Store.ModeLabel(),
		TodayDate:         today,
		TodayWeekday:      wd,
		BellSchedule:      bells,
		MyTodayLessons:    myLessons,
		HomeroomLessons:   homeroomLessons,
		TodayEvents:       todayEvents,
		WeekEvents:        weekEvents,
		MyExamSlots:       myExamSlots,
		PendingApprovals:  pendingApprovals,
		PendingEvents:     pendingEvents,
		OpenExternalExams: openExternal,
		AdminPendingCounts: map[string]int{
			"timetable": len(pendingApprovals),
			"events":    len(pendingEvents),
			"external":  len(openExternal),
		},
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *ServerApp) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	logs, err := a.Store.ListAuditLogs(30)
	if err != nil {
		writeJSON(w, http.StatusOK, a.Bus.RecentEvents(30))
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (a *ServerApp) handleEvaluateMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	var req protocol.EvaluateMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Timetable.EvaluateMove(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *ServerApp) handleTimetableRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		reqs, err := a.Timetable.ListRequests()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, reqs)
		return
	}
	if r.Method == http.MethodPost {
		var req protocol.TimetableChangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SubmitRequest(req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleCommitTimetableRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var body struct {
		RequestID    string `json:"requestId"`
		Approve      bool   `json:"approve"`
		RejectReason string `json:"rejectReason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.Timetable.CommitRequest(body.RequestID, actor.Name, body.Approve, body.RejectReason)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleNEISHelper(w http.ResponseWriter, r *http.Request) {
	list, err := a.Timetable.NEISHelperList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}
