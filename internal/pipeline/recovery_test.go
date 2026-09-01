package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
)

// scriptedWorker is a small stateful worker that returns a scripted sequence
// of results per stage id, useful for testing resume semantics.
type scriptedWorker struct {
	mu    sync.Mutex
	seq   map[string][]worker.Result
	calls map[string]int
}

func (w *scriptedWorker) Name() string { return "fake" }

func (w *scriptedWorker) Execute(_ context.Context, req worker.Request) (worker.Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	i := w.calls[req.StageID]
	w.calls[req.StageID]++
	results := w.seq[req.StageID]
	switch {
	case i < len(results):
		return results[i], nil
	case len(results) > 0:
		return results[len(results)-1], nil
	default:
		return worker.Result{Status: worker.StatusSucceeded}, nil
	}
}

func TestResumeFromFailedStage(t *testing.T) {
	eng, dir := newTestEngine(t, nil)
	scripted := &scriptedWorker{
		seq: map[string][]worker.Result{
			"impl": {
				{Status: worker.StatusFailed, Summary: "boom"},
				{Status: worker.StatusSucceeded, Summary: "ok"},
			},
			"done": {{Status: worker.StatusSucceeded, Summary: "done"}},
		},
		calls: map[string]int{},
	}
	eng.Registry.Register("fake", scripted)

	p := &Pipeline{ID: "t", Stages: []Stage{
		{ID: "impl", Retries: 0},
		{ID: "done", DependsOn: []string{"impl"}},
	}}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{TemplateID: "t", Pipeline: p, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	_ = eng.RunUntilBlockOrDone(ctx, runID)
	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("precondition: run=%s want failed", run.Status)
	}

	// Use a fresh Engine instance to exercise EnsurePipeline restoring the template.
	eng2 := NewEngine(eng.Store, eng.Registry, eng.Cfg)
	eng2.RetryBackoff = 0
	eng2.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }

	if err := eng2.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	run, _ = eng2.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("after resume run=%s want succeeded", run.Status)
	}
	sm := stageMap(t, eng2, runID)
	if sm["impl"].Status != store.StageStatusSucceeded {
		t.Fatalf("impl=%s want succeeded", sm["impl"].Status)
	}
	if sm["done"].Status != store.StageStatusSucceeded {
		t.Fatalf("done=%s want succeeded", sm["done"].Status)
	}
}

func TestResumeAfterReject(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
		"c": {Status: worker.StatusSucceeded, Summary: "c ok"},
	})
	// upstream/downstream stages use the default "fake" worker
	p := &Pipeline{
		ID: "gate-resume",
		Stages: []Stage{
			{ID: "a"},
			{ID: "gate", Worker: "manual", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"gate"}},
		},
	}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{TemplateID: "gate-resume", Pipeline: p, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	if err := eng.Reject(runID, "gate", "not ready"); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%s want failed", run.Status)
	}

	// Resume should move the run back to blocked and the gate to waiting approval.
	if err := eng.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	run, _ = eng.Store.GetRun(runID)
	if run.Status != store.RunStatusBlocked {
		t.Fatalf("after resume run=%s want blocked", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["gate"].Status != store.StageStatusWaitingApproval {
		t.Fatalf("gate=%s want waiting_approval", sm["gate"].Status)
	}

	// Approve and finish.
	if err := eng.Approve(runID, "gate", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}
	run, _ = eng.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("after approve run=%s want succeeded", run.Status)
	}
}

func TestResumeAfterCancelBlocked(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
	})
	p := &Pipeline{
		ID: "cancel-resume",
		Stages: []Stage{
			{ID: "a"},
			{ID: "gate", Worker: "manual", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"gate"}},
		},
	}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{TemplateID: "cancel-resume", Pipeline: p, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	if err := eng.Cancel(runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusCancelled {
		t.Fatalf("precondition run=%s want cancelled", run.Status)
	}

	// Resume should un-skip the gate and run should block again waiting for approval.
	if err := eng.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	run, _ = eng.Store.GetRun(runID)
	if run.Status != store.RunStatusBlocked {
		t.Fatalf("after resume run=%s want blocked", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["gate"].Status != store.StageStatusWaitingApproval {
		t.Fatalf("gate=%s want waiting_approval", sm["gate"].Status)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateTemplateDrift(t *testing.T) {
	eng, _ := newTestEngine(t, nil)
	pl := &Pipeline{Stages: []Stage{{ID: "intake"}, {ID: "implement"}}}
	// DB has an extra stage "open_pr" not in template
	stages := []store.StageRun{
		{StageID: "intake", Worker: "intake"},
		{StageID: "implement", Worker: "claude"},
		{StageID: "open_pr", Worker: "shell"},
	}
	err := eng.validateTemplate("a1b2c3d4", pl, stages)
	if err == nil || !strings.Contains(err.Error(), "open_pr") {
		t.Fatalf("want drift error naming open_pr, got %v", err)
	}
}

func TestArchiveStageArtifacts(t *testing.T) {
	runDir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(runDir, "logs"), 0o755))
	must(t, os.MkdirAll(filepath.Join(runDir, "artifacts", "implement"), 0o755))
	must(t, os.WriteFile(filepath.Join(runDir, "logs", "implement.log"), []byte("old log"), 0o644))
	must(t, os.WriteFile(filepath.Join(runDir, "artifacts", "implement", "out.txt"), []byte("old art"), 0o644))

	ts := time.Date(2026, 8, 26, 17, 0, 0, 0, time.UTC)
	if err := archiveStageArtifacts(runDir, []string{"implement", "missing"}, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "logs", "implement.log")); !os.IsNotExist(err) {
		t.Fatalf("live log should be moved")
	}
	arch := filepath.Join(runDir, ".recovery", recoveryStamp(ts), "implement", "implement.log")
	if b, err := os.ReadFile(arch); err != nil || string(b) != "old log" {
		t.Fatalf("archived log missing: %v", err)
	}
}

func TestValidateWorkersMissing(t *testing.T) {
	eng, _ := newTestEngine(t, nil) // registry has no "ghost" worker
	stages := []store.StageRun{
		{StageID: "intake", Worker: "intake"},
		{StageID: "implement", Worker: "ghost"},
	}
	err := eng.validateWorkers("a1b2c3d4", stages)
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("want worker error naming ghost, got %v", err)
	}
}

func TestRetryFromFailedSetsParent(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{"impl": {Status: worker.StatusSucceeded, Summary: "ok"}})
	p := &Pipeline{ID: "t", Stages: []Stage{{ID: "impl", Worker: "fake"}}}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }
	ctx := context.Background()
	parent, _ := eng.StartRun(ctx, StartParams{TemplateID: "t", Pipeline: p, CWD: dir, SpecContent: "do the thing"})
	_ = eng.RunUntilBlockOrDone(ctx, parent)
	// force parent to failed for the guard
	_ = eng.Store.UpdateRunStatus(parent, store.RunStatusFailed)

	res, err := eng.RetryRun(ctx, parent, RetryParams{Agent: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	child, _ := eng.Store.GetRun(res.RunID)
	if child.ParentRunID != parent {
		t.Fatalf("parent=%q want %q", child.ParentRunID, parent)
	}
	if child.Agent != "shell" {
		t.Fatalf("agent override not applied: %q", child.Agent)
	}
	if child.Status != store.RunStatusSucceeded {
		t.Fatalf("child run=%s want succeeded", child.Status)
	}
}

func TestRetryFromSucceededRequiresForce(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{"impl": {Status: worker.StatusSucceeded, Summary: "ok"}})
	p := &Pipeline{ID: "t", Stages: []Stage{{ID: "impl", Worker: "fake"}}}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }
	ctx := context.Background()
	parent, _ := eng.StartRun(ctx, StartParams{TemplateID: "t", Pipeline: p, CWD: dir, SpecContent: "x"})
	_ = eng.RunUntilBlockOrDone(ctx, parent) // succeeds

	if _, err := eng.RetryRun(ctx, parent, RetryParams{}); err == nil {
		t.Fatal("retry of succeeded run without --force should error")
	}
	res, err := eng.RetryRun(ctx, parent, RetryParams{Force: true})
	if err != nil {
		t.Fatalf("forced retry: %v", err)
	}
	if res.RunID == parent {
		t.Fatal("retry must create a new run id")
	}
}

func TestRetryPreflightUsesResolvedParentBranch(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{"impl": {Status: worker.StatusSucceeded, Summary: "ok"}})
	p := &Pipeline{ID: "t", Stages: []Stage{{ID: "impl", Worker: "fake"}}}
	eng.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }
	ctx := context.Background()

	parent, err := eng.StartRun(ctx, StartParams{TemplateID: "t", Pipeline: p, CWD: dir, SpecContent: "parent spec"})
	if err != nil {
		t.Fatal(err)
	}
	_ = eng.RunUntilBlockOrDone(ctx, parent)
	if err := eng.Store.UpdateRunStatus(parent, store.RunStatusFailed); err != nil {
		t.Fatal(err)
	}

	parentRun, err := eng.Store.GetRun(parent)
	if err != nil {
		t.Fatal(err)
	}
	wantBranch, err := issue.ResolveBranch(parentRun.IssueRef, parentRun.ID, parentRun.ParentRunID, func(id string) (string, string, string, error) {
		r, err := eng.Store.GetRun(id)
		if err != nil {
			return "", "", "", err
		}
		return r.IssueRef, r.ID, r.ParentRunID, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	origPreflight := gitPreflight
	t.Cleanup(func() { gitPreflight = origPreflight })
	var gotBranch string
	gitPreflight = func(cwd, branch string) []string {
		gotBranch = branch
		return nil
	}

	if _, err := eng.RetryRun(ctx, parent, RetryParams{}); err != nil {
		t.Fatal(err)
	}
	if gotBranch != wantBranch {
		t.Fatalf("preflight branch=%q want %q", gotBranch, wantBranch)
	}
}
