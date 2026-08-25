package cli

import (
	"bytes"
	"os"
	"path/filepath"
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
