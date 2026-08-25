package pipeline_test

import (
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

func TestRenderSubstitutes(t *testing.T) {
	ctx := pipeline.RenderContext{
		RunID:   "run-1",
		StageID: "implement",
		Cwd:     "/project",
		RunDir:  "/project/.shuttle/runs/run-1",
		Artifacts: map[string]string{
			"spec": "/project/.shuttle/runs/run-1/artifacts/spec",
		},
		Inputs: map[string]string{
			"spec": "/project/.shuttle/runs/run-1/inputs/spec.md",
		},
		Env: map[string]string{
			"FOO": "bar",
		},
	}

	got, err := pipeline.Render("spec={{.Artifacts.spec}} cwd={{.Cwd}} env={{.Env.FOO}}", ctx)
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	want := "spec=/project/.shuttle/runs/run-1/artifacts/spec cwd=/project env=bar"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderMissingKeyErrors(t *testing.T) {
	ctx := pipeline.RenderContext{
		Artifacts: map[string]string{},
	}

	_, err := pipeline.Render("need {{.Artifacts.missing}}", ctx)
	if err == nil {
		t.Fatal("Render() = nil, want missing key error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Render() = %v, want missing key error", err)
	}
}
