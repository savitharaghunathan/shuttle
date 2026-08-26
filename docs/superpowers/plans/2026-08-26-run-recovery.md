# Run Recovery Implementation Plan (Plan 2 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `shuttle resume <run-id>` (same run, reset from earliest failed/cancelled stage) and `shuttle retry <run-id>` (new run from frozen spec, optional agent/model override), plus the matching `POST /v1/runs/{id}/resume` and `/retry` endpoints.

**Architecture:** Engine gains `ResumeRun` and `RetryRun`. Resume validates template + workers, archives stale logs/artifacts to `.recovery/`, then resets stages in a single store transaction and continues. Retry snapshots the parent spec, runs a git preflight (go-git, warn-only), and starts a fresh run linked by `parent_run_id`. Recovery reuses Plan 1's metadata, `CurrentStage`, and `ResolveRunID`.

**Tech Stack:** Go, `modernc.org/sqlite`, cobra, chi, go-git (Apache-2.0) for git preflight.

**Spec:** [`docs/superpowers/specs/2026-08-26-run-recovery-design.md`](../specs/2026-08-26-run-recovery-design.md) (rev 3). **Depends on Plan 1** ([`2026-08-26-runs-view.md`](2026-08-26-runs-view.md)) being merged: uses `store.Run` metadata, `ResolveRunID`, `StartParams.IssueRef/ParentRunID`, and persisted resolved agent.

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Prefer MIT / Apache-2.0 OSS libraries; use go-git, never shell out to `git`
- Git commits: `git commit -s` plus body line `Assisted-by: Cursor <cursoragent@cursor.com>`. Do **not** add `Co-authored-by: Cursor`. Ask user for attribution before each commit.
- Stage ordering: always iterate `pl.Stages` (template order); join DB status via `stage_id → StageRun` map. Never rely on `GetStageRuns` row order.
- Shuttle never mutates the user's git repo (no stash/reset/checkout/branch-delete). Retry git checks are **warn-only**.
- Test doubles live in `internal/worker/workertest` (test-only package, excluded from the production binary). `workertest.NewFakeWorker(map[string]worker.Result)` is keyed by **stage id** and returns the same result each call; `newTestEngine` registers it as `"fake"` (the `DefaultWorker`). For fail-then-succeed behavior (resume tests) define a small stateful worker in `recovery_test.go` and register it as `"fake"` (see Task 4). Never reference an unregistered worker name in a dispatched stage — the registry only has `"fake"` plus built-ins (`shell`, `exec`, `intake`).

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/store/store.go` | `ResetStages(updates []StageRun) error` — transactional batch stage update |
| `internal/store/store_test.go` | `ResetStages` atomicity test |
| `internal/pipeline/recovery.go` (new) | `ResumeRun`, `RetryRun`, template + worker validation, `.recovery/` archive |
| `internal/pipeline/recovery_test.go` (new) | Resume reset, validation errors, archive-not-delete, retry lineage |
| `internal/pipeline/gitpreflight.go` (new) | go-git dirty-tree + non-default-branch checks |
| `internal/pipeline/gitpreflight_test.go` (new) | Preflight warnings on temp repos |
| `internal/cli/runs.go` | `resume`, `retry` commands (top-level) |
| `internal/cli/root.go` | Register `resume`, `retry` |
| `internal/api/server.go` | `POST /v1/runs/{id}/resume`, `/retry` (warnings[]) |
| `internal/api/server_test.go` | resume/retry endpoint tests |
| `README.md` | Document resume/retry, git-state caveats, frozen spec, async limitation |
| `docs/ROADMAP.md` | Move "Runs view + recovery" from Now → Shipped |

---

### Task 1: Store — transactional `ResetStages`

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Interfaces:**
- Produces: `func (s *Store) ResetStages(updates []StageRun) error` — applies all `UpdateStage`-equivalent writes in ONE transaction; all-or-nothing.

- [ ] **Step 1: Write failing test**

```go
func TestResetStagesAtomic(t *testing.T) {
	s := openStore(t)
	must(t, s.CreateRun(Run{ID: "r", TemplateID: "t", Status: RunStatusFailed, CWD: "/x", RunDir: "/x"}))
	must(t, s.CreateStageRuns([]StageRun{
		{ID: "s1", RunID: "r", StageID: "a", Worker: "w", Status: StageStatusFailed, Attempt: 3, Retries: 2},
		{ID: "s2", RunID: "r", StageID: "b", Worker: "w", Status: StageStatusSkipped},
	}))
	err := s.ResetStages([]StageRun{
		{ID: "s1", RunID: "r", StageID: "a", Worker: "w", Status: StageStatusReady, Attempt: 0, Retries: 2},
		{ID: "s2", RunID: "r", StageID: "b", Worker: "w", Status: StageStatusPending, Attempt: 0, Retries: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetStageRuns("r")
	m := map[string]StageRun{}
	for _, sr := range got {
		m[sr.StageID] = sr
	}
	if m["a"].Status != StageStatusReady || m["a"].Attempt != 0 {
		t.Fatalf("a=%+v", m["a"])
	}
	if m["b"].Status != StageStatusPending {
		t.Fatalf("b=%+v", m["b"])
	}
}
```

- [ ] **Step 2: Run — FAIL (undefined ResetStages)**

Run: `go test ./internal/store/ -run TestResetStagesAtomic -count=1` → FAIL.

- [ ] **Step 3: Implement**

```go
func (s *Store) ResetStages(updates []StageRun) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset stages: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.Prepare(`
UPDATE stage_runs SET status = ?, attempt = ?, retries = ?, timeout_ms = ?, summary = ?, log_path = ?
WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare reset stages: %w", err)
	}
	defer stmt.Close()
	for _, st := range updates {
		if _, err = stmt.Exec(string(st.Status), st.Attempt, st.Retries, st.TimeoutMS, st.Summary, st.LogPath, st.ID); err != nil {
			return fmt.Errorf("reset stage %s: %w", st.ID, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit reset stages: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/store/ -run TestResetStagesAtomic -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 2: Pipeline — template + worker validation

**Files:**
- Create: `internal/pipeline/recovery.go`
- Create: `internal/pipeline/recovery_test.go`

**Interfaces:**
- Produces:
```go
// validateTemplate ensures DB stages and template stages are a bijection.
func (e *Engine) validateTemplate(runID string, pl *Pipeline, stages []store.StageRun) error
// validateWorkers ensures each non-builtin persisted worker still resolves.
func (e *Engine) validateWorkers(runID string, stages []store.StageRun) error
```
- Consumes: `e.Registry.Get`, `e.TemplateLoader`
- Built-ins exempt from worker validation: `manual`, `shell`, `intake`.

- [ ] **Step 1: Write failing tests**

```go
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
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/pipeline/ -run 'TestValidateTemplateDrift|TestValidateWorkersMissing' -count=1` → FAIL.

- [ ] **Step 3: Implement in `recovery.go`**

```go
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

var builtinWorkers = map[string]bool{"manual": true, "shell": true, "intake": true}

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
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/pipeline/ -run 'TestValidateTemplateDrift|TestValidateWorkersMissing' -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 3: Pipeline — `.recovery/` archive helper

**Files:**
- Modify: `internal/pipeline/recovery.go`
- Modify: `internal/pipeline/recovery_test.go`

**Interfaces:**
- Produces: `func archiveStageArtifacts(runDir string, stageIDs []string, ts time.Time) error`
- Behavior: for each stage, move `logs/<stage>.log`, `artifacts/<stage>/`, and legacy file `artifacts/<stage>` into `.recovery/<UTC RFC3339>/<stage>/`. Missing sources are skipped (not an error). Rename within the run dir; fall back to copy+remove on cross-device error.

- [ ] **Step 1: Write failing test (archive, not delete)**

```go
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
	// original log/artifact gone from live paths
	if _, err := os.Stat(filepath.Join(runDir, "logs", "implement.log")); !os.IsNotExist(err) {
		t.Fatalf("live log should be moved")
	}
	// archived copy exists under .recovery/
	arch := filepath.Join(runDir, ".recovery", ts.Format(time.RFC3339), "implement", "implement.log")
	if b, err := os.ReadFile(arch); err != nil || string(b) != "old log" {
		t.Fatalf("archived log missing: %v", err)
	}
}
```

(Note: on case-insensitive/`:`-restricted filesystems RFC3339 has colons; use `ts.UTC().Format("20060102T150405Z")` instead to be safe. Update the test's `arch` path to match.)

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/pipeline/ -run TestArchiveStageArtifacts -count=1` → FAIL.

- [ ] **Step 3: Implement**

```go
func recoveryStamp(ts time.Time) string { return ts.UTC().Format("20060102T150405Z") }

func archiveStageArtifacts(runDir string, stageIDs []string, ts time.Time) error {
	base := filepath.Join(runDir, ".recovery", recoveryStamp(ts))
	for _, id := range stageIDs {
		dst := filepath.Join(base, id)
		sources := []string{
			filepath.Join(runDir, "logs", id+".log"),
			filepath.Join(runDir, "artifacts", id),      // dir or legacy file
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
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/pipeline/ -run TestArchiveStageArtifacts -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 4: Pipeline — `ResumeRun`

**Files:**
- Modify: `internal/pipeline/recovery.go`
- Modify: `internal/pipeline/recovery_test.go`

**Interfaces:**
- Produces: `func (e *Engine) ResumeRun(ctx context.Context, runID string) error`
- Order of operations: load run → guard status → `EnsurePipeline` → load stages → validateTemplate → validateWorkers → compute resume point (template order) → archive resume-point + downstream → `ResetStages` (one txn) → `UpdateRunStatus(running)` → `RunUntilBlockOrDone`.

- [ ] **Step 1: Add a stateful test worker (fail-then-succeed)**

`newTestEngine` registers a static, map-based `workertest.FakeWorker` under the name `"fake"` (the config's `DefaultWorker`), so it returns the *same* result for a stage every call. Resume needs a stage that fails once then succeeds, so define a small stateful worker in `recovery_test.go` and register it as `"fake"`, overriding the static one. Stages with no explicit `Worker` resolve to `"fake"` via `applyDefaults`.

```go
// recovery_test.go
type scriptedWorker struct {
	mu    sync.Mutex
	seq   map[string][]worker.Result // per stage id, one result per call
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
```

(Requires `sync` and `context` imports plus `worker` in `recovery_test.go`.)

- [ ] **Step 2: Write failing test (fail implement → resume → succeed)**

```go
func TestResumeFromFailedStage(t *testing.T) {
	eng, dir := newTestEngine(t, nil) // DefaultWorker "fake"; we override the fake below
	scripted := &scriptedWorker{
		seq: map[string][]worker.Result{
			"impl": {
				{Status: worker.StatusFailed, Summary: "boom"},   // attempt 1 fails
				{Status: worker.StatusSucceeded, Summary: "ok"},  // attempt 2 (after resume) succeeds
			},
			"done": {{Status: worker.StatusSucceeded, Summary: "done"}},
		},
		calls: map[string]int{},
	}
	eng.Registry.Register("fake", scripted)

	// Stages use the default "fake" worker (no explicit Worker set).
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

	if err := eng.ResumeRun(ctx, runID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	run, _ = eng.Store.GetRun(runID)
	if run.Status != store.RunStatusSucceeded {
		t.Fatalf("after resume run=%s want succeeded", run.Status)
	}
	sm := stageMap(t, eng, runID)
	if sm["impl"].Status != store.StageStatusSucceeded {
		t.Fatalf("impl=%s want succeeded", sm["impl"].Status)
	}
	if sm["done"].Status != store.StageStatusSucceeded {
		t.Fatalf("done=%s want succeeded", sm["done"].Status)
	}
}
```

To additionally exercise `EnsurePipeline` restore across a fresh process, build a second engine sharing the store before resume:

```go
	eng2 := NewEngine(eng.Store, eng.Registry, eng.Cfg)
	eng2.RetryBackoff = 0
	eng2.TemplateLoader = func(id string) (*Pipeline, error) { return p, nil }
	// call eng2.ResumeRun(ctx, runID) instead of eng.ResumeRun
```

- [ ] **Step 3: Run — FAIL (undefined ResumeRun)**

Run: `go test ./internal/pipeline/ -run TestResumeFromFailedStage -count=1` → FAIL.

- [ ] **Step 4: Implement `ResumeRun`**

```go
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
```

- [ ] **Step 5: Run — PASS**

Run: `go test ./internal/pipeline/ -run TestResumeFromFailedStage -count=1` → PASS.

- [ ] **Step 6: Add reject/cancel resume tests**

Both use a manual gate (`Worker: "manual"`), which the engine handles specially and `validateWorkers` skips as a built-in. No fake needed for the gate stage; upstream/downstream stages use the default `"fake"` worker.

```go
// Reject gate → run failed → resume → gate waits again → approve → succeed
func TestResumeAfterReject(t *testing.T) { /* build gate pipeline; Reject; ResumeRun; assert gate waiting_approval; Approve; succeed */ }
// Cancel while blocked → resume → gate waits again
func TestResumeAfterCancelBlocked(t *testing.T) { /* StartRun to gate; Cancel; ResumeRun; assert gate waiting_approval */ }
```

Run: `go test ./internal/pipeline/ -run 'TestResumeAfter' -count=1` → PASS.

- [ ] **Step 7: Commit** (ask attribution)

---

### Task 5: Pipeline — git preflight (go-git)

**Files:**
- Create: `internal/pipeline/gitpreflight.go`
- Create: `internal/pipeline/gitpreflight_test.go`

**Interfaces:**
- Produces: `func GitPreflight(cwd string) []string` — returns human-readable warnings; never errors (best-effort). Checks: (1) dirty working tree, (2) HEAD on non-default branch (`main`/`master`).

- [ ] **Step 1: Write failing tests using go-git temp repos**

```go
func TestGitPreflightDirty(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil { t.Fatal(err) }
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt") // staged but uncommitted → dirty
	warns := GitPreflight(dir)
	if len(warns) == 0 {
		t.Fatalf("want dirty warning")
	}
}

func TestGitPreflightCleanMain(t *testing.T) {
	dir := t.TempDir()
	repo, _ := gogit.PlainInit(dir, false)
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt")
	_, _ = wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	// default branch after init is "master" (go-git) — treated as default
	warns := GitPreflight(dir)
	if len(warns) != 0 {
		t.Fatalf("want no warnings on clean default branch, got %v", warns)
	}
}
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/pipeline/ -run 'TestGitPreflight' -count=1` → FAIL.

- [ ] **Step 3: Implement**

```go
package pipeline

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
)

func GitPreflight(cwd string) []string {
	var warns []string
	repo, err := gogit.PlainOpen(cwd)
	if err != nil {
		return nil // not a git repo (spec-only run) → no warnings
	}
	if wt, err := repo.Worktree(); err == nil {
		if st, err := wt.Status(); err == nil && !st.IsClean() {
			warns = append(warns, fmt.Sprintf("uncommitted changes in %s", cwd))
		}
	}
	if head, err := repo.Head(); err == nil && head.Name().IsBranch() {
		branch := head.Name().Short()
		if branch != "main" && branch != "master" {
			warns = append(warns, fmt.Sprintf("on non-default branch %s", branch))
		}
	}
	return warns
}
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/pipeline/ -run 'TestGitPreflight' -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 6: Pipeline — `RetryRun`

**Files:**
- Modify: `internal/pipeline/recovery.go`
- Modify: `internal/pipeline/recovery_test.go`

**Interfaces:**
- Produces:
```go
type RetryParams struct{ Agent, Model string; Force bool }
type RetryResult struct{ RunID string; Warnings []string }
func (e *Engine) RetryRun(ctx context.Context, runID string, p RetryParams) (RetryResult, error)
```
- Order: load parent → guard status (`failed`/`cancelled`, or `succeeded` only with `Force`) → require `inputs/spec.md` → validate workers (parent's or overridden agent) → git preflight (warn) → `StartRun` with copied spec, inherited template/cwd/issue, overrides applied, `ParentRunID` set → `RunUntilBlockOrDone` → return new id + warnings.

- [ ] **Step 1: Write failing tests (lineage + guards)**

The `impl` stage leaves `Worker` unset so it resolves to the default `"fake"` worker (registered by `newTestEngine`); results are keyed by **stage id**. The agent override uses `"shell"` — a real, always-registered built-in — so `RetryRun`'s worker-existence check passes. Because the stage's `Worker` is explicitly `"fake"`, the override changes only the persisted `runs.agent`, not dispatch (so the child run still succeeds via the fake).

```go
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
```

- [ ] **Step 2: Run — FAIL**

Run: `go test ./internal/pipeline/ -run 'TestRetry' -count=1` → FAIL.

- [ ] **Step 3: Implement**

```go
func (e *Engine) RetryRun(ctx context.Context, runID string, p RetryParams) (RetryResult, error) {
	parent, err := e.Store.GetRun(runID)
	if err != nil {
		return RetryResult{}, err
	}
	switch parent.Status {
	case store.RunStatusFailed, store.RunStatusCancelled:
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

	warnings := GitPreflight(parent.CWD)

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
	if err := e.RunUntilBlockOrDone(ctx, newID); err != nil {
		return RetryResult{RunID: newID, Warnings: warnings}, err
	}
	return RetryResult{RunID: newID, Warnings: warnings}, nil
}
```

Note: `StartRun` copies the spec via `SpecPath` (existing `artifact.CopySpec`), giving the new run its own `inputs/spec.md`. Worker validation for retry is implicit — `StartRun` → `applyDefaults(agent)` → dispatch will fail on unknown worker; add an explicit `validateWorkers`-style check on the resolved `agent` before `StartRun` for a clearer error:

```go
	if !builtinWorkers[agent] && agent != "" {
		if _, ok := e.Registry.Get(agent); !ok {
			return RetryResult{}, fmt.Errorf("worker %q not configured; retry with --agent <name>", agent)
		}
	}
```

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/pipeline/ -run 'TestRetry' -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 7: CLI — `resume` and `retry`

**Files:**
- Modify: `internal/cli/runs.go`
- Modify: `internal/cli/root.go` (register both)

**Interfaces:**
- `newResumeCommand(out)`: `shuttle resume <run-id>`; resolves prefix; calls `engine.ResumeRun`; prints run summary via `printRun`.
- `newRetryCommand(out)`: `shuttle retry <run-id> [--agent] [--model] [--force]`; resolves prefix; calls `engine.RetryRun`; prints new run id + warnings to stderr.

- [ ] **Step 1: Implement commands**

```go
func newResumeCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "resume <run-id>",
		Short: "Resume a failed or cancelled run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := app.store.ResolveRunID(args[0])
			if err != nil {
				return err
			}
			app.engine.Progress = out
			if err := app.engine.ResumeRun(cmd.Context(), runID); err != nil {
				return err
			}
			return printRun(out, app.store, runID, true, true)
		},
	}
}

func newRetryCommand(out, errOut io.Writer) *cobra.Command {
	var agent, model string
	var force bool
	cmd := &cobra.Command{
		Use:   "retry <run-id>",
		Short: "Start a new run from a terminal run's spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := openApp()
			if err != nil {
				return err
			}
			defer app.close()
			runID, err := app.store.ResolveRunID(args[0])
			if err != nil {
				return err
			}
			app.engine.Progress = out
			res, err := app.engine.RetryRun(cmd.Context(), runID, pipeline.RetryParams{Agent: agent, Model: model, Force: force})
			if err != nil {
				return err
			}
			for _, wmsg := range res.Warnings {
				fmt.Fprintf(errOut, "warning: %s\n", wmsg)
			}
			fmt.Fprintf(out, "retry: %s (parent %s)\n", res.RunID, shortID(runID))
			return printRun(out, app.store, res.RunID, true, true)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "override agent for the new run")
	cmd.Flags().StringVar(&model, "model", "", "override model for the new run")
	cmd.Flags().BoolVar(&force, "force", false, "allow retry of a succeeded run")
	return cmd
}
```

Register in `root.go`: pass `errOut` where the root command already has it (the root builds with `out, errOut`). Add `newResumeCommand(out)` and `newRetryCommand(out, errOut)` to `AddCommand`.

- [ ] **Step 2: Build + manual smoke**

```bash
go build -o bin/shuttle ./cmd/shuttle
./bin/shuttle resume <failed-run-id>
./bin/shuttle retry <run-id> --agent claude
```

- [ ] **Step 3: `go test ./... -count=1`** → PASS.

- [ ] **Step 4: Commit** (ask attribution)

---

### Task 8: API — resume/retry endpoints

**Files:**
- Modify: `internal/api/server.go`
- Modify/Create: `internal/api/server_test.go`

**Interfaces:**
- `POST /v1/runs/{id}/resume` → resolves id, calls `ResumeRun`, returns run JSON. Errors: 404 not found, 409 invalid state / drift / stale worker.
- `POST /v1/runs/{id}/retry` → optional body `{agent, model, force}`; returns `{ id, parent_run_id, status, warnings[] }`.

- [ ] **Step 1: Write failing endpoint tests**

```go
func TestResumeEndpoint(t *testing.T) {
	// seed a failed run whose single stage is failed; POST resume; expect 200 and status advanced
}
func TestRetryEndpointWarnings(t *testing.T) {
	// seed failed run with dirty cwd git repo; POST retry; expect warnings[] non-empty
}
```

- [ ] **Step 2: Run — FAIL (no route)**

Run: `go test ./internal/api/ -run 'TestResumeEndpoint|TestRetryEndpointWarnings' -count=1` → FAIL.

- [ ] **Step 3: Implement**

Routes:

```go
	r.Post("/v1/runs/{id}/resume", s.resumeRun)
	r.Post("/v1/runs/{id}/retry", s.retryRun)
```

Handlers:

```go
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

type retryRequest struct {
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
	Force bool   `json:"force,omitempty"`
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            res.RunID,
		"parent_run_id": run.ParentRunID,
		"status":        string(run.Status),
		"warnings":      res.Warnings,
	})
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
```

Add imports `strings` (if not already present from Plan 1) and ensure `pipeline` is imported.

- [ ] **Step 4: Run — PASS**

Run: `go test ./internal/api/ -count=1` → PASS.

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 9: Docs + ROADMAP + full verification

**Files:**
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

- [ ] **Step 1: README — resume/retry section**

Document:

```bash
shuttle resume <run-id>                 # continue a failed/cancelled run
shuttle retry <run-id> --agent claude   # new run, same spec, different agent
shuttle retry <run-id> --force          # retry a succeeded run (sharp edge)
```

Include caveats verbatim from spec:
- Resume is for terminal `failed`/`cancelled` only; use `approve`/`reject` for `blocked`.
- **Do not edit pipeline YAML for in-flight runs** — template drift blocks resume.
- Retry uses a **frozen spec** (`inputs/spec.md`); it does not re-fetch the issue — use `run --issue` for that.
- Retry does **not** clean git; inspect `git status` / open PRs first. Warnings are advisory.
- API resume/retry POSTs **block** for the full run; prefer the CLI for long recovery runs, or set a 30+ min client timeout.
- Stale logs/artifacts for reset stages are archived under `<run-dir>/.recovery/` (not deleted; not auto-pruned).

- [ ] **Step 2: ROADMAP — mark shipped**

Move the **Runs view + recovery** block from **Now** to **Shipped**, phrased as a capability:

```markdown
### Runs view + recovery
List and compare runs (agent, status, time, issue, stage) via `shuttle runs list` and `GET /v1/runs`. Resume failed/cancelled runs from the earliest failed stage; retry any terminal run against its frozen spec with optional agent/model overrides. Run metadata persists across sessions.
```

Leave **Local web dashboard** under Future.

- [ ] **Step 3: Full verification**

Run: `make verify && make test-integration`
Expected: all green (build, unit, integration).

- [ ] **Step 4: Commit** (ask attribution)

---

## Execution order

1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9

Store batch (1) and validation/archive (2–3) precede `ResumeRun` (4). Preflight (5) precedes `RetryRun` (6). CLI (7) and API (8) are surfaces. Docs + verification (9) last.

## Self-review notes

- **Spec coverage:** resume reset rules (Task 4), attempt reset to 0 (Task 4 updates), archive-not-delete (Task 3), template drift (Task 2), stale worker (Task 2/6), git preflight scope = dirty + non-default branch (Task 5), `--force` for succeeded (Task 6), `parent_run_id` lineage (Task 6), warnings surfaced CLI+API (Tasks 7–8), `.recovery/` retention documented (Task 9), frozen-spec + async caveats (Task 9). All mapped.
- **Type consistency:** `ResumeRun(ctx, id) error`, `RetryRun(ctx, id, RetryParams) (RetryResult, error)`, `RetryResult{RunID, Warnings}`, `ResetStages([]store.StageRun) error`, `CurrentStage` (Plan 1), `ResolveRunID` (Plan 1) used consistently across tasks.
- **Dependency on Plan 1:** every task assumes Plan 1 merged (metadata columns, `ResolveRunID`, `StartParams.IssueRef/ParentRunID`, resolved-agent persistence).
