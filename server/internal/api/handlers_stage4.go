package api

import (
	"encoding/json"
	"net/http"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/event_calendar"
)

func (a *ServerApp) handleCalendarUpdateEvent(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	var ev protocol.SchoolEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	isEventAdmin := auth.HasRole(actor, protocol.RoleEventAdmin)
	updated, err := a.Calendar.UpdateEvent(ev, actor.Name, isEventAdmin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleCalendarConflicts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	var ev protocol.SchoolEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	conflicts, err := a.Calendar.CheckConflicts(ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if conflicts == nil {
		conflicts = []protocol.EventConflict{}
	}
	writeJSON(w, http.StatusOK, conflicts)
}

func (a *ServerApp) handleCalendarBellPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, event_calendar.DefaultBellPresets())
}
