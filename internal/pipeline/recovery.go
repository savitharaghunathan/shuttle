package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

var builtinWorkers = map[string]bool{"manual": true, "shell": true, "intake": true}
var gitPreflight = GitPreflight

func (e *Engine) validateTemplate(runID string, pl *Pipeline, stages []store.StageRun) error {
	inTemplate := map[string]bool{}
	for _, st := range pl.Stages {
		inTemplate[st.ID] = true
	}
	inDB := map[string]bool{}
	for _, sr := range stages {
		inDB[sr.StageID] = true
	}
	var missingInTemplate, extraInTemplate []string
	for id := range inDB {
		if !inTemplate[id] {
			missingInTemplate = append(missingInTemplate, id)
		}
	}
	for id := range inTemplate {
		if !inDB[id] {
			extraInTemplate = append(extraInTemplate, id)
		}
	}
	if len(missingInTemplate) == 0 && len(extraInTemplate) == 0 {
		return nil
	}
	sort.Strings(missingInTemplate)
	sort.Strings(extraInTemplate)
	return fmt.Errorf(
		"template no longer matches run %s:\n  missing stages in template: %v\n  extra stages in template:   %v\nrestore the template definition, or start fresh with: shuttle retry %s",
		shortRunID(runID), missingInTemplate, extraInTemplate, shortRunID(runID))
}

func (e *Engine) validateWorkers(runID string, stages []store.StageRun) error {
	for _, sr := range stages {
		if builtinWorkers[sr.Worker] || sr.Worker == "" {
			continue
		}
		if _, ok := e.Registry.Get(sr.Worker); !ok {
			return fmt.Errorf(
				"worker %q is no longer configured for run %s\nadd it back to shuttle.yaml workers:, or retry with a different agent:\n  shuttle retry %s --agent <name>",
				sr.Worker, shortRunID(runID), shortRunID(runID))
		}
	}
	return nil
}

func shortRunID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func recoveryStamp(ts time.Time) string { return ts.UTC().Format("20060102T150405Z") }

func archiveStageArtifacts(runDir string, stageIDs []string, ts time.Time) error {
	base := filepath.Join(runDir, ".recovery", recoveryStamp(ts))
	for _, id := range stageIDs {
		dst := filepath.Join(base, id)
		sources := []string{
			filepath.Join(runDir, "logs", id+".log"),
			filepath.Join(runDir, "artifacts", id), // dir or legacy file
		}
		for _, src := range sources {
			if _, err := os.Stat(src); err != nil {
				continue // missing is fine
			}
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return fmt.Errorf("mkdir recovery %s: %w", dst, err)
			}
			target := filepath.Join(dst, filepath.Base(src))
			if err := os.Rename(src, target); err != nil {
				if err := copyTree(src, target); err != nil {
					return fmt.Errorf("archive %s: %w", src, err)
				}
				_ = os.RemoveAll(src)
			}
		}
	}
	return nil
}

// RetryParams contains options for RetryRun.
type RetryParams struct {
	Agent string
	Model string
	Force bool
}

// RetryResult is returned by RetryRun with new run id and any git warnings.
type RetryResult struct {
	RunID    string
	Warnings []string
}

// copyTree copies a file or directory tree (cross-device rename fallback).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ResumeRun resumes a failed or cancelled run by finding the resume point
// (earliest stage in template order that is failed/cancelled/rejected, or the
// first skipped stage when the run was cancelled), archiving artifacts for the
// reset stages, resetting them in the DB, updating the run status to running,
// and continuing execution.
func (e *Engine) ResumeRun(ctx context.Context, runID string) error {
	run, err := e.Store.GetRun(runID)
	if err != nil {
		return err
	}
	if run.Status != store.RunStatusFailed && run.Status != store.RunStatusCancelled {
		return fmt.Errorf("run %s status is %s, cannot resume (want failed or cancelled)", shortRunID(runID), run.Status)
	}
	if err := e.EnsurePipeline(runID); err != nil {
		return err
	}
	e.mu.Lock()
	pl := e.pipelines[runID]
	e.mu.Unlock()
	if pl == nil {
		return fmt.Errorf("pipeline not loaded for run %s", shortRunID(runID))
	}
	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return err
	}
	if err := e.validateTemplate(runID, pl, stages); err != nil {
		return err
	}
	if err := e.validateWorkers(runID, stages); err != nil {
		return err
	}

	byID := make(map[string]store.StageRun, len(stages))
	for _, sr := range stages {
		byID[sr.StageID] = sr
	}

	// resume point: earliest stage (template order) in failed/cancelled/rejected,
	// or skipped only when the run was cancelled and nothing else matched.
	resumeIdx := -1
	for i, st := range pl.Stages {
		switch byID[st.ID].Status {
		case store.StageStatusFailed, store.StageStatusCancelled, store.StageStatusRejected:
			resumeIdx = i
		}
		if resumeIdx >= 0 {
			break
		}
	}
	if resumeIdx == -1 && run.Status == store.RunStatusCancelled {
		for i, st := range pl.Stages {
			if byID[st.ID].Status == store.StageStatusSkipped {
				resumeIdx = i
				break
			}
		}
	}
	if resumeIdx == -1 {
		return fmt.Errorf("run %s has no stage to resume", shortRunID(runID))
	}

	// stages to reset: resume point + all downstream (template order)
	var resetIDs []string
	var updates []store.StageRun
	for i := resumeIdx; i < len(pl.Stages); i++ {
		sr := byID[pl.Stages[i].ID]
		resetIDs = append(resetIDs, sr.StageID)
		if i == resumeIdx {
			sr.Status = store.StageStatusReady
		} else {
			sr.Status = store.StageStatusPending
		}
		sr.Attempt = 0
		sr.Summary = nil
		sr.LogPath = nil
		updates = append(updates, sr)
	}

	// filesystem first (archive), then DB transaction
	if err := archiveStageArtifacts(run.RunDir, resetIDs, time.Now().UTC()); err != nil {
		return fmt.Errorf("archive on resume: %w", err)
	}
	if err := e.Store.ResetStages(updates); err != nil {
		return err
	}
	if err := e.Store.UpdateRunStatus(runID, store.RunStatusRunning); err != nil {
		return err
	}
	// refresh cached pipeline/runOpts already done by EnsurePipeline
	return e.RunUntilBlockOrDone(ctx, runID)
}

// RetryRun creates a new run reusing a parent's spec and honoring overrides.
func (e *Engine) RetryRun(ctx context.Context, runID string, p RetryParams) (RetryResult, error) {
	parent, err := e.Store.GetRun(runID)
	if err != nil {
		return RetryResult{}, err
	}
	switch parent.Status {
	case store.RunStatusFailed, store.RunStatusCancelled:
		// ok
	case store.RunStatusSucceeded:
		if !p.Force {
			return RetryResult{}, fmt.Errorf("run %s succeeded; use --force to retry", shortRunID(runID))
		}
	default:
		return RetryResult{}, fmt.Errorf("run %s status is %s, cannot retry", shortRunID(runID), parent.Status)
	}

	specPath := filepath.Join(parent.RunDir, "inputs", "spec.md")
	if _, err := os.Stat(specPath); err != nil {
		return RetryResult{}, fmt.Errorf("run %s has no spec to retry (%s)", shortRunID(runID), specPath)
	}

	agent := parent.Agent
	if p.Agent != "" {
		agent = p.Agent
	}
	model := parent.Model
	if p.Model != "" {
		model = p.Model
	}

	if e.TemplateLoader == nil {
		return RetryResult{}, fmt.Errorf("no TemplateLoader configured")
	}
	pl, err := e.TemplateLoader(parent.TemplateID)
	if err != nil {
		return RetryResult{}, fmt.Errorf("load template %q: %w", parent.TemplateID, err)
	}

	// validate template/stages against DB
	stages, err := e.Store.GetStageRuns(runID)
	if err != nil {
		return RetryResult{}, err
	}
	if err := e.validateTemplate(runID, pl, stages); err != nil {
		return RetryResult{}, err
	}
	if err := e.validateWorkers(runID, stages); err != nil {
		return RetryResult{}, err
	}

	// explicit worker validation for overridden/resolved agent
	if agent != "" && !builtinWorkers[agent] {
		if _, ok := e.Registry.Get(agent); !ok {
			return RetryResult{}, fmt.Errorf("worker %q not configured; retry with --agent <name>", agent)
		}
	}

	newID, err := e.StartRun(ctx, StartParams{
		TemplateID:  parent.TemplateID,
		Pipeline:    pl,
		CWD:         parent.CWD,
		SpecPath:    specPath,
		Agent:       agent,
		Model:       model,
		IssueRef:    parent.IssueRef,
		ParentRunID: parent.ID,
	})
	if err != nil {
		return RetryResult{}, err
	}
	warnings := e.StartWarnings(newID)
	if parent.TemplateID == "sdlc" || parent.TemplateID == "sdlc-headless" {
		warnings = append(warnings, "retry starts a new run; plan/design artifacts are not copied from the parent run")
	}
	if err := e.RunUntilBlockOrDone(ctx, newID); err != nil {
		return RetryResult{RunID: newID, Warnings: warnings}, err
	}
	return RetryResult{RunID: newID, Warnings: warnings}, nil
}
