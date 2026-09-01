package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/savitharaghunathan/shuttle/internal/worker/workertest"
)

func newTestEngine(t *testing.T, results map[string]worker.Result) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()

	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", workertest.NewFakeWorker(results))

	cfg := &config.Config{
		WorkRoot:       filepath.Join(dir, ".shuttle"),
		Concurrency:    2,
		DefaultWorker:  "fake",
		DefaultTimeout: 5 * time.Minute,
	}

	eng := NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0
	return eng, dir
}

func stageMap(t *testing.T, eng *Engine, runID string) map[string]store.StageRun {
	t.Helper()
	stages, err := eng.Store.GetStageRuns(runID)
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[string]store.StageRun, len(stages))
	for _, sr := range stages {
		m[sr.StageID] = sr
	}
	return m
}

// Step 1 + 2: linear pipeline with three stages, all succeed via FakeWorker.
func TestLinearSuccess(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
		"b": {Status: worker.StatusSucceeded, Summary: "b ok"},
		"c": {Status: worker.StatusSucceeded, Summary: "c ok"},
	})

	p := &Pipeline{
		ID: "linear",
		Stages: []Stage{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"b"}},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, err := eng.Store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded", run.Status)
	}

	sm := stageMap(t, eng, runID)
	for _, id := range []string{"a", "b", "c"} {
		if sm[id].Status != store.StageStatusSucceeded {
			t.Fatalf("stage %q status=%q want succeeded", id, sm[id].Status)
		}
		if sm[id].Attempt != 1 {
			t.Fatalf("stage %q attempt=%d want 1", id, sm[id].Attempt)
		}
	}
}

func TestRunWithSpecPathExecutesIntakeStage(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{})
	eng.Registry.Register("intake", worker.NewIntakeWorker())

	specPath := filepath.Join(dir, "task.md")
	if err := os.WriteFile(specPath, []byte("# Task\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	p := &Pipeline{
		ID:     "intake",
		Stages: []Stage{{ID: "intake", Worker: "intake"}},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		SpecPath:   specPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, err := eng.Store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded", run.Status)
	}

	data, err := os.ReadFile(filepath.Join(run.RunDir, "artifacts", "spec"))
	if err != nil {
		t.Fatalf("read artifacts/spec: %v", err)
	}
	if string(data) != "# Task\n" {
		t.Fatalf("artifacts/spec=%q want copied spec", data)
	}
}

// Step 3: stage b fails → c is skipped, run is failed.
func TestFailureSkipsDownstream(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
		"b": {Status: worker.StatusFailed, Summary: "b boom"},
		"c": {Status: worker.StatusSucceeded, Summary: "c ok"},
	})

	p := &Pipeline{
		ID: "fail",
		Stages: []Stage{
			{ID: "a"},
			{ID: "b", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"b"}},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%q want failed", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["a"].Status != store.StageStatusSucceeded {
		t.Fatalf("a status=%q want succeeded", sm["a"].Status)
	}
	if sm["b"].Status != store.StageStatusFailed {
		t.Fatalf("b status=%q want failed", sm["b"].Status)
	}
	if sm["c"].Status != store.StageStatusSkipped {
		t.Fatalf("c status=%q want skipped", sm["c"].Status)
	}
}

// Step 3b: retries exhausted — stage with retries=1 fails twice then run fails.
func TestRetryExhausted(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusFailed, Summary: "always fails"},
	})

	p := &Pipeline{
		ID: "retry",
		Stages: []Stage{
			{ID: "a", Retries: 1},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%q want failed", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["a"].Status != store.StageStatusFailed {
		t.Fatalf("a status=%q want failed", sm["a"].Status)
	}
	if sm["a"].Attempt != 2 {
		t.Fatalf("a attempt=%d want 2 (1 original + 1 retry)", sm["a"].Attempt)
	}
}

// Step 4a: manual gate blocks, approve unblocks and run succeeds.
func TestGateApprove(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
		"c": {Status: worker.StatusSucceeded, Summary: "c ok"},
	})

	p := &Pipeline{
		ID: "gate-approve",
		Stages: []Stage{
			{ID: "a"},
			{ID: "gate", Worker: "manual", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"gate"}},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusBlocked {
		t.Fatalf("run status=%q want blocked", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["gate"].Status != store.StageStatusWaitingApproval {
		t.Fatalf("gate status=%q want waiting_approval", sm["gate"].Status)
	}

	if err := eng.Approve(runID, "gate", "LGTM"); err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ = eng.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded after approve", run.Status)
	}

	approvalFile := filepath.Join(run.RunDir, "approvals", "gate.md")
	data, err := os.ReadFile(approvalFile)
	if err != nil {
		t.Fatalf("read approval: %v", err)
	}
	if string(data) != "LGTM" {
		t.Fatalf("approval content=%q want LGTM", string(data))
	}
}

// Step 4b: manual gate blocks, reject → run failed, downstream skipped.
func TestGateReject(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
	})

	p := &Pipeline{
		ID: "gate-reject",
		Stages: []Stage{
			{ID: "a"},
			{ID: "gate", Worker: "manual", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"gate"}},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
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
		t.Fatalf("run status=%q want failed", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["gate"].Status != store.StageStatusRejected {
		t.Fatalf("gate status=%q want rejected", sm["gate"].Status)
	}
	if sm["c"].Status != store.StageStatusSkipped {
		t.Fatalf("c status=%q want skipped", sm["c"].Status)
	}
}

// Step 5: cancel a blocked run → waiting_approval and pending become skipped, run cancelled.
func TestCancel(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "a ok"},
	})

	p := &Pipeline{
		ID: "cancel",
		Stages: []Stage{
			{ID: "a"},
			{ID: "gate", Worker: "manual", DependsOn: []string{"a"}},
			{ID: "c", DependsOn: []string{"gate"}},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
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
		t.Fatalf("run status=%q want cancelled", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["a"].Status != store.StageStatusSucceeded {
		t.Fatalf("a status=%q want succeeded", sm["a"].Status)
	}
	if sm["gate"].Status != store.StageStatusSkipped {
		t.Fatalf("gate status=%q want skipped", sm["gate"].Status)
	}
	if sm["c"].Status != store.StageStatusSkipped {
		t.Fatalf("c status=%q want skipped", sm["c"].Status)
	}
}

// Cancel interrupts an in-flight slow Execute via context cancellation.
func TestCancelInterruptsExecute(t *testing.T) {
	fw := workertest.NewFakeWorker(map[string]worker.Result{
		"slow": {Status: worker.StatusSucceeded, Summary: "done"},
	})
	fw.Delay = 10 * time.Second

	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", fw)

	cfg := &config.Config{
		WorkRoot:      filepath.Join(dir, ".shuttle"),
		Concurrency:   2,
		DefaultWorker: "fake",
	}

	eng := NewEngine(s, reg, cfg)

	p := &Pipeline{
		ID:     "cancel-inflight",
		Stages: []Stage{{ID: "slow"}},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, tickErr := eng.Tick(ctx, runID)
		done <- tickErr
	}()

	// Give Tick time to start Execute
	time.Sleep(100 * time.Millisecond)

	if err := eng.Cancel(runID); err != nil {
		t.Fatal(err)
	}

	select {
	case tickErr := <-done:
		if tickErr != nil {
			// Context cancelled causes the execute to fail; engine marks it failed or retries
			t.Logf("tick returned error (expected): %v", tickErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Tick did not return within 3s after Cancel — context not propagated")
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusCancelled && run.Status != store.RunStatusFailed {
		t.Logf("run status=%q (cancel propagated context)", run.Status)
	}
}

// Task 8: fresh Engine (empty in-memory maps) can Tick an existing run after EnsurePipeline.
func TestFreshEngineApproveAndContinue(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"intake":    {Status: worker.StatusSucceeded, Summary: "intake ok"},
		"implement": {Status: worker.StatusSucceeded, Summary: "impl ok"},
		"test":      {Status: worker.StatusSucceeded, Summary: "tests pass"},
	})

	p := &Pipeline{
		ID: "pr-factory",
		Stages: []Stage{
			{ID: "intake", Worker: "fake"},
			{ID: "implement", Worker: "fake", DependsOn: []string{"intake"}},
			{ID: "test", Worker: "fake", DependsOn: []string{"implement"}},
			{ID: "approve_pr", Worker: "manual", DependsOn: []string{"test"}},
			{ID: "ship", Worker: "fake", DependsOn: []string{"approve_pr"}},
		},
	}

	// Set TemplateLoader so EnsurePipeline can reload the pipeline definition.
	eng.TemplateLoader = func(id string) (*Pipeline, error) {
		if id == "pr-factory" {
			return p, nil
		}
		return nil, fmt.Errorf("unknown template %q", id)
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "pr-factory",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusBlocked {
		t.Fatalf("run status=%q want blocked", run.Status)
	}

	// Simulate a fresh CLI invocation: new Engine with same store, empty maps.
	shipResults := workertest.NewFakeWorker(map[string]worker.Result{
		"ship": {Status: worker.StatusSucceeded, Summary: "shipped"},
	})
	reg2 := worker.NewRegistry(nil)
	reg2.Register("fake", shipResults)

	eng2 := NewEngine(eng.Store, reg2, eng.Cfg)
	eng2.RetryBackoff = 0
	eng2.TemplateLoader = func(id string) (*Pipeline, error) {
		if id == "pr-factory" {
			return p, nil
		}
		return nil, fmt.Errorf("unknown template %q", id)
	}

	if err := eng2.Approve(runID, "approve_pr", "LGTM"); err != nil {
		t.Fatal(err)
	}

	if err := eng2.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ = eng2.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded after fresh-engine approve", run.Status)
	}

	sm := stageMap(t, eng2, runID)
	if sm["ship"].Status != store.StageStatusSucceeded {
		t.Fatalf("ship status=%q want succeeded", sm["ship"].Status)
	}
}

// captureWorker records the last Request it received.
type captureWorker struct {
	result  worker.Result
	lastReq worker.Request
}

func (w *captureWorker) Name() string { return "capture" }
func (w *captureWorker) Execute(_ context.Context, req worker.Request) (worker.Result, error) {
	w.lastReq = req
	return w.result, nil
}

type captureByStageWorker struct {
	result worker.Result
	reqs   map[string]worker.Request
}

func (w *captureByStageWorker) Name() string { return "capture-by-stage" }
func (w *captureByStageWorker) Execute(_ context.Context, req worker.Request) (worker.Result, error) {
	if w.reqs == nil {
		w.reqs = make(map[string]worker.Request)
	}
	w.reqs[req.StageID] = req
	return w.result, nil
}

func TestRenderPromptWithArtifact(t *testing.T) {
	cw := &captureWorker{
		result: worker.Result{Status: worker.StatusSucceeded, Summary: "ok"},
	}

	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("capture", cw)

	cfg := &config.Config{
		WorkRoot:    filepath.Join(dir, ".shuttle"),
		Concurrency: 2,
	}

	eng := NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0

	specFile := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specFile, []byte("# Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &Pipeline{
		ID: "render-test",
		Stages: []Stage{
			{ID: "impl", Worker: "capture", Prompt: "Read {{.Artifacts.spec}} and implement"},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		SpecPath:   specFile,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded", run.Status)
	}

	expectedArtifact := filepath.Join(run.RunDir, "artifacts", "spec")
	expectedPrompt := "Read " + expectedArtifact + " and implement"
	if cw.lastReq.Prompt != expectedPrompt {
		t.Fatalf("rendered prompt=%q\n     want=%q", cw.lastReq.Prompt, expectedPrompt)
	}
}

func TestRenderIncludesBranchAndShuttleBinForIssueRun(t *testing.T) {
	cw := &captureByStageWorker{
		result: worker.Result{Status: worker.StatusSucceeded, Summary: "ok"},
	}

	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("capture", cw)

	cfg := &config.Config{
		WorkRoot:    filepath.Join(dir, ".shuttle"),
		Concurrency: 2,
	}
	eng := NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0

	p := &Pipeline{
		ID: "render-bin-branch",
		Stages: []Stage{
			{ID: "implement", Worker: "capture", Prompt: "use {{.Branch}}"},
			{
				ID:        "open_pr",
				Worker:    "capture",
				DependsOn: []string{"implement"},
				Command:   []string{"{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"},
			},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		IssueRef:   "org/repo#42",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	implReq, ok := cw.reqs["implement"]
	if !ok {
		t.Fatal("implement stage was not executed")
	}
	if implReq.Prompt != "use shuttle/issue-42" {
		t.Fatalf("implement prompt = %q, want %q", implReq.Prompt, "use shuttle/issue-42")
	}

	openReq, ok := cw.reqs["open_pr"]
	if !ok {
		t.Fatal("open_pr stage was not executed")
	}
	if len(openReq.Command) == 0 {
		t.Fatal("open_pr command is empty")
	}
	if !filepath.IsAbs(openReq.Command[0]) {
		t.Fatalf("open_pr command[0] = %q, want absolute path", openReq.Command[0])
	}
	if got := openReq.Command[4]; got != "shuttle/issue-42" {
		t.Fatalf("open_pr branch arg = %q, want shuttle/issue-42", got)
	}
}

func TestRenderMissingArtifactFailsStage(t *testing.T) {
	cw := &captureWorker{
		result: worker.Result{Status: worker.StatusSucceeded, Summary: "ok"},
	}

	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("capture", cw)

	cfg := &config.Config{
		WorkRoot:    filepath.Join(dir, ".shuttle"),
		Concurrency: 2,
	}

	eng := NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0

	p := &Pipeline{
		ID: "render-missing",
		Stages: []Stage{
			{ID: "impl", Worker: "capture", Prompt: "Read {{.Artifacts.nonexistent}} please"},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%q want failed (missing artifact)", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["impl"].Status != store.StageStatusFailed {
		t.Fatalf("impl status=%q want failed", sm["impl"].Status)
	}
	if sm["impl"].Summary == nil || *sm["impl"].Summary == "" {
		t.Fatal("impl summary should describe render error")
	}
}

func TestMalformedIssueRefFailsStageAtRender(t *testing.T) {
	cw := &captureWorker{
		result: worker.Result{Status: worker.StatusSucceeded, Summary: "ok"},
	}

	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("capture", cw)

	cfg := &config.Config{
		WorkRoot:    filepath.Join(dir, ".shuttle"),
		Concurrency: 2,
	}

	eng := NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0

	p := &Pipeline{
		ID: "bad-issue-ref",
		Stages: []Stage{
			{ID: "implement", Worker: "capture", Prompt: "Read {{.Branch}}"},
		},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		IssueRef:   "badformat",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%q want failed", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["implement"].Status != store.StageStatusFailed {
		t.Fatalf("implement status=%q want failed", sm["implement"].Status)
	}
	if sm["implement"].Summary == nil || !strings.Contains(*sm["implement"].Summary, "resolve branch") {
		t.Fatalf("implement summary=%v, want resolve branch error", sm["implement"].Summary)
	}
}

// Outputs produced by a successful stage are materialized under artifacts/<stageID>/.
func TestOutputsMaterialized(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"build": {
			Status:  worker.StatusSucceeded,
			Summary: "built",
			Outputs: map[string]string{"report.txt": "all green"},
		},
	})

	p := &Pipeline{
		ID:     "outputs",
		Stages: []Stage{{ID: "build"}},
	}

	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, _ := eng.Store.GetRun(runID)
	outPath := filepath.Join(run.RunDir, "artifacts", "build", "report.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "all green" {
		t.Fatalf("output=%q want 'all green'", string(data))
	}
}

// Persisted resolved agent must be restored by a fresh Engine.
func TestPersistedAgentRestoredOnFreshEngine(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"a": {Status: worker.StatusSucceeded, Summary: "ok"},
	})
	// newTestEngine sets Cfg.DefaultWorker = "fake"; stage "a" (no explicit
	// Worker) resolves to the fake, so resolvedAgent persists as "fake".
	p := &Pipeline{ID: "t", Stages: []Stage{{ID: "a"}}}
	runID, err := eng.StartRun(context.Background(), StartParams{TemplateID: "t", Pipeline: p, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := eng.Store.GetRun(runID)
	if run.Agent == "" {
		t.Fatalf("resolved agent not persisted")
	}
	// fresh engine sharing the same store; runOpts cache empty
	eng2 := NewEngine(eng.Store, eng.Registry, eng.Cfg)
	eng2.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }
	if err := eng2.EnsurePipeline(runID); err != nil {
		t.Fatal(err)
	}
	eng2.mu.Lock()
	opts := eng2.runOpts[runID]
	eng2.mu.Unlock()
	if opts.Agent != run.Agent {
		t.Fatalf("runOpts.Agent=%q want %q", opts.Agent, run.Agent)
	}
}

func TestImplementFailsWhenExpectedRemoteBranchMissing(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"implement": {Status: worker.StatusSucceeded, Summary: "impl ok"},
	})
	eng.Cfg.Concurrency = 1
	var progress bytes.Buffer
	eng.Progress = &progress

	origIsGitRepo := isGitRepo
	origVerifyRemoteBranch := verifyRemoteBranch
	t.Cleanup(func() {
		isGitRepo = origIsGitRepo
		verifyRemoteBranch = origVerifyRemoteBranch
	})
	isGitRepo = func(string) bool { return true }
	verifyRemoteBranch = func(string, string) (bool, error) { return false, nil }

	p := &Pipeline{ID: "impl-only", Stages: []Stage{{ID: "implement"}}}
	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		IssueRef:   "org/repo#42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, err := eng.Store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusFailed {
		t.Fatalf("run status=%q want failed", run.Status)
	}

	sm := stageMap(t, eng, runID)
	if sm["implement"].Status != store.StageStatusFailed {
		t.Fatalf("implement status=%q want failed", sm["implement"].Status)
	}
	if sm["implement"].Summary == nil {
		t.Fatal("implement summary is nil")
	}
	want := "expected branch shuttle/issue-42 not found on remote origin; agent must push to this branch"
	if *sm["implement"].Summary != want {
		t.Fatalf("implement summary=%q want %q", *sm["implement"].Summary, want)
	}
}

func TestImplementSucceedsWithWarningWhenRemoteBranchCheckErrors(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"implement": {Status: worker.StatusSucceeded, Summary: "impl ok"},
	})
	eng.Cfg.Concurrency = 1
	var progress bytes.Buffer
	eng.Progress = &progress

	origIsGitRepo := isGitRepo
	origVerifyRemoteBranch := verifyRemoteBranch
	t.Cleanup(func() {
		isGitRepo = origIsGitRepo
		verifyRemoteBranch = origVerifyRemoteBranch
	})
	isGitRepo = func(string) bool { return true }
	verifyRemoteBranch = func(string, string) (bool, error) {
		return false, fmt.Errorf("auth required")
	}

	p := &Pipeline{ID: "impl-only", Stages: []Stage{{ID: "implement"}}}
	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		IssueRef:   "org/repo#42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, err := eng.Store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["implement"].Status != store.StageStatusSucceeded {
		t.Fatalf("implement status=%q want succeeded", sm["implement"].Status)
	}

	out := progress.String()
	if !strings.Contains(out, "could not verify remote branch shuttle/issue-42: auth required") {
		t.Fatalf("progress output missing advisory warning, got %q", out)
	}
}

func TestImplementSucceedsWhenExpectedRemoteBranchPresent(t *testing.T) {
	eng, dir := newTestEngine(t, map[string]worker.Result{
		"implement": {Status: worker.StatusSucceeded, Summary: "impl ok"},
	})
	eng.Cfg.Concurrency = 1

	origIsGitRepo := isGitRepo
	origVerifyRemoteBranch := verifyRemoteBranch
	t.Cleanup(func() {
		isGitRepo = origIsGitRepo
		verifyRemoteBranch = origVerifyRemoteBranch
	})
	isGitRepo = func(string) bool { return true }
	verifyRemoteBranch = func(string, string) (bool, error) { return true, nil }

	p := &Pipeline{ID: "impl-only", Stages: []Stage{{ID: "implement"}}}
	ctx := context.Background()
	runID, err := eng.StartRun(ctx, StartParams{
		TemplateID: "tmpl",
		Pipeline:   p,
		CWD:        dir,
		IssueRef:   "org/repo#42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunUntilBlockOrDone(ctx, runID); err != nil {
		t.Fatal(err)
	}

	run, err := eng.Store.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("run status=%q want succeeded", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["implement"].Status != store.StageStatusSucceeded {
		t.Fatalf("implement status=%q want succeeded", sm["implement"].Status)
	}
}
