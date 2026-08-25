package worker_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/artifact"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/worker"
)

func TestShellExecuteRunsDirectArgv(t *testing.T) {
	w := worker.NewShellWorker(config.WorkerPreset{})

	res, err := w.Execute(context.Background(), worker.Request{
		StageID: "echo",
		Command: []string{"echo", "ok"},
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
	if !strings.Contains(res.Summary, "ok") {
		t.Fatalf("Summary = %q, want it to contain ok", res.Summary)
	}
}

func TestShellExecuteReportsCommandFailure(t *testing.T) {
	w := worker.NewShellWorker(config.WorkerPreset{})

	res, err := w.Execute(context.Background(), worker.Request{
		StageID: "fail",
		Command: []string{"/bin/sh", "-c", "echo nope; exit 7"},
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusFailed {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusFailed)
	}
	if !strings.Contains(res.Summary, "nope") {
		t.Fatalf("Summary = %q, want command output", res.Summary)
	}
}

func TestShellExecuteHonorsTimeout(t *testing.T) {
	w := worker.NewShellWorker(config.WorkerPreset{})

	res, err := w.Execute(context.Background(), worker.Request{
		StageID: "timeout",
		Command: []string{"/bin/sh", "-c", "sleep 5"},
		Timeout: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want timeout error")
	}
	if res.Status != worker.StatusFailed {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusFailed)
	}
}

func TestIntakeExecuteCopiesSpecAndReportsOutput(t *testing.T) {
	workRoot := t.TempDir()
	runDir, err := artifact.InitRun(workRoot, "run-1")
	if err != nil {
		t.Fatalf("InitRun() = %v", err)
	}
	specPath := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(specPath, []byte("# Spec\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) = %v", err)
	}

	res, err := worker.NewIntakeWorker().Execute(context.Background(), worker.Request{
		RunDir:   runDir,
		SpecPath: specPath,
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
	if got := res.Outputs["spec"]; got != "spec" {
		t.Fatalf("Outputs[spec] = %q, want spec", got)
	}
	copied, err := os.ReadFile(filepath.Join(runDir, "artifacts", "spec"))
	if err != nil {
		t.Fatalf("ReadFile(artifacts/spec) = %v", err)
	}
	if string(copied) != "# Spec\n" {
		t.Fatalf("copied spec = %q", copied)
	}
}

func TestIntakeExecuteAcceptsPreloadedSpec(t *testing.T) {
	workRoot := t.TempDir()
	runDir, err := artifact.InitRun(workRoot, "run-1")
	if err != nil {
		t.Fatalf("InitRun() = %v", err)
	}
	if err := artifact.WriteSpec(runDir, []byte("# Issue spec\n")); err != nil {
		t.Fatal(err)
	}

	res, err := worker.NewIntakeWorker().Execute(context.Background(), worker.Request{
		RunDir: runDir,
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
}

func TestShellExecuteStreamsToOutput(t *testing.T) {
	w := worker.NewShellWorker(config.WorkerPreset{})

	var streamed strings.Builder
	res, err := w.Execute(context.Background(), worker.Request{
		StageID: "echo",
		Command: []string{"echo", "streaming-ok"},
		Output:  &streamed,
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
	if streamed.String() != "streaming-ok\n" {
		t.Fatalf("streamed = %q", streamed.String())
	}
}

func TestExecExecuteRunsFullArgv(t *testing.T) {
	w := worker.NewExecWorker()

	res, err := w.Execute(context.Background(), worker.Request{
		Command: []string{"echo", "exec-ok"},
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
	if !strings.Contains(res.Summary, "exec-ok") {
		t.Fatalf("Summary = %q, want it to contain exec-ok", res.Summary)
	}
}

func TestPresetExecuteRendersPromptAndRunsArgv(t *testing.T) {
	w := worker.NewPresetWorker("claude", config.WorkerPreset{
		Bin:  "/bin/sh",
		Args: []string{"-c", "printf '%s' \"$1\"", "shuttle-preset", "{{.Prompt}}"},
	})

	res, err := w.Execute(context.Background(), worker.Request{
		Prompt: "build the feature",
	})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusSucceeded)
	}
	if res.Summary != "build the feature" {
		t.Fatalf("Summary = %q, want rendered prompt", res.Summary)
	}
}

func TestPresetExecuteMissingBinNamesWorker(t *testing.T) {
	w := worker.NewPresetWorker("cursor", config.WorkerPreset{})

	_, err := w.Execute(context.Background(), worker.Request{Prompt: "ship it"})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing bin error")
	}
	if !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("Execute() error = %v, want worker name", err)
	}
}

func TestPresetExecuteMissingBinaryNamesWorker(t *testing.T) {
	w := worker.NewPresetWorker("claude", config.WorkerPreset{
		Bin:  "/definitely/missing/shuttle-binary",
		Args: []string{"--version"},
	})

	_, err := w.Execute(context.Background(), worker.Request{Prompt: "ship it"})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing binary error")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("Execute() error = %v, want worker name", err)
	}
}

func TestFakeExecuteReturnsStageResult(t *testing.T) {
	w := worker.NewFakeWorker(map[string]worker.Result{
		"test": {Status: worker.StatusSucceeded, Summary: "done"},
	})

	res, err := w.Execute(context.Background(), worker.Request{StageID: "test"})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Summary != "done" {
		t.Fatalf("Summary = %q, want done", res.Summary)
	}
}

func TestRegistryIncludesCoreWorkers(t *testing.T) {
	reg := worker.NewRegistry(&config.Config{
		Workers: map[string]config.WorkerPreset{
			"shell": {Bin: "/bin/sh"},
		},
	})

	for _, name := range []string{"shell", "exec", "intake", "claude", "codex", "opencode", "cursor"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("Get(%q) ok = false, want true", name)
		}
	}
}
