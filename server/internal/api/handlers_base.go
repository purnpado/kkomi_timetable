package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/modules/timetable"
)

func (a *ServerApp) handleTimetableCells(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cells, err := a.Timetable.ListCells()
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := a.Timetable.AutoGenerateBaseTimetable(body.Grades, body.ClassesPerGrade, body.ClearExisting)
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
