package api

import (
	"encoding/json"
	"net/http"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
	"kkomi-timetable/server/internal/modules/internal_exam"
)

func (a *ServerApp) handleInternalExamSlots(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	slots, cfg, err := a.InternalExam.ListSlotsForViewer(isExamAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fairness, _ := a.InternalExam.CalculateFairnessSummary()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config":      cfg,
		"slots":       slots,
		"specs":       a.InternalExam.ListSpecs(),
		"fairness":    fairness,
		"roomCatalog": a.InternalExam.BuildRoomCatalog(),
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
	if _, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin); !ok {
		return
	}
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
	actor, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin);
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
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
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
	actor, ok := a.requireAuth(w, r)
	if !ok {
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
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	noticeID := r.URL.Query().Get("noticeId")
	cands, notice, err := a.ExternalExam.CalculateCandidates(noticeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	isExtAdmin := auth.HasRole(actor, protocol.RoleExternalExamAdmin)
	for i := range cands {
		cands[i].Teacher.PasswordHash = ""
		if !isExtAdmin && cands[i].Teacher.ID != actor.ID {
			cands[i].Teacher.Phone = ""
			if cands[i].IsExempt || len(cands[i].Teacher.ExemptReasons) > 0 {
				cands[i].Teacher.ExemptReasons = []string{"면제 대상"}
				if cands[i].IsExempt {
					cands[i].ReasonBadge = "[면제 대상]"
				}
			}
		}
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
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		list, err := a.Calendar.ListEvents()
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
		var ev protocol.SchoolEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ev.ProposedBy = actor.Name
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
