//go:build integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAndStatusEmbeddedTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: shell
work_root: .shuttle
test:
  command: ["true"]
`
	if err := os.WriteFile("shuttle.yaml", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("stub feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "pr-factory", "--spec", specPath})

	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runOutput := out.String()
	if !strings.Contains(runOutput, "status: blocked") {
		t.Fatalf("run output %q missing blocked status", runOutput)
	}
	if !strings.Contains(runOutput, "shuttle approve") || !strings.Contains(runOutput, "approve_pr") {
		t.Fatalf("run output %q missing approval hint", runOutput)
	}
	runID := parseRunID(t, runOutput)

	out.Reset()
	statusCmd := NewRootCommand(&out, &errOut)
	statusCmd.SetArgs([]string{"status", runID})
	if err := statusCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	statusOutput := out.String()
	if !strings.Contains(statusOutput, "run: "+runID) {
		t.Fatalf("status output %q missing run id", statusOutput)
	}
	for _, want := range []string{"intake", "implement", "test", "approve_pr"} {
		if !strings.Contains(statusOutput, want) {
			t.Fatalf("status output %q missing stage %q", statusOutput, want)
		}
	}
}

func TestE2EDryPRFactory(t *testing.T) {
	root := moduleRoot(t)
	pipelinePath := filepath.Join(root, "pipelines", "pr-factory-dry.yaml")

	dir := t.TempDir()
	chdir(t, dir)

	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("dry pr factory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", pipelinePath, "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: blocked") {
		t.Fatalf("run output %q missing blocked status", runOutput)
	}
	for _, want := range []string{
		"stage: intake\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: approve_pr\tstatus: waiting_approval",
		"stage: open_pr\tstatus: pending",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}

	runID := parseRunID(t, runOutput)
	out.Reset()
	approveCmd := NewRootCommand(&out, &errOut)
	approveCmd.SetArgs([]string{"approve", runID, "approve_pr", "--message", "dry approved"})
	if err := approveCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	approveOutput := out.String()
	if !strings.Contains(approveOutput, "status: succeeded") {
		t.Fatalf("approve output %q missing succeeded status", approveOutput)
	}
	if !strings.Contains(approveOutput, "stage: open_pr\tstatus: succeeded") {
		t.Fatalf("approve output %q missing succeeded open_pr", approveOutput)
	}
}

func TestE2ECustomAgentFromConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: cursor
work_root: .shuttle
test:
  command: ["true"]
open_pr:
  command: ["true"]
workers:
  stubagent:
    bin: /bin/sh
    args: ["-c", "printf 'stub:%s' \"$1\"", "sh", "{{.Prompt}}"]
`
	if err := os.WriteFile("shuttle.yaml", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("custom agent feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	workersCmd := NewRootCommand(&out, &errOut)
	workersCmd.SetArgs([]string{"workers"})
	if err := workersCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stubagent") {
		t.Fatalf("workers output %q missing stubagent", out.String())
	}

	out.Reset()
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{
		"run", "-t", "pr-factory-headless",
		"--spec", specPath,
		"--agent", "stubagent",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	for _, want := range []string{
		"→ stage implement (stubagent) starting...",
		"stage: intake\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: open_pr\tstatus: succeeded",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}
}
