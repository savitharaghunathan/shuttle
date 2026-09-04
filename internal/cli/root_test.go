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

func TestResolveTemplateID(t *testing.T) {
	tests := []struct {
		name     string
		template string
		headless bool
		hasIssue bool
		hasSpec  bool
		want     string
		wantErr  string
	}{
		{name: "default is gated pr-factory", hasSpec: true, want: "pr-factory"},
		{name: "bare issue stays pr-factory-headless", hasIssue: true, want: "pr-factory-headless"},
		{name: "bare headless stays pr-factory-headless", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps pr-factory", template: "pr-factory", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps sdlc", template: "sdlc", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "headless leaves sdlc-headless", template: "sdlc-headless", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "sdlc with issue", template: "sdlc", hasIssue: true, want: "sdlc"},
		{name: "local overlay needs no spec", template: "sdlc-dry", want: "sdlc-dry"},
		{name: "pipeline path needs no spec", template: "pipelines/sdlc-dry.yaml", want: "pipelines/sdlc-dry.yaml"},

		{name: "pr-factory without input", template: "pr-factory", wantErr: "--spec is required for pr-factory"},
		{name: "pr-factory-headless without input", template: "pr-factory-headless", wantErr: "--spec is required for pr-factory"},
		{name: "headless without input", headless: true, wantErr: "--spec is required for pr-factory"},
		{name: "sdlc without input", template: "sdlc", wantErr: "--spec is required for sdlc"},
		{name: "sdlc-headless without input", template: "sdlc-headless", wantErr: "--spec is required for sdlc"},
		{name: "spec and issue together", template: "sdlc", hasIssue: true, hasSpec: true, wantErr: "not both"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTemplateID(tc.template, tc.headless, tc.hasIssue, tc.hasSpec)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveTemplateID(%q) = %q, want error %q", tc.template, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveTemplateID(%q) error = %v, want %q", tc.template, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTemplateID(%q) returned error: %v", tc.template, err)
			}
			if got != tc.want {
				t.Fatalf("resolveTemplateID(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
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
