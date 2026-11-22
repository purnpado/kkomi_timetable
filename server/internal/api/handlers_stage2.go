package api

import (
	"encoding/json"
	"net/http"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
)

func (a *ServerApp) handleExploreSwaps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	var req protocol.ExploreSwapsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.Timetable.ExploreSwapOptions(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *ServerApp) handleCreativeActivities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var body struct {
		Slots []protocol.CreativeActivitySlot `json:"slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	applied, err := a.Timetable.ApplyCreativeActivities(body.Slots, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"appliedCount": len(applied),
		"cells":        applied,
	})
}

func (a *ServerApp) handleTimetableWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	wf, err := a.Timetable.GetWorkflowState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (a *ServerApp) handleTimetableWorkflowPhase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var body struct {
		Phase string `json:"phase"`
		Note  string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wf, err := a.Timetable.SetWorkflowPhase(body.Phase, actor.Name, body.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (a *ServerApp) handleDeptSubmissions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		list, err := a.Timetable.ListDeptSubmissions()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	if r.Method == http.MethodPost {
		actor, ok := a.requireAuth(w, r)
		if !ok {
			return
		}
		var sub protocol.DeptLoadSubmission
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !auth.HasRole(actor, protocol.RoleTimetableAdmin) || sub.SubmitterID == "" {
			sub.SubmitterID = actor.ID
			sub.Submitter = actor.Name
		}
		saved, err := a.Timetable.SubmitDeptLoads(sub)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleReviewDeptSubmission(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var body struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		ReviewNote string `json:"reviewNote"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.Timetable.ReviewDeptSubmission(body.ID, body.Status, actor.Name, body.ReviewNote)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleDraftFeedbacks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		list, err := a.Timetable.ListDraftFeedbacks()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	if r.Method == http.MethodPost {
		actor, ok := a.requireAuth(w, r)
		if !ok {
			return
		}
		var fb protocol.DraftFeedback
		if err := json.NewDecoder(r.Body).Decode(&fb); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		fb.AuthorID = actor.ID
		fb.AuthorName = actor.Name
		saved, err := a.Timetable.SubmitDraftFeedback(fb)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleResolveDraftFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin)
	if !ok {
		return
	}
	var body struct {
		ID         string `json:"id"`
		Resolution string `json:"resolution"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.Timetable.ResolveDraftFeedback(body.ID, actor.Name, body.Resolution)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleSemesterLedger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	report, err := a.Timetable.CalculateSemesterLedger()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *ServerApp) handleSemesterLedgerConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		cfg, err := a.Timetable.GetSemesterLedgerConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
		return
	}
	if r.Method == http.MethodPost {
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		var cfg protocol.SemesterLedgerConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SaveSemesterLedgerConfig(cfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleSemesterLedgerDebt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
		return
	}
	var body struct {
		DebtID   string `json:"debtId"`
		Resolved bool   `json:"resolved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Timetable.SetMakeupDebtResolved(body.DebtID, body.Resolved); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"debtId": body.DebtID, "resolved": body.Resolved})
}
