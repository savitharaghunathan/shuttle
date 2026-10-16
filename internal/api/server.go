package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/savitharaghunathan/shuttle/internal/home"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/registry"
	"github.com/savitharaghunathan/shuttle/internal/runinput"
	"github.com/savitharaghunathan/shuttle/internal/store"
)

type Options struct {
	CWD         string
	PipelineIDs []string
	// LookupRepo and FetchSpec override the home registry and gh issue fetch.
	// Leave nil in production.
	LookupRepo func(owner, name string) (string, error)
	FetchSpec  func(ctx context.Context, owner, name string, number int) (string, error)
}

type Server struct {
	engine      *pipeline.Engine
	cwd         string
	pipelineIDs []string
	lookupRepo  func(owner, name string) (string, error)
	fetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}

func NewHandler(engine *pipeline.Engine, opts Options) http.Handler {
	s := &Server{
		engine:      engine,
		cwd:         opts.CWD,
		pipelineIDs: append([]string(nil), opts.PipelineIDs...),
		lookupRepo:  opts.LookupRepo,
		fetchSpec:   opts.FetchSpec,
	}

	r := chi.NewRouter()
	// Note: register the list route before the parameterized {id} route.
	r.Get("/v1/runs", s.listRuns)
	r.Post("/v1/runs", s.createRun)
	r.Get("/v1/runs/{id}", s.getRun)
	r.Post("/v1/runs/{id}/resume", s.resumeRun)
	r.Post("/v1/runs/{id}/retry", s.retryRun)
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
	Agent      string `json:"agent,omitempty"`
	Model      string `json:"model,omitempty"`
	IssueURL   string `json:"issue_url,omitempty"`
	Headless   bool   `json:"headless,omitempty"`
}

type decisionRequest struct {
	StageID string `json:"stage_id"`
	Message string `json:"message,omitempty"`
}

type runResponse struct {
	ID          string          `json:"id"`
	TemplateID  string          `json:"template_id"`
	Status      string          `json:"status"`
	CWD         string          `json:"cwd"`
	RunDir      string          `json:"run_dir"`
	Agent       string          `json:"agent,omitempty"`
	Model       string          `json:"model,omitempty"`
	IssueRef    string          `json:"issue_ref,omitempty"`
	ParentRunID string          `json:"parent_run_id,omitempty"`
	OutputsMode string          `json:"outputs_mode,omitempty"`
	Stages      []stageResponse `json:"stages"`
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

type retryRequest struct {
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
	Force bool   `json:"force,omitempty"`
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
	if req.SpecPath != "" && !filepath.IsAbs(req.SpecPath) {
		writeError(w, http.StatusBadRequest, errors.New("spec_path must be an absolute path"))
		return
	}
	templateID, err := runinput.ResolveTemplate(req.TemplateID, req.Headless, req.IssueURL != "", req.SpecPath != "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.engine.TemplateLoader == nil {
		writeError(w, http.StatusInternalServerError, errors.New("template loader is not configured"))
		return
	}
	pl, err := s.engine.TemplateLoader(templateID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	opts := runinput.Options{FallbackCWD: req.CWD, FetchSpec: s.fetchSpec}
	if opts.FallbackCWD == "" {
		opts.FallbackCWD = s.cwd
	}
	lookup, closeLookup := s.servedRepoLookup()
	defer closeLookup()
	opts.LookupRepo = lookup
	in, err := runinput.ResolveInput(r.Context(), req.IssueURL, opts)
	if err != nil {
		writeError(w, inputErrorStatus(err), err)
		return
	}

	runID, err := s.engine.StartRun(r.Context(), pipeline.StartParams{
		TemplateID:  templateID,
		Pipeline:    pl,
		CWD:         in.CWD,
		SpecPath:    req.SpecPath,
		SpecContent: in.SpecContent,
		Agent:       req.Agent,
		Model:       req.Model,
		IssueRef:    in.IssueRef,
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

// servedRepoLookup returns a repo lookup for one request that only accepts the
// repo this server was started in: the engine's config, DB, and work root all
// belong to that repo. It runs before the issue fetch, so a mismatch never
// calls gh. The home registry is opened on first use and closed by the
// returned func.
func (s *Server) servedRepoLookup() (func(owner, name string) (string, error), func()) {
	var reg *registry.Registry
	base := s.lookupRepo
	if base == nil {
		base = func(owner, name string) (string, error) {
			if reg == nil {
				if err := os.MkdirAll(home.Dir(), 0o755); err != nil {
					return "", err
				}
				opened, err := registry.Open(home.RegistryDB())
				if err != nil {
					return "", err
				}
				reg = opened
			}
			entry, err := reg.Get(owner, name)
			if err != nil {
				return "", err
			}
			return entry.Path, nil
		}
	}
	lookup := func(owner, name string) (string, error) {
		path, err := base(owner, name)
		if err != nil {
			return "", err
		}
		served := s.cwd
		if served == "" {
			if served, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		if !sameDir(path, served) {
			return "", fmt.Errorf("issue repo %s/%s is imported at %s; this server serves %s (run shuttle serve from that repo)", owner, name, path, served)
		}
		return path, nil
	}
	closeFn := func() {
		if reg != nil {
			_ = reg.Close()
		}
	}
	return lookup, closeFn
}

// sameDir compares by file identity so symlinks, /var vs /private/var, and
// case-insensitive filesystems don't cause false mismatches.
func sameDir(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	return cleanAbs(a) == cleanAbs(b)
}

func cleanAbs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func inputErrorStatus(err error) int {
	var fetchErr *runinput.FetchError
	if errors.As(err, &fetchErr) {
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id, err := s.engine.Store.ResolveRunID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	s.writeRun(w, http.StatusOK, id)
}

func (s *Server) resumeRun(w http.ResponseWriter, r *http.Request) {
	id, err := s.engine.Store.ResolveRunID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	if err := s.engine.ResumeRun(r.Context(), id); err != nil {
		writeError(w, recoveryHTTPStatus(err), err)
		return
	}
	s.writeRun(w, http.StatusOK, id)
}

func (s *Server) retryRun(w http.ResponseWriter, r *http.Request) {
	id, err := s.engine.Store.ResolveRunID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	var req retryRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty body OK
	}
	res, err := s.engine.RetryRun(r.Context(), id, pipeline.RetryParams{Agent: req.Agent, Model: req.Model, Force: req.Force})
	if err != nil {
		writeError(w, recoveryHTTPStatus(err), err)
		return
	}
	run, _ := s.engine.Store.GetRun(res.RunID)
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            res.RunID,
		"parent_run_id": run.ParentRunID,
		"status":        string(run.Status),
		"warnings":      warnings,
	})
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "id")
	runID, err := s.engine.Store.ResolveRunID(raw)
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
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

	raw := chi.URLParam(r, "id")
	runID, err := s.engine.Store.ResolveRunID(raw)
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
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

func parseSinceAPI(since string) (time.Time, error) {
	since = strings.TrimSpace(since)
	if since == "" {
		return time.Time{}, nil
	}
	if len(since) > 1 && since[len(since)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(since, "%dd", &days); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(since); err == nil {
		return time.Now().Add(-d), nil
	}
	if t, err := time.Parse("2006-01-02", since); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid since %q", since)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := store.ListRunsOpts{
		Agent:    q.Get("agent"),
		Template: q.Get("template"),
		IssueRef: q.Get("issue"),
	}
	if m := q.Get("model"); m == "unset" {
		opts.ModelUnset = true
	} else if m != "" {
		opts.Model = m
	}
	if st := q.Get("status"); st != "" {
		for _, part := range strings.Split(st, ",") {
			if p := strings.TrimSpace(part); p != "" {
				opts.Statuses = append(opts.Statuses, store.RunStatus(p))
			}
		}
	}
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			opts.Limit = n
		}
	}
	if since := q.Get("since"); since != "" {
		if t, err := parseSinceAPI(since); err == nil {
			opts.Since = t
		}
	}
	if p := q.Get("parent"); p != "" {
		if pid, err := s.engine.Store.ResolveRunID(p); err == nil {
			opts.ParentID = pid
		}
	}
	runs, err := s.engine.Store.ListRuns(opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.runsToJSON(runs))
}

func (s *Server) runsToJSON(runs []store.Run) []any {
	type runJSON struct {
		ID           string `json:"id"`
		ParentRunID  string `json:"parent_run_id,omitempty"`
		Status       string `json:"status"`
		Agent        string `json:"agent"`
		Model        string `json:"model"`
		TemplateID   string `json:"template_id"`
		IssueRef     string `json:"issue_ref,omitempty"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		WallDuration int64  `json:"wall_duration"`
		CurrentStage string `json:"current_stage"`
		OutputsMode  string `json:"outputs_mode,omitempty"`
	}
	cache := map[string]*pipeline.Pipeline{}
	out := make([]any, 0, len(runs))
	for _, run := range runs {
		cs := "unknown"
		pl, ok := cache[run.TemplateID]
		if !ok {
			loaded, err := s.engine.TemplateLoader(run.TemplateID)
			if err == nil {
				pl = loaded
			}
			cache[run.TemplateID] = pl
		}
		if pl != nil {
			if stages, err := s.engine.Store.GetStageRuns(run.ID); err == nil {
				cs = pipeline.CurrentStage(pl, stages)
			}
		}
		out = append(out, runJSON{
			ID:          run.ID,
			ParentRunID: run.ParentRunID,
			Status:      string(run.Status),
			Agent:       run.Agent,
			Model:       run.Model,
			TemplateID:  run.TemplateID,
			IssueRef:    run.IssueRef,
			CreatedAt:   run.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   run.UpdatedAt.UTC().Format(time.RFC3339),
			WallDuration: int64(func() int64 {
				d := run.UpdatedAt.Sub(run.CreatedAt)
				if run.Status != store.RunStatusSucceeded && run.Status != store.RunStatusFailed && run.Status != store.RunStatusCancelled {
					d = time.Since(run.CreatedAt)
				}
				return int64(d.Seconds())
			}()),
			CurrentStage: cs,
			OutputsMode:  pipeline.OutputsMode(run.TemplateID, pl),
		})
	}
	return out
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
	outputsMode := ""
	if s.engine.TemplateLoader != nil {
		if pl, err := s.engine.TemplateLoader(run.TemplateID); err == nil {
			outputsMode = pipeline.OutputsMode(run.TemplateID, pl)
		}
	}

	resp := runResponse{
		ID:          run.ID,
		TemplateID:  run.TemplateID,
		Status:      string(run.Status),
		CWD:         run.CWD,
		RunDir:      run.RunDir,
		Agent:       run.Agent,
		Model:       run.Model,
		IssueRef:    run.IssueRef,
		ParentRunID: run.ParentRunID,
		OutputsMode: outputsMode,
		Stages:      make([]stageResponse, 0, len(stages)),
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
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrRunIDNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// recoveryHTTPStatus maps engine recovery errors to HTTP codes (409 for state/drift/worker).
func recoveryHTTPStatus(err error) int {
	msg := err.Error()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	case strings.Contains(msg, "no spec"):
		return http.StatusBadRequest
	case strings.Contains(msg, "cannot resume"),
		strings.Contains(msg, "cannot retry"),
		strings.Contains(msg, "use --force"),
		strings.Contains(msg, "no longer matches"),
		strings.Contains(msg, "no longer configured"),
		strings.Contains(msg, "not configured"),
		strings.Contains(msg, "no stage to resume"):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
