package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkersUsesDefaultsWithoutConfigFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"workers"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"claude", "cursor", "shell"} {
		if !strings.Contains(got, want) {
			t.Fatalf("workers output %q missing %q", got, want)
		}
	}
}

func TestRunRequiresSpecForPRFactory(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "pr-factory"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run without --spec succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--spec is required for pr-factory") {
		t.Fatalf("error=%v", err)
	}
}

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

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	})
}

func parseRunID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "run: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "run: "))
		}
	}
	t.Fatalf("run id not found in output %q", output)
	return ""
}
