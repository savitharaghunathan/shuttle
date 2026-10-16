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

func TestRunRequiresSpecForSDLC(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "sdlc"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run without --spec succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--spec is required for sdlc") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunHeadlessUnsupportedTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "sdlc-dry", "--headless", "--spec", "spec.md"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run --headless on sdlc-dry succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--headless is not supported for template sdlc-dry") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunRejectsSpecAndIssue(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "sdlc", "--spec", "spec.md", "--issue", "https://github.com/acme/widget/issues/1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run with --spec and --issue succeeded, want error")
	}
	if !strings.Contains(err.Error(), "use either --issue or --spec, not both") {
		t.Fatalf("error=%v", err)
	}
}

func TestTailLines(t *testing.T) {
	log := "one\ntwo\nthree\nfour\n"
	tests := []struct {
		n    int
		want string
	}{
		{n: 0, want: log},
		{n: -1, want: log},
		{n: 2, want: "three\nfour\n"},
		{n: 4, want: log},
		{n: 10, want: log},
	}
	for _, tc := range tests {
		if got := tailLines(log, tc.n); got != tc.want {
			t.Fatalf("tailLines(log, %d) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if got := tailLines("", 3); got != "" {
		t.Fatalf("tailLines(\"\", 3) = %q, want empty", got)
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
