package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

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
