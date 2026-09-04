package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArtifactFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runDirWithArtifacts(t *testing.T) string {
	t.Helper()
	runDir := t.TempDir()
	writeArtifactFile(t, filepath.Join(runDir, "artifacts", "spec"), "the spec\n")
	writeArtifactFile(t, filepath.Join(runDir, "artifacts", "plan", "summary"), "the plan\n")
	writeArtifactFile(t, filepath.Join(runDir, "artifacts", "design", "summary"), "the design\n")
	return runDir
}

func TestListArtifactsFlattensStageDirs(t *testing.T) {
	entries, err := listArtifacts(runDirWithArtifacts(t))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	want := []string{"design/summary", "plan/summary", "spec"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("artifact names = %v, want %v", names, want)
	}
	for _, entry := range entries {
		if entry.Size == 0 {
			t.Fatalf("artifact %s has zero size", entry.Name)
		}
	}
}

func TestListArtifactsMissingDirIsEmpty(t *testing.T) {
	entries, err := listArtifacts(t.TempDir())
	if err != nil {
		t.Fatalf("listArtifacts on run without artifacts returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want none", entries)
	}
	if hasArtifacts(t.TempDir()) {
		t.Fatal("hasArtifacts = true for run without artifacts")
	}
}

func TestPrintArtifactByStageID(t *testing.T) {
	entries, err := listArtifacts(runDirWithArtifacts(t))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := printArtifact(&out, entries, "plan"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "the plan\n" {
		t.Fatalf("printArtifact(plan) = %q, want %q", out.String(), "the plan\n")
	}

	out.Reset()
	if err := printArtifact(&out, entries, "plan/summary"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "the plan\n" {
		t.Fatalf("printArtifact(plan/summary) = %q, want %q", out.String(), "the plan\n")
	}
}

func TestPrintArtifactUnknownNameListsAvailable(t *testing.T) {
	entries, err := listArtifacts(runDirWithArtifacts(t))
	if err != nil {
		t.Fatal(err)
	}
	err = printArtifact(&bytes.Buffer{}, entries, "review")
	if err == nil {
		t.Fatal("printArtifact for unknown name succeeded, want error")
	}
	for _, want := range []string{`no artifact "review"`, "plan/summary", "spec"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v missing %q", err, want)
		}
	}
}

func TestWriteArtifactTableIncludesPaths(t *testing.T) {
	runDir := runDirWithArtifacts(t)
	entries, err := listArtifacts(runDir)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeArtifactTable(&out, entries); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ARTIFACT", "BYTES", "PATH", "plan/summary", filepath.Join(runDir, "artifacts", "spec")} {
		if !strings.Contains(got, want) {
			t.Fatalf("artifact table %q missing %q", got, want)
		}
	}
}
