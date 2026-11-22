package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const MaxLogBytes = 1 << 20

func readStageLog(runDir, logPath string, tailLines int) ([]byte, bool, error) {
	if strings.TrimSpace(logPath) == "" {
		return nil, false, os.ErrNotExist
	}
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return nil, false, err
	}
	absRun = filepath.Clean(absRun)
	candidate := logPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRun, candidate)
	}
	absFile, err := filepath.Abs(candidate)
	if err != nil {
		return nil, false, err
	}
	absFile = filepath.Clean(absFile)
	rel, err := filepath.Rel(absRun, absFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, false, fmt.Errorf("log path escapes run dir")
	}
	realRun, err := filepath.EvalSymlinks(absRun)
	if err != nil {
		return nil, false, err
	}
	realFile, err := filepath.EvalSymlinks(absFile)
	if err != nil {
		return nil, false, err
	}
	rel, err = filepath.Rel(realRun, realFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, false, fmt.Errorf("log path escapes run dir")
	}
	data, err := os.ReadFile(realFile)
	if err != nil {
		return nil, false, err
	}
	truncated := false
	if len(data) > MaxLogBytes {
		data = data[len(data)-MaxLogBytes:]
		truncated = true
	}
	if tailLines > 0 {
		data = tailBytesByLines(data, tailLines)
	}
	return data, truncated, nil
}

func tailBytesByLines(data []byte, n int) []byte {
	if n <= 0 {
		return data
	}
	s := string(data)
	parts := strings.Split(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= n {
		return data
	}
	return []byte(strings.Join(parts[len(parts)-n:], "\n") + "\n")
}

func (s *Server) getStageLog(w http.ResponseWriter, r *http.Request) {
	runID, err := s.engine.Store.ResolveRunID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	stageID := chi.URLParam(r, "stage_id")
	run, err := s.engine.Store.GetRun(runID)
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	stages, err := s.engine.Store.GetStageRuns(runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var logPath string
	found := false
	for _, st := range stages {
		if st.StageID == stageID {
			found = true
			if st.LogPath != nil {
				logPath = *st.LogPath
			}
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Errorf("stage %s not found", stageID))
		return
	}
	tail := 0
	if raw := r.URL.Query().Get("tail"); raw != "" {
		tail, err = strconv.Atoi(raw)
		if err != nil || tail < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid tail"))
			return
		}
	}
	body, truncated, err := readStageLog(run.RunDir, logPath, tail)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("log not found"))
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if truncated {
		w.Header().Set("X-Shuttle-Log-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
