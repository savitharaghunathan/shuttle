package store_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

func TestCreateAndGetRun(t *testing.T) {
	s := openTempStore(t)

	run := store.Run{
		ID:         "run-1",
		TemplateID: "template-1",
		Status:     store.RunStatusPending,
		CWD:        "/repo",
		RunDir:     "/repo/.shuttle/runs/run-1",
	}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetRun("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID {
		t.Fatalf("ID=%q want %q", got.ID, run.ID)
	}
	if got.TemplateID != run.TemplateID {
		t.Fatalf("TemplateID=%q want %q", got.TemplateID, run.TemplateID)
	}
	if got.Status != store.RunStatusPending {
		t.Fatalf("Status=%q want %q", got.Status, store.RunStatusPending)
	}
	if got.CWD != run.CWD {
		t.Fatalf("CWD=%q want %q", got.CWD, run.CWD)
	}
	if got.RunDir != run.RunDir {
		t.Fatalf("RunDir=%q want %q", got.RunDir, run.RunDir)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt is zero")
	}
}

func TestStageRunsInsertUpdateAndListWaitingApproval(t *testing.T) {
	s := openTempStore(t)
	createRun(t, s, "run-1")

	timeoutMS := int64(30000)
	stages := []store.StageRun{
		{
			ID:        "stage-run-1",
			RunID:     "run-1",
			StageID:   "test",
			Worker:    "shell",
			Status:    store.StageStatusReady,
			Attempt:   1,
			Retries:   2,
			TimeoutMS: &timeoutMS,
		},
		{
			ID:      "stage-run-2",
			RunID:   "run-1",
			StageID: "open_pr",
			Worker:  "cursor",
			Status:  store.StageStatusPending,
		},
	}
	if err := s.CreateStageRuns(stages); err != nil {
		t.Fatal(err)
	}

	summary := "needs approval"
	logPath := "/repo/.shuttle/runs/run-1/open_pr.log"
	if err := s.UpdateStage(store.StageRun{
		ID:      "stage-run-2",
		Status:  store.StageStatusWaitingApproval,
		Attempt: 1,
		Summary: &summary,
		LogPath: &logPath,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRunStatus("run-1", store.RunStatusBlocked); err != nil {
		t.Fatal(err)
	}

	run, err := s.GetRun("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStatusBlocked {
		t.Fatalf("run Status=%q want %q", run.Status, store.RunStatusBlocked)
	}

	gotStages, err := s.GetStageRuns("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotStages) != 2 {
		t.Fatalf("len(GetStageRuns)=%d want 2", len(gotStages))
	}
	if gotStages[0].StageID != "test" {
		t.Fatalf("first StageID=%q want test", gotStages[0].StageID)
	}
	if gotStages[0].TimeoutMS == nil || *gotStages[0].TimeoutMS != timeoutMS {
		t.Fatalf("TimeoutMS=%v want %d", gotStages[0].TimeoutMS, timeoutMS)
	}
	if gotStages[1].Status != store.StageStatusWaitingApproval {
		t.Fatalf("updated Status=%q want %q", gotStages[1].Status, store.StageStatusWaitingApproval)
	}
	if gotStages[1].Summary == nil || *gotStages[1].Summary != summary {
		t.Fatalf("Summary=%v want %q", gotStages[1].Summary, summary)
	}
	if gotStages[1].LogPath == nil || *gotStages[1].LogPath != logPath {
		t.Fatalf("LogPath=%v want %q", gotStages[1].LogPath, logPath)
	}

	waiting, err := s.ListWaitingApproval()
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 {
		t.Fatalf("len(ListWaitingApproval)=%d want 1", len(waiting))
	}
	if waiting[0].ID != "stage-run-2" {
		t.Fatalf("waiting ID=%q want stage-run-2", waiting[0].ID)
	}
}

func openTempStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(filepath.Join(t.TempDir(), "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})

	return s
}

func createRun(t *testing.T, s *store.Store, id string) {
	t.Helper()

	if err := s.CreateRun(store.Run{
		ID:         id,
		TemplateID: "template-1",
		Status:     store.RunStatusPending,
		CWD:        "/repo",
		RunDir:     "/repo/.shuttle/runs/" + id,
	}); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *store.Store {
	return openTempStore(t)
}

func TestResetStagesAtomic(t *testing.T) {
	s := openStore(t)
	must(t, s.CreateRun(store.Run{ID: "r", TemplateID: "t", Status: store.RunStatusFailed, CWD: "/x", RunDir: "/x"}))
	must(t, s.CreateStageRuns([]store.StageRun{
		{ID: "s1", RunID: "r", StageID: "a", Worker: "w", Status: store.StageStatusFailed, Attempt: 3, Retries: 2},
		{ID: "s2", RunID: "r", StageID: "b", Worker: "w", Status: store.StageStatusSkipped},
	}))
	err := s.ResetStages([]store.StageRun{
		{ID: "s1", RunID: "r", StageID: "a", Worker: "w", Status: store.StageStatusReady, Attempt: 0, Retries: 2},
		{ID: "s2", RunID: "r", StageID: "b", Worker: "w", Status: store.StageStatusPending, Attempt: 0, Retries: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetStageRuns("r")
	m := map[string]store.StageRun{}
	for _, sr := range got {
		m[sr.StageID] = sr
	}
	if m["a"].Status != store.StageStatusReady || m["a"].Attempt != 0 {
		t.Fatalf("a=%+v", m["a"])
	}
	if m["b"].Status != store.StageStatusPending {
		t.Fatalf("b=%+v", m["b"])
	}
}

func TestRunMetadataRoundTrip(t *testing.T) {
	s := openTempStore(t)
	err := s.CreateRun(store.Run{
		ID:         "run-1",
		TemplateID: "pr-factory-headless",
		Status:     store.RunStatusRunning,
		CWD:        "/tmp/x",
		RunDir:     "/tmp/x/.shuttle/runs/run-1",
		Agent:      "cursor",
		Model:      "claude-4-sonnet",
		IssueRef:   "org/repo#42",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "cursor" || got.Model != "claude-4-sonnet" || got.IssueRef != "org/repo#42" {
		t.Fatalf("metadata not persisted: %+v", got)
	}
	if got.ParentRunID != "" {
		t.Fatalf("ParentRunID=%q want empty", got.ParentRunID)
	}
}

func TestMigrateRunsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shuttle.db")
	s1, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()
	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	_ = s2.Close()
}

func TestResolveRunID(t *testing.T) {
	s := openTempStore(t)
	must(t, s.CreateRun(store.Run{ID: "a1b2c3d4-1111-4111-8111-000000000000", TemplateID: "t", Status: store.RunStatusFailed, CWD: "/x", RunDir: "/x"}))
	must(t, s.CreateRun(store.Run{ID: "a1b2c3d4-2222-4222-8222-000000000000", TemplateID: "t", Status: store.RunStatusFailed, CWD: "/x", RunDir: "/x"}))
	must(t, s.CreateRun(store.Run{ID: "ffffffff-3333-4333-8333-000000000000", TemplateID: "t", Status: store.RunStatusFailed, CWD: "/x", RunDir: "/x"}))

	// exact full id
	if id, err := s.ResolveRunID("ffffffff-3333-4333-8333-000000000000"); err != nil || id != "ffffffff-3333-4333-8333-000000000000" {
		t.Fatalf("exact: id=%q err=%v", id, err)
	}
	// unique 8-char prefix (ffffffff)
	if id, err := s.ResolveRunID("ffffffff"); err != nil || !strings.HasPrefix(id, "ffffffff") {
		t.Fatalf("prefix: id=%q err=%v", id, err)
	}
	// mixed case normalized
	if _, err := s.ResolveRunID("FFFFFFFF"); err != nil {
		t.Fatalf("mixed-case: err=%v", err)
	}
	// too short
	if _, err := s.ResolveRunID("a1b2"); !errors.Is(err, store.ErrRunIDTooShort) {
		t.Fatalf("too short: err=%v", err)
	}
	// ambiguous (a1b2c3d4 matches two)
	if _, err := s.ResolveRunID("a1b2c3d4"); !errors.Is(err, store.ErrRunIDAmbiguous) {
		t.Fatalf("ambiguous: err=%v", err)
	}
	// not found
	if _, err := s.ResolveRunID("00000000"); !errors.Is(err, store.ErrRunIDNotFound) {
		t.Fatalf("not found: err=%v", err)
	}
}

func TestListRunsFilters(t *testing.T) {
	s := openTempStore(t)
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mk := func(id, agent, model, tmpl, issue string, st store.RunStatus, created time.Time) {
		must(t, s.CreateRun(store.Run{ID: id, TemplateID: tmpl, Status: st, CWD: "/x", RunDir: "/x",
			Agent: agent, Model: model, IssueRef: issue, CreatedAt: created, UpdatedAt: created}))
	}
	mk("11111111-a", "cursor", "m1", "pr-factory-headless", "o/r#1", store.RunStatusFailed, base)
	mk("22222222-b", "claude", "", "pr-factory-headless", "o/r#2", store.RunStatusSucceeded, base.Add(time.Hour))
	mk("33333333-c", "cursor", "m1", "pr-factory", "o/r#1", store.RunStatusRunning, base.Add(2*time.Hour))

	// newest first, default limit
	all, err := s.ListRuns(store.ListRunsOpts{})
	if err != nil || len(all) != 3 || all[0].ID != "33333333-c" {
		t.Fatalf("all=%d first=%v err=%v", len(all), all, err)
	}
	// agent filter
	if got, _ := s.ListRuns(store.ListRunsOpts{Agent: "cursor"}); len(got) != 2 {
		t.Fatalf("agent cursor=%d want 2", len(got))
	}
	// status OR
	if got, _ := s.ListRuns(store.ListRunsOpts{Statuses: []store.RunStatus{store.RunStatusFailed, store.RunStatusRunning}}); len(got) != 2 {
		t.Fatalf("status filter=%d want 2", len(got))
	}
	// model unset
	if got, _ := s.ListRuns(store.ListRunsOpts{ModelUnset: true}); len(got) != 1 || got[0].ID != "22222222-b" {
		t.Fatalf("model unset=%v", got)
	}
	// template
	if got, _ := s.ListRuns(store.ListRunsOpts{Template: "pr-factory"}); len(got) != 1 {
		t.Fatalf("template=%d want 1", len(got))
	}
	// issue
	if got, _ := s.ListRuns(store.ListRunsOpts{IssueRef: "o/r#1"}); len(got) != 2 {
		t.Fatalf("issue=%d want 2", len(got))
	}
	// since
	if got, _ := s.ListRuns(store.ListRunsOpts{Since: base.Add(90 * time.Minute)}); len(got) != 1 {
		t.Fatalf("since=%d want 1", len(got))
	}
	// limit
	if got, _ := s.ListRuns(store.ListRunsOpts{Limit: 1}); len(got) != 1 {
		t.Fatalf("limit=%d want 1", len(got))
	}
}
