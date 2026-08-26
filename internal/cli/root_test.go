package cli

import (
	"bytes"
	"os"
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
