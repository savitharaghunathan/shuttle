package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/worker"
)

func TestFinalizeStageOutputs_StrictFailsWhenBothMissing(t *testing.T) {
	runDir := t.TempDir()

	err := finalizeStageOutputs(runDir, "plan", []string{"summary"}, &worker.Result{}, true)
	if err == nil {
		t.Fatal("finalizeStageOutputs() error = nil, want missing output error")
	}
}

func TestFinalizeStageOutputs_BridgeSucceedsWhenBothMissing(t *testing.T) {
	runDir := t.TempDir()

	err := finalizeStageOutputs(runDir, "implement", []string{"summary"}, &worker.Result{}, false)
	if err != nil {
		t.Fatalf("finalizeStageOutputs() = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "artifacts", "implement", "summary")); !os.IsNotExist(err) {
		t.Fatalf("summary stat error = %v, want missing file", err)
	}
}

func TestFinalizeStageOutputs_StdoutBridge(t *testing.T) {
	runDir := t.TempDir()

	err := finalizeStageOutputs(runDir, "design", []string{"summary"}, &worker.Result{Summary: "design notes\n"}, true)
	if err != nil {
		t.Fatalf("finalizeStageOutputs() = %v, want nil", err)
	}
	data, err := os.ReadFile(filepath.Join(runDir, "artifacts", "design", "summary"))
	if err != nil {
		t.Fatalf("read bridged summary: %v", err)
	}
	if string(data) != "design notes\n" {
		t.Fatalf("bridged summary = %q, want worker summary", string(data))
	}
}

func TestEnsureOutputDirs(t *testing.T) {
	runDir := t.TempDir()

	if err := ensureOutputDirs(runDir, "plan", []string{"summary"}); err != nil {
		t.Fatalf("ensureOutputDirs() = %v", err)
	}
	info, err := os.Stat(filepath.Join(runDir, "artifacts", "plan"))
	if err != nil {
		t.Fatalf("stat output dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("output path is not a directory")
	}
}

func TestIsStrictOutputTemplate(t *testing.T) {
	tests := map[string]bool{
		"sdlc":          true,
		"sdlc-headless": true,
		"sdlc-dry":      false,
		"sdlc-foo":      false,
		"pr-factory":    false,
	}
	for id, want := range tests {
		if got := IsStrictOutputTemplate(id); got != want {
			t.Fatalf("IsStrictOutputTemplate(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestShuttleStageEnv(t *testing.T) {
	env := shuttleStageEnv("/tmp/run", "implement")
	if env["SHUTTLE_RUN_DIR"] != "/tmp/run" {
		t.Fatalf("SHUTTLE_RUN_DIR = %q, want /tmp/run", env["SHUTTLE_RUN_DIR"])
	}
	if env["SHUTTLE_STAGE_ID"] != "implement" {
		t.Fatalf("SHUTTLE_STAGE_ID = %q, want implement", env["SHUTTLE_STAGE_ID"])
	}
	if len(env) != 2 {
		t.Fatalf("env has %d keys, want 2", len(env))
	}
}
