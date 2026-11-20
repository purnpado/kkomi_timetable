package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"kkomi-timetable/pkg/protocol"
	"kkomi-timetable/server/internal/ws"
)

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

func (a *ServerApp) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleMaster)
	if !ok {
		return
	}
	var body struct {
		FileName string `json:"fileName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preBackup, cnt, err := a.Store.RestoreEncryptedBackup(body.FileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.Bus.Publish(protocol.DomainEvent{
		Type:      protocol.EventSystemRestored,
		Title:     "암호화 백업 데이터 복원 완료",
		Message:   fmt.Sprintf("백업(%s)에서 %d건의 데이터가 복원되었습니다. 모든 세션이 재인증됩니다.", body.FileName, cnt),
		Urgent:    true,
		ActorID:   actor.ID,
		ActorName: actor.Name,
		Payload:   map[string]interface{}{"fileName": body.FileName, "restoredCount": cnt},
	})
	a.Auth.RevokeAllSessions()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "암호화 백업 복원이 완료되었습니다",
		"fileName":      body.FileName,
		"restoredCount": cnt,
		"preBackupFile": preBackup,
	})
}

func (a *ServerApp) handleSystemReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	actor, ok := a.requireRole(w, r, protocol.RoleMaster)
	if !ok {
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
	a.Bus.Publish(protocol.DomainEvent{
		Type:      protocol.EventSystemReset,
		Title:     "시스템 데이터 초기화 실행",
		Message:   fmt.Sprintf("초기화 모드(%s)가 완료되었습니다. 다시 로그인해 주세요.", body.Mode),
		Urgent:    true,
		ActorID:   actor.ID,
		ActorName: actor.Name,
		Payload:   map[string]string{"mode": body.Mode},
	})
	a.Auth.RevokeAllSessions()
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "시스템 초기화가 완료되었습니다",
		"mode":       body.Mode,
		"backupFile": backupPath,
	})
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
			ver = ws.ServerVersion
		}
		saved, err := ws.SaveClientPatch(ver, hdr.Filename, file, a.Bus)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"savedPath": saved, "version": ver})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, "지원하지 않는 메서드입니다")
}

func (a *ServerApp) handleClientPatchMeta(w http.ResponseWriter, r *http.Request) {
	meta, err := ws.LoadClientPatchMeta()
	if err != nil {
		writeError(w, http.StatusNotFound, "등록된 클라이언트 패치 메타데이터가 없습니다")
		return
	}
	writeJSON(w, http.StatusOK, meta)
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

func (a *ServerApp) handleGitHubRelease(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAuth(w, r); !ok {
		return
	}
	info, err := ws.CheckGitHubLatestRelease()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (a *ServerApp) handleGitHubUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST 요청만 허용됩니다")
		return
	}
	if _, ok := a.requireRole(w, r, protocol.RoleMaster); !ok {
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Target == "" {
		body.Target = "client"
	}
	info, err := ws.ApplyGitHubReleaseUpdate(body.Target, a.Store, a.Bus, a.OnShutdown)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  fmt.Sprintf("GitHub 최신 릴리즈(%s) 기반 %s 업데이트를 적용했습니다.", info.TagName, body.Target),
		"release": info,
	})
}
