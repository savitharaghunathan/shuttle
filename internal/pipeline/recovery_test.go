package pipeline

import (
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

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
