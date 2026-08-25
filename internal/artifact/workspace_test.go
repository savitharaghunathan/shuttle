package artifact_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/artifact"
)

func TestInitRunCreatesLayout(t *testing.T) {
	workRoot := t.TempDir()

	runDir, err := artifact.InitRun(workRoot, "run-1")
	if err != nil {
		t.Fatalf("InitRun() = %v", err)
	}
	want := filepath.Join(workRoot, "runs", "run-1")
	if runDir != want {
		t.Fatalf("InitRun() dir = %q, want %q", runDir, want)
	}

	for _, sub := range []string{"inputs", "artifacts", "logs", "approvals"} {
		info, err := os.Stat(filepath.Join(runDir, sub))
		if err != nil {
			t.Fatalf("stat %s: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", sub)
		}
	}
}

func TestCopySpecWritesBothPaths(t *testing.T) {
	workRoot := t.TempDir()
	runDir, err := artifact.InitRun(workRoot, "run-1")
	if err != nil {
		t.Fatalf("InitRun() = %v", err)
	}

	specPath := filepath.Join(t.TempDir(), "feature.md")
	const content = "# Feature spec\n\nDo the thing.\n"
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) = %v", err)
	}

	if err := artifact.CopySpec(runDir, specPath); err != nil {
		t.Fatalf("CopySpec() = %v", err)
	}

	inputsPath := filepath.Join(runDir, "inputs", "spec.md")
	artifactsPath := filepath.Join(runDir, "artifacts", "spec")

	inputBytes, err := os.ReadFile(inputsPath)
	if err != nil {
		t.Fatalf("ReadFile(inputs/spec.md) = %v", err)
	}
	artifactBytes, err := os.ReadFile(artifactsPath)
	if err != nil {
		t.Fatalf("ReadFile(artifacts/spec) = %v", err)
	}

	if !bytes.Equal(inputBytes, []byte(content)) {
		t.Fatalf("inputs/spec.md = %q, want %q", inputBytes, content)
	}
	if !bytes.Equal(artifactBytes, []byte(content)) {
		t.Fatalf("artifacts/spec = %q, want %q", artifactBytes, content)
	}
	if !bytes.Equal(inputBytes, artifactBytes) {
		t.Fatal("inputs/spec.md and artifacts/spec differ")
	}
}
