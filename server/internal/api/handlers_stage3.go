package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/auth"
)

func (a *ServerApp) handleSwapInternalExamSlots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin)
	if !ok {
		return
	}
	var body struct {
		SlotID1 string `json:"slotId1"`
		SlotID2 string `json:"slotId2"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.InternalExam.SwapSlots(body.SlotID1, body.SlotID2, actor.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handlePersonalInternalExam(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireAuth(w, r)
	if !ok {
		return
	}
	isExamAdmin := auth.HasRole(actor, protocol.RoleInternalExamAdmin)
	targetTeacherID := strings.TrimSpace(r.URL.Query().Get("teacherId"))
	if targetTeacherID == "" || !isExamAdmin {
		targetTeacherID = actor.ID
	}
	res, err := a.InternalExam.PersonalSchedule(targetTeacherID, isExamAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *ServerApp) handleExternalExamConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, ok := a.requireAuth(w, r); !ok {
			return
		}
		cfg, err := a.ExternalExam.GetSchoolConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
		return
	}
	if r.Method == http.MethodPost {
		if _, ok := a.requireRole(w, r, protocol.RoleExternalExamAdmin); !ok {
			return
		}
		var cfg protocol.ExternalExamSchoolConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.ExternalExam.SaveSchoolConfig(cfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}
