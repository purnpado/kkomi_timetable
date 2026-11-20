package api

import (
	"encoding/json"
	"net/http"

	"kkomi-timetable/pkg/protocol"
)

func (a *ServerApp) handleTimetableConstraints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		list, err := a.Timetable.ListConstraints()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
		if !ok {
			return
		}
		var req protocol.TeacherConstraint
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SaveConstraint(req, actor.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
		if !ok {
			return
		}
		id := r.URL.Query().Get("id")
		if err := a.Timetable.DeleteConstraint(id, actor.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
	}
}

func (a *ServerApp) handleTimetableConstraintsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var req protocol.BatchConstraintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.Timetable.BatchSaveConstraints(req, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"saved": saved,
		"count": len(saved),
	})
}

func (a *ServerApp) handleTimetableScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		list, err := a.Timetable.ListScenarios()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cur, _ := a.Timetable.CurrentMetrics()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"scenarios":      list,
			"currentMetrics": cur,
		})
	case http.MethodPost:
		actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
		if !ok {
			return
		}
		var req struct {
			SlotCode    string `json:"slotCode"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sc, err := a.Timetable.SaveScenario(req.SlotCode, req.Name, req.Description, actor.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sc)
	case http.MethodDelete:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		id := r.URL.Query().Get("id")
		if err := a.Timetable.DeleteScenario(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
	}
}

func (a *ServerApp) handleApplyTimetableScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var req struct {
		ScenarioID string `json:"scenarioId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sc, err := a.Timetable.ApplyScenario(req.ScenarioID, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hist, _ := a.Timetable.GetHistoryStatus()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scenario": sc,
		"history":  hist,
	})
}

func (a *ServerApp) handleTimetableHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	st, err := a.Timetable.GetHistoryStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *ServerApp) handleTimetableUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	st, cells, err := a.Timetable.UndoTimetable(actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"history": st,
		"cells":   cells,
	})
}

func (a *ServerApp) handleTimetableRedo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	st, cells, err := a.Timetable.RedoTimetable(actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"history": st,
		"cells":   cells,
	})
}
