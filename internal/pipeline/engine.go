package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/savitharaghunathan/shuttle/internal/artifact"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
)

type StartParams struct {
	TemplateID string
	Pipeline   *Pipeline
	CWD        string
	SpecPath   string
}

type Engine struct {
	Store    *store.Store
	Registry *worker.Registry
	Cfg      *config.Config

	// TemplateLoader resolves a template ID to its Pipeline definition.
	// Set by the CLI layer to templates.Get to avoid import cycles.
	TemplateLoader func(id string) (*Pipeline, error)

	// RetryBackoff is the fixed delay between retry attempts (default 2s).
	RetryBackoff time.Duration

	mu        sync.Mutex
	pipelines map[string]*Pipeline
	specPaths map[string]string
	cancels   map[string]context.CancelFunc
}

func NewEngine(s *store.Store, reg *worker.Registry, cfg *config.Config) *Engine {
	return &Engine{
		Store:        s,
		Registry:     reg,
		Cfg:          cfg,
		RetryBackoff: 2 * time.Second,
		pipelines:    make(map[string]*Pipeline),
		specPaths:    make(map[string]string),
		cancels:      make(map[string]context.CancelFunc),
	}
}

// EnsurePipeline reloads the pipeline definition into memory if a fresh Engine
// instance doesn't already have it cached (e.g. across CLI invocations).
func (e *Engine) EnsurePipeline(runID string) error {
	e.mu.Lock()
	if e.pipelines[runID] != nil {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	if e.TemplateLoader == nil {
		return fmt.Errorf("pipeline not found for run %s (no TemplateLoader configured)", runID)
	}

	run, err := e.Store.GetRun(runID)
	if err != nil {
		return fmt.Errorf("ensure pipeline: %w", err)
	}

	pl, err := e.TemplateLoader(run.TemplateID)
	if err != nil {
		return fmt.Errorf("ensure pipeline: load template %q: %w", run.TemplateID, err)
	}
	pl = e.applyDefaults(pl)

	specPath := filepath.Join(run.RunDir, "inputs", "spec.md")
	if _, statErr := os.Stat(specPath); statErr != nil {
		specPath = ""
	}

	e.mu.Lock()
	e.pipelines[runID] = pl
	if specPath != "" {
		e.specPaths[runID] = specPath
	}
	e.mu.Unlock()
	return nil
}

func (e *Engine) StartRun(ctx context.Context, p StartParams) (string, error) {
	if err := Validate(p.Pipeline); err != nil {
		return "", fmt.Errorf("validate pipeline: %w", err)
	}

	runID := uuid.New().String()

	runDir, err := artifact.InitRun(e.Cfg.WorkRoot, runID)
	if err != nil {
		return "", fmt.Errorf("init run workspace: %w", err)
	}

	if p.SpecPath != "" {
		if err := artifact.CopySpec(runDir, p.SpecPath); err != nil {
			return "", fmt.Errorf("copy spec: %w", err)
		}
	}

	if err := e.Store.CreateRun(store.Run{
		ID:         runID,
		TemplateID: p.TemplateID,
		Status:     store.RunStatusRunning,
		CWD:        p.CWD,
		RunDir:     runDir,
	}); err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

	pl := e.applyDefaults(p.Pipeline)

	var stageRuns []store.StageRun
	for _, stage := range pl.Stages {
		status := store.StageStatusPending
		if len(stage.DependsOn) == 0 {
			status = store.StageStatusReady
		}

		var timeoutMS *int64
		if stage.Timeout != "" {
			if d, err := time.ParseDuration(stage.Timeout); err == nil {
				ms := d.Milliseconds()
				timeoutMS = &ms
			}
		} else if e.Cfg.DefaultTimeout > 0 {
			ms := e.Cfg.DefaultTimeout.Milliseconds()
			timeoutMS = &ms
		}

		stageRuns = append(stageRuns, store.StageRun{
			ID:        uuid.New().String(),
			RunID:     runID,
			StageID:   stage.ID,
			Worker:    stage.Worker,
			Status:    status,
			Attempt:   0,
			Retries:   stage.Retries,
			TimeoutMS: timeoutMS,
		})
	}

	if err := e.Store.CreateStageRuns(stageRuns); err != nil {
		return "", fmt.Errorf("create stage runs: %w", err)
	}

	e.mu.Lock()
	e.pipelines[runID] = pl
	if p.SpecPath != "" {
		e.specPaths[runID] = p.SpecPath
	}
	e.mu.Unlock()

	return runID, nil
}

func (e *Engine) Tick(ctx context.Context, runID string) (bool, error) {
	if err := e.EnsurePipeline(runID); err != nil {
		return false, err
	}

	run, err := e.Store.GetRun(runID)
	if err != nil {
		return false, fmt.Errorf("get run: %w", err)
	}

	if isRunTerminal(run.Status) {
		return true, nil
	}

	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return false, fmt.Errorf("get stage runs: %w", err)
	}

	statusMap := make(map[string]string, len(stages))
	stageByID := make(map[string]store.StageRun, len(stages))
	runningCount := 0
	for _, sr := range stages {
		statusMap[sr.StageID] = string(sr.Status)
		stageByID[sr.StageID] = sr
		if sr.Status == store.StageStatusRunning {
			runningCount++
		}
	}

	e.mu.Lock()
	pl := e.pipelines[runID]
	specPath := e.specPaths[runID]
	e.mu.Unlock()
	if pl == nil {
		return false, fmt.Errorf("pipeline not found for run %s", runID)
	}

	readyIDs := ReadyIDs(pl.Stages, statusMap)

	slots := e.Cfg.Concurrency - runningCount
	dispatched := 0

	for _, sid := range readyIDs {
		if dispatched >= slots {
			break
		}

		sr := stageByID[sid]
		stageDef := findStage(pl, sid)

		// Manual gate: block without executing
		if sr.Worker == "manual" {
			sr.Status = store.StageStatusWaitingApproval
			if err := e.Store.UpdateStage(sr); err != nil {
				return false, fmt.Errorf("set stage %s waiting_approval: %w", sid, err)
			}
			if err := e.Store.UpdateRunStatus(runID, store.RunStatusBlocked); err != nil {
				return false, fmt.Errorf("block run: %w", err)
			}
			return true, nil
		}

		w, ok := e.Registry.Get(sr.Worker)
		if !ok {
			return false, fmt.Errorf("worker %q not registered", sr.Worker)
		}

		sr.Status = store.StageStatusRunning
		sr.Attempt++
		if err := e.Store.UpdateStage(sr); err != nil {
			return false, fmt.Errorf("set stage %s running: %w", sid, err)
		}

		runCtx, runCancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.cancels[runID] = runCancel
		e.mu.Unlock()

		execCtx := runCtx
		if sr.TimeoutMS != nil && *sr.TimeoutMS > 0 {
			var tCancel context.CancelFunc
			execCtx, tCancel = context.WithTimeout(runCtx, time.Duration(*sr.TimeoutMS)*time.Millisecond)
			defer tCancel()
		}

		req := worker.Request{
			RunID:    runID,
			StageID:  sid,
			WorkDir:  run.CWD,
			RunDir:   run.RunDir,
			SpecPath: specPath,
		}
		if stageDef != nil {
			req.Prompt = stageDef.Prompt
			req.Command = stageDef.Command
		}

		result, execErr := w.Execute(execCtx, req)
		runCancel()

		if execErr != nil || result.Status == worker.StatusFailed {
			if sr.Attempt <= sr.Retries {
				if e.RetryBackoff > 0 {
					time.Sleep(e.RetryBackoff)
				}
				sr.Status = store.StageStatusReady
				msg := fmt.Sprintf("attempt %d failed, retrying", sr.Attempt)
				sr.Summary = &msg
				if err := e.Store.UpdateStage(sr); err != nil {
					return false, err
				}
			} else {
				sr.Status = store.StageStatusFailed
				msg := result.Summary
				if execErr != nil {
					msg = execErr.Error()
				}
				sr.Summary = &msg
				if err := e.Store.UpdateStage(sr); err != nil {
					return false, err
				}
				if err := e.skipRemaining(runID); err != nil {
					return false, err
				}
				if err := e.Store.UpdateRunStatus(runID, store.RunStatusFailed); err != nil {
					return false, err
				}
				return true, nil
			}
		} else {
			sr.Status = store.StageStatusSucceeded
			sr.Summary = &result.Summary
			if err := e.Store.UpdateStage(sr); err != nil {
				return false, err
			}
			if err := e.materializeOutputs(run.RunDir, sid, result.Outputs); err != nil {
				return false, err
			}
		}

		dispatched++
	}

	// Re-evaluate run state after dispatches
	stages, err = e.Store.GetStageRuns(runID)
	if err != nil {
		return false, err
	}

	allDone := true
	anyFailed := false
	anyWaiting := false

	for _, sr := range stages {
		switch sr.Status {
		case store.StageStatusWaitingApproval:
			allDone = false
			anyWaiting = true
		case store.StageStatusFailed, store.StageStatusRejected:
			anyFailed = true
		case store.StageStatusSucceeded, store.StageStatusSkipped, store.StageStatusCancelled:
			// terminal — ok
		default:
			allDone = false
		}
	}

	if allDone {
		finalStatus := store.RunStatusSucceeded
		if anyFailed {
			finalStatus = store.RunStatusFailed
		}
		if err := e.Store.UpdateRunStatus(runID, finalStatus); err != nil {
			return false, err
		}
		return true, nil
	}

	if anyWaiting {
		return true, nil
	}

	return false, nil
}

func (e *Engine) RunUntilBlockOrDone(ctx context.Context, runID string) error {
	for {
		done, err := e.Tick(ctx, runID)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (e *Engine) Approve(runID, stageID, message string) error {
	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return fmt.Errorf("get stage runs: %w", err)
	}

	for _, sr := range stages {
		if sr.StageID != stageID {
			continue
		}
		if sr.Status != store.StageStatusWaitingApproval {
			return fmt.Errorf("stage %q status is %s, not waiting_approval", stageID, sr.Status)
		}
		sr.Status = store.StageStatusSucceeded
		sr.Summary = &message
		if err := e.Store.UpdateStage(sr); err != nil {
			return err
		}

		run, err := e.Store.GetRun(runID)
		if err != nil {
			return err
		}
		approvalPath := filepath.Join(run.RunDir, "approvals", stageID+".md")
		if err := os.WriteFile(approvalPath, []byte(message), 0o644); err != nil {
			return fmt.Errorf("write approval: %w", err)
		}

		return e.Store.UpdateRunStatus(runID, store.RunStatusRunning)
	}

	return fmt.Errorf("stage %q not found in run %s", stageID, runID)
}

func (e *Engine) Reject(runID, stageID, message string) error {
	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return fmt.Errorf("get stage runs: %w", err)
	}

	for _, sr := range stages {
		if sr.StageID != stageID {
			continue
		}
		if sr.Status != store.StageStatusWaitingApproval {
			return fmt.Errorf("stage %q status is %s, not waiting_approval", stageID, sr.Status)
		}
		sr.Status = store.StageStatusRejected
		sr.Summary = &message
		if err := e.Store.UpdateStage(sr); err != nil {
			return err
		}
		if err := e.skipRemaining(runID); err != nil {
			return err
		}
		return e.Store.UpdateRunStatus(runID, store.RunStatusFailed)
	}

	return fmt.Errorf("stage %q not found in run %s", stageID, runID)
}

func (e *Engine) Cancel(runID string) error {
	e.mu.Lock()
	if cancel, ok := e.cancels[runID]; ok {
		cancel()
		delete(e.cancels, runID)
	}
	e.mu.Unlock()

	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return fmt.Errorf("get stage runs: %w", err)
	}

	for _, sr := range stages {
		switch sr.Status {
		case store.StageStatusRunning:
			sr.Status = store.StageStatusCancelled
			if err := e.Store.UpdateStage(sr); err != nil {
				return err
			}
		case store.StageStatusPending, store.StageStatusReady, store.StageStatusWaitingApproval:
			sr.Status = store.StageStatusSkipped
			if err := e.Store.UpdateStage(sr); err != nil {
				return err
			}
		}
	}

	return e.Store.UpdateRunStatus(runID, store.RunStatusCancelled)
}

func (e *Engine) applyDefaults(p *Pipeline) *Pipeline {
	pl := &Pipeline{
		ID:      p.ID,
		Version: p.Version,
		Stages:  make([]Stage, len(p.Stages)),
	}
	copy(pl.Stages, p.Stages)

	for i := range pl.Stages {
		if pl.Stages[i].Worker == "" {
			pl.Stages[i].Worker = e.Cfg.DefaultWorker
		}
		if len(pl.Stages[i].Command) == 0 {
			switch pl.Stages[i].ID {
			case "test":
				pl.Stages[i].Command = e.Cfg.Test.Command
			case "open_pr":
				pl.Stages[i].Command = e.Cfg.OpenPR.Command
			case "ship":
				pl.Stages[i].Command = e.Cfg.Ship.Command
			}
		}
	}

	return pl
}

func (e *Engine) skipRemaining(runID string) error {
	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return err
	}
	for _, sr := range stages {
		if sr.Status == store.StageStatusPending || sr.Status == store.StageStatusReady {
			sr.Status = store.StageStatusSkipped
			if err := e.Store.UpdateStage(sr); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) materializeOutputs(runDir, stageID string, outputs map[string]string) error {
	if len(outputs) == 0 {
		return nil
	}
	outDir := filepath.Join(runDir, "artifacts", stageID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	for name, content := range outputs {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write output %s/%s: %w", stageID, name, err)
		}
	}
	return nil
}

func isRunTerminal(s store.RunStatus) bool {
	switch s {
	case store.RunStatusSucceeded, store.RunStatusFailed, store.RunStatusCancelled:
		return true
	}
	return false
}

func findStage(pl *Pipeline, id string) *Stage {
	for i := range pl.Stages {
		if pl.Stages[i].ID == id {
			return &pl.Stages[i]
		}
	}
	return nil
}
