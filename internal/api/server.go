package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

type Options struct {
	CWD         string
	PipelineIDs []string
}

type Server struct {
	engine      *pipeline.Engine
	cwd         string
	pipelineIDs []string
}

func NewHandler(engine *pipeline.Engine, opts Options) http.Handler {
	s := &Server{
		engine:      engine,
		cwd:         opts.CWD,
		pipelineIDs: append([]string(nil), opts.PipelineIDs...),
	}

	r := chi.NewRouter()
	r.Post("/v1/runs", s.createRun)
	r.Get("/v1/runs/{id}", s.getRun)
	r.Post("/v1/runs/{id}/cancel", s.cancelRun)
	r.Post("/v1/runs/{id}/approve", s.approveRun)
	r.Post("/v1/runs/{id}/reject", s.rejectRun)
	r.Get("/v1/pipelines", s.listPipelines)
	return r
}

type createRunRequest struct {
	TemplateID string `json:"template_id"`
	SpecPath   string `json:"spec_path,omitempty"`
	CWD        string `json:"cwd,omitempty"`
}

type decisionRequest struct {
	StageID string `json:"stage_id"`
	Message string `json:"message,omitempty"`
}

type runResponse struct {
	ID         string          `json:"id"`
	TemplateID string          `json:"template_id"`
	Status     string          `json:"status"`
	CWD        string          `json:"cwd"`
	RunDir     string          `json:"run_dir"`
	Stages     []stageResponse `json:"stages"`
}

type stageResponse struct {
	ID      string  `json:"id"`
	Worker  string  `json:"worker"`
	Status  string  `json:"status"`
	Attempt int     `json:"attempt"`
	Retries int     `json:"retries"`
	Summary *string `json:"summary,omitempty"`
	LogPath *string `json:"log_path,omitempty"`
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if req.TemplateID == "" {
		writeError(w, http.StatusBadRequest, errors.New("template_id is required"))
		return
	}
	if s.engine.TemplateLoader == nil {
		writeError(w, http.StatusInternalServerError, errors.New("template loader is not configured"))
		return
	}

	pl, err := s.engine.TemplateLoader(req.TemplateID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cwd := req.CWD
	if cwd == "" {
		cwd = s.cwd
	}
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	runID, err := s.engine.StartRun(r.Context(), pipeline.StartParams{
		TemplateID: req.TemplateID,
		Pipeline:   pl,
		CWD:        cwd,
		SpecPath:   req.SpecPath,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.engine.RunUntilBlockOrDone(r.Context(), runID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeRun(w, http.StatusCreated, runID)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	s.writeRun(w, http.StatusOK, chi.URLParam(r, "id"))
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "id")
	if err := s.engine.Cancel(runID); err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	s.writeRun(w, http.StatusOK, runID)
}

func (s *Server) approveRun(w http.ResponseWriter, r *http.Request) {
	s.decideRun(w, r, true)
}

func (s *Server) rejectRun(w http.ResponseWriter, r *http.Request) {
	s.decideRun(w, r, false)
}

func (s *Server) decideRun(w http.ResponseWriter, r *http.Request, approve bool) {
	var req decisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if req.StageID == "" {
		writeError(w, http.StatusBadRequest, errors.New("stage_id is required"))
		return
	}

	runID := chi.URLParam(r, "id")
	var err error
	if approve {
		if req.Message == "" {
			req.Message = "approved"
		}
		err = s.engine.Approve(runID, req.StageID, req.Message)
	} else {
		if req.Message == "" {
			req.Message = "rejected"
		}
		err = s.engine.Reject(runID, req.StageID, req.Message)
	}
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	if approve {
		if err := s.engine.RunUntilBlockOrDone(r.Context(), runID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.writeRun(w, http.StatusOK, runID)
}

func (s *Server) listPipelines(w http.ResponseWriter, r *http.Request) {
	type pipelineResponse struct {
		ID string `json:"id"`
	}
	resp := struct {
		Pipelines []pipelineResponse `json:"pipelines"`
	}{Pipelines: make([]pipelineResponse, 0, len(s.pipelineIDs))}
	for _, id := range s.pipelineIDs {
		resp.Pipelines = append(resp.Pipelines, pipelineResponse{ID: id})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) writeRun(w http.ResponseWriter, status int, runID string) {
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

	resp := runResponse{
		ID:         run.ID,
		TemplateID: run.TemplateID,
		Status:     string(run.Status),
		CWD:        run.CWD,
		RunDir:     run.RunDir,
		Stages:     make([]stageResponse, 0, len(stages)),
	}
	for _, stage := range stages {
		resp.Stages = append(resp.Stages, stageResponse{
			ID:      stage.StageID,
			Worker:  stage.Worker,
			Status:  string(stage.Status),
			Attempt: stage.Attempt,
			Retries: stage.Retries,
			Summary: stage.Summary,
			LogPath: stage.LogPath,
		})
	}
	writeJSON(w, status, resp)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func httpStatusForError(err error) int {
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}
