package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/savitharaghunathan/shuttle/internal/artifact"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
)

type StartParams struct {
	TemplateID  string
	Pipeline    *Pipeline
	CWD         string
	SpecPath    string
	SpecContent string
	Agent       string
	Model       string
	IssueRef    string
	ParentRunID string
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

	// Progress receives live stage announcements and worker output.
	Progress io.Writer

	mu        sync.Mutex
	pipelines map[string]*Pipeline
	specPaths map[string]string
	runOpts   map[string]runOptions
	cancels   map[string]context.CancelFunc
	startWarn map[string][]string
}

type runOptions struct {
	Agent string
	Model string
}

func NewEngine(s *store.Store, reg *worker.Registry, cfg *config.Config) *Engine {
	return &Engine{
		Store:        s,
		Registry:     reg,
		Cfg:          cfg,
		RetryBackoff: 2 * time.Second,
		pipelines:    make(map[string]*Pipeline),
		specPaths:    make(map[string]string),
		runOpts:      make(map[string]runOptions),
		cancels:      make(map[string]context.CancelFunc),
		startWarn:    make(map[string][]string),
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
	// apply defaults using the persisted resolved agent so stage workers match what was saved
	pl = e.applyDefaults(pl, run.Agent)

	specPath := filepath.Join(run.RunDir, "inputs", "spec.md")
	if _, statErr := os.Stat(specPath); statErr != nil {
		specPath = ""
	}

	e.mu.Lock()
	e.pipelines[runID] = pl
	if specPath != "" {
		e.specPaths[runID] = specPath
	}
	// restore run options (agent/model) from the DB so fresh engines use the same settings
	e.runOpts[runID] = runOptions{Agent: run.Agent, Model: run.Model}
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
	} else if p.SpecContent != "" {
		if err := artifact.WriteSpec(runDir, []byte(p.SpecContent)); err != nil {
			return "", fmt.Errorf("write spec: %w", err)
		}
	}

	// Resolve pipeline defaults and the resolved agent before persisting the run.
	pl := e.applyDefaults(p.Pipeline, p.Agent)

	resolvedAgent := p.Agent
	if resolvedAgent == "" {
		resolvedAgent = e.Cfg.DefaultWorker
	}

	// Validate parent run exists (if provided) before creating the new run.
	if p.ParentRunID != "" {
		if _, err := e.Store.GetRun(p.ParentRunID); err != nil {
			return "", fmt.Errorf("parent run %s: %w", p.ParentRunID, err)
		}
	}

	startWarnings, err := e.concurrentIssueWarnings(p.IssueRef)
	if err != nil {
		return "", err
	}

	if err := e.Store.CreateRun(store.Run{
		ID:          runID,
		TemplateID:  p.TemplateID,
		Status:      store.RunStatusRunning,
		CWD:         p.CWD,
		RunDir:      runDir,
		Agent:       resolvedAgent,
		Model:       p.Model,
		IssueRef:    p.IssueRef,
		ParentRunID: p.ParentRunID,
	}); err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

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
	e.runOpts[runID] = runOptions{Agent: resolvedAgent, Model: p.Model}
	if len(startWarnings) > 0 {
		e.startWarn[runID] = append([]string(nil), startWarnings...)
	}
	e.mu.Unlock()

	return runID, nil
}

func (e *Engine) StartWarnings(runID string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	warns := e.startWarn[runID]
	return append([]string(nil), warns...)
}

func (e *Engine) concurrentIssueWarnings(issueRef string) ([]string, error) {
	if strings.TrimSpace(issueRef) == "" {
		return nil, nil
	}
	runs, err := e.Store.ListRuns(store.ListRunsOpts{
		Limit:    100,
		IssueRef: issueRef,
		Statuses: []store.RunStatus{
			store.RunStatusPending,
			store.RunStatusRunning,
			store.RunStatusBlocked,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list active issue runs: %w", err)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	branch, err := issue.BranchName(issueRef, "")
	if err != nil {
		return nil, nil
	}
	return []string{
		fmt.Sprintf(
			"another run for %s is already active (%s); concurrent work may conflict on branch %s",
			issueRef,
			runs[0].ID,
			branch,
		),
	}, nil
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
	opts := e.runOpts[runID]
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
			e.progressf("⏸ stage %s waiting for approval\n", sid)
			sr.Status = store.StageStatusWaitingApproval
			if err := e.Store.UpdateStage(sr); err != nil {
				return false, fmt.Errorf("set stage %s waiting_approval: %w", sid, err)
			}
			if err := e.Store.UpdateRunStatus(runID, store.RunStatusBlocked); err != nil {
				return false, fmt.Errorf("block run: %w", err)
			}
			return true, nil
		}

		// Render stage templates before execution
		branch, resolveErr := issue.ResolveBranch(
			run.IssueRef,
			run.ID,
			run.ParentRunID,
			func(id string) (string, string, string, error) {
				parent, err := e.Store.GetRun(id)
				if err != nil {
					return "", "", "", err
				}
				return parent.IssueRef, parent.ID, parent.ParentRunID, nil
			},
		)
		if resolveErr != nil {
			sr.Status = store.StageStatusFailed
			msg := fmt.Sprintf("resolve branch: %v", resolveErr)
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

		rctx := RenderContext{
			RunID:      runID,
			StageID:    sid,
			Cwd:        run.CWD,
			RunDir:     run.RunDir,
			IssueRef:   run.IssueRef,
			Branch:     branch,
			ShuttleBin: ShuttleBin(),
			Artifacts:  collectDir(filepath.Join(run.RunDir, "artifacts")),
			Inputs:     collectDir(filepath.Join(run.RunDir, "inputs")),
			Env:        envMap(),
		}
		var renderedPrompt string
		var renderedCmd []string
		if stageDef != nil {
			var renderErr error
			renderedPrompt, renderErr = Render(stageDef.Prompt, rctx)
			if renderErr != nil {
				sr.Status = store.StageStatusFailed
				msg := fmt.Sprintf("render prompt: %v", renderErr)
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
			renderedCmd = make([]string, len(stageDef.Command))
			for i, arg := range stageDef.Command {
				renderedCmd[i], renderErr = Render(arg, rctx)
				if renderErr != nil {
					sr.Status = store.StageStatusFailed
					msg := fmt.Sprintf("render command[%d]: %v", i, renderErr)
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
			}
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
		var tCancel context.CancelFunc
		if sr.TimeoutMS != nil && *sr.TimeoutMS > 0 {
			execCtx, tCancel = context.WithTimeout(runCtx, time.Duration(*sr.TimeoutMS)*time.Millisecond)
		}

		e.progressf("→ stage %s (%s) starting...\n", sid, sr.Worker)

		req := worker.Request{
			RunID:    runID,
			StageID:  sid,
			WorkDir:  run.CWD,
			RunDir:   run.RunDir,
			SpecPath: specPath,
			Prompt:   renderedPrompt,
			Command:  renderedCmd,
			Agent:    opts.Agent,
			Model:    opts.Model,
			Output:   e.Progress,
			LogPath:  filepath.Join(run.RunDir, "logs", sid+".log"),
		}

		result, execErr := w.Execute(execCtx, req)
		if tCancel != nil {
			tCancel()
		}
		runCancel()

		if execErr != nil || result.Status == worker.StatusFailed {
			if sr.Attempt <= sr.Retries {
				e.progressf("↻ stage %s failed, retrying (attempt %d)\n", sid, sr.Attempt)
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
				logPath := filepath.Join(run.RunDir, "logs", sid+".log")
				sr.LogPath = &logPath
				e.progressf("✗ stage %s failed\n", sid)
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
			if sid == "implement" && isGitRepo(run.CWD) {
				found, verifyErr := verifyRemoteBranch(run.CWD, branch)
				switch {
				case verifyErr != nil:
					e.progressf("! could not verify remote branch %s: %v\n", branch, verifyErr)
				case !found:
					sr.Status = store.StageStatusFailed
					msg := fmt.Sprintf("expected branch %s not found on remote origin; agent must push to this branch", branch)
					sr.Summary = &msg
					logPath := filepath.Join(run.RunDir, "logs", sid+".log")
					sr.LogPath = &logPath
					e.progressf("✗ stage %s failed\n", sid)
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
			}

			sr.Status = store.StageStatusSucceeded
			sr.Summary = &result.Summary
			logPath := filepath.Join(run.RunDir, "logs", sid+".log")
			sr.LogPath = &logPath
			e.progressf("✓ stage %s succeeded\n", sid)
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
		approvalDir := filepath.Join(run.RunDir, "approvals")
		if err := os.MkdirAll(approvalDir, 0o755); err != nil {
			return fmt.Errorf("create approvals dir: %w", err)
		}
		approvalPath := filepath.Join(approvalDir, stageID+".md")
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

func (e *Engine) applyDefaults(p *Pipeline, agentOverride string) *Pipeline {
	pl := &Pipeline{
		ID:      p.ID,
		Version: p.Version,
		Stages:  make([]Stage, len(p.Stages)),
	}
	copy(pl.Stages, p.Stages)

	defaultWorker := e.Cfg.DefaultWorker
	if agentOverride != "" {
		defaultWorker = agentOverride
	}

	for i := range pl.Stages {
		if pl.Stages[i].Worker == "" {
			pl.Stages[i].Worker = defaultWorker
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

func collectDir(dir string) map[string]string {
	m := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return m
	}
	for _, e := range entries {
		m[e.Name()] = filepath.Join(dir, e.Name())
	}
	return m
}

func envMap() map[string]string {
	m := make(map[string]string)
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func (e *Engine) progressf(format string, args ...any) {
	if e.Progress == nil {
		return
	}
	fmt.Fprintf(e.Progress, format, args...)
}
