package pipeline

import (
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

func mkStage(id string, st store.StageStatus) store.StageRun {
	return store.StageRun{StageID: id, Status: st}
}

func TestCurrentStage(t *testing.T) {
	pl := &Pipeline{Stages: []Stage{{ID: "intake"}, {ID: "implement"}, {ID: "test"}, {ID: "open_pr"}}}

	running := []store.StageRun{
		mkStage("intake", store.StageStatusSucceeded),
		mkStage("implement", store.StageStatusRunning),
		mkStage("test", store.StageStatusPending),
	}
	if got := CurrentStage(pl, running); got != "implement" {
		t.Fatalf("running=%q want implement", got)
	}

	failed := []store.StageRun{
		mkStage("intake", store.StageStatusSucceeded),
		mkStage("implement", store.StageStatusFailed),
		mkStage("test", store.StageStatusSkipped),
	}
	if got := CurrentStage(pl, failed); got != "implement" {
		t.Fatalf("failed=%q want implement", got)
	}

	succeeded := []store.StageRun{
		mkStage("intake", store.StageStatusSucceeded),
		mkStage("implement", store.StageStatusSucceeded),
		mkStage("test", store.StageStatusSucceeded),
		mkStage("open_pr", store.StageStatusSucceeded),
	}
	if got := CurrentStage(pl, succeeded); got != "open_pr" {
		t.Fatalf("succeeded=%q want open_pr", got)
	}

	if got := CurrentStage(&Pipeline{}, nil); got != "" {
		t.Fatalf("empty=%q want \"\"", got)
	}
}
