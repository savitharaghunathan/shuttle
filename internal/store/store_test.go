package store_test

import (
	"path/filepath"
	"testing"

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
