package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/internal_exam"
	"kkomi-timetable/server/internal/modules/timetable"
)

func (a *ServerApp) handleTimetableCells(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		weekStart := r.URL.Query().Get("weekStart")
		cells, err := a.Timetable.ListWeekCells(weekStart)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cells)
	case http.MethodPost:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		var cell protocol.TimetableCell
		if err := json.NewDecoder(r.Body).Decode(&cell); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SaveCell(cell)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		cellID := r.URL.Query().Get("id")
		if err := a.Timetable.DeleteCell(cellID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": cellID})
	default:
		writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
	}
}

func (a *ServerApp) handleAutoGenerateBaseTimetable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
		return
	}
	var body struct {
		Grades          []int `json:"grades"`
		ClassesPerGrade int   `json:"classesPerGrade"`
		ClearExisting   bool  `json:"clearExisting"`
		UseCurriculum   bool  `json:"useCurriculum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var (
		created []protocol.TimetableCell
		err     error
	)
	if body.UseCurriculum {
		created, err = a.Timetable.AutoGenerateFromCurriculum(body.ClearExisting)
	} else {
		created, err = a.Timetable.AutoGenerateBaseTimetable(body.Grades, body.ClassesPerGrade, body.ClearExisting)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"createdCount": len(created),
		"cells":        created,
	})
}

func (a *ServerApp) handleImportTimetableXLSX(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "엑셀 파일(file)이 필요합니다")
		return
	}
	defer file.Close()

	cnt, err := a.Timetable.ImportTimetableXLSX(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"importedCount": cnt})
}

func (a *ServerApp) handleBellSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		bells, err := a.Calendar.ListBellSchedule()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, bells)
		return
	}
	if r.Method == http.MethodPost {
		actor, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin, protocol.RoleEventAdmin)
		if !ok {
			return
		}
		var bells []protocol.BellPeriod
		if err := json.NewDecoder(r.Body).Decode(&bells); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Calendar.SaveBellSchedule(bells, actor.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor := a.currentActor(r)
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "로그인이 필요합니다")
		return
	}
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.Auth.ChangePassword(actor.ID, body.CurrentPassword, body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *ServerApp) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	var body struct {
		TeacherID string `json:"teacherId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Auth.ResetTeacherPassword(body.TeacherID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "비밀번호가 초기값(1234)으로 재설정되었습니다"})
}

func (a *ServerApp) handleSystemReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backupPath, err := a.Store.ResetSystem(body.Mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "시스템 초기화가 완료되었습니다",
		"mode":       body.Mode,
		"backupFile": backupPath,
	})
}

func writeBaseTemplateXLSX(w http.ResponseWriter) {
	raw, err := timetable.BuildBaseTemplateXLSX()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape("기초시간표_업로드양식.xlsx")))
	_, _ = w.Write(raw)
}

func (a *ServerApp) handleInternalExamTeacherPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleInternalExamAdmin); !ok {
		return
	}
	var req internal_exam.TeacherExamPolicyUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.InternalExam.UpdateTeacherExamPolicy(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fairness, _ := a.InternalExam.CalculateFairnessSummary()
	writeJSON(w, http.StatusOK, fairness)
}
