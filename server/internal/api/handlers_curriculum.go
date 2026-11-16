package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
)

func (a *ServerApp) handleSchoolConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := a.Timetable.GetSchoolConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPost:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		var cfg protocol.SchoolConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SaveSchoolConfig(cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
	}
}

func (a *ServerApp) handleTeachingLoads(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		loads, err := a.Timetable.ListTeachingLoads()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		summary, _ := a.Timetable.ValidateCurriculum()
		cfg, _ := a.Timetable.GetSchoolConfig()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"loads":   loads,
			"summary": summary,
			"config":  cfg,
		})
	case http.MethodPost:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		var ld protocol.TeachingLoad
		if err := json.NewDecoder(r.Body).Decode(&ld); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := a.Timetable.SaveTeachingLoad(ld)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if _, ok := a.requireRole(w, r, protocol.RoleTimetableAdmin); !ok {
			return
		}
		id := r.URL.Query().Get("id")
		if err := a.Timetable.DeleteTeachingLoad(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
	}
}

func (a *ServerApp) handleImportTeachingLoadsXLSX(w http.ResponseWriter, r *http.Request) {
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

	cnt, err := a.Timetable.ImportTeachingLoadsXLSX(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"importedCount": cnt})
}

func (a *ServerApp) handleImportTeachersXLSX(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster, protocol.RoleTimetableAdmin); !ok {
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

	cnt, err := a.Timetable.ImportTeachersXLSX(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"importedCount": cnt})
}

func writeCurriculumXLSX(w http.ResponseWriter, fileName string, raw []byte, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	_, _ = w.Write(raw)
}
