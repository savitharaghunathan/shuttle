package store

import (
	"path/filepath"
	"testing"
)

func TestOpenEnforcesForeignKeysAndSingleWriter(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})

	if got := s.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections=%d want 1", got)
	}

	err = s.CreateStageRuns([]StageRun{{
		ID:      "stage-run-1",
		RunID:   "missing-run",
		StageID: "test",
		Worker:  "shell",
		Status:  StageStatusReady,
	}})
	if err == nil {
		t.Fatal("CreateStageRuns with missing run succeeded, want foreign key error")
	}
}
