package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
)

func TestLoadArtifactBodies_PlanGetsSpec(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "spec"), "spec body")

	bodies, err := loadArtifactBodies(runDir, "plan", &Pipeline{
		Stages: []Stage{{ID: "plan"}},
	})
	if err != nil {
		t.Fatalf("loadArtifactBodies() = %v", err)
	}
	if bodies["spec"] != "spec body" {
		t.Fatalf("spec body = %q, want spec body", bodies["spec"])
	}
}

func TestLoadArtifactBodies_DesignGetsPlanSummary(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "plan", "summary"), "plan summary")

	bodies, err := loadArtifactBodies(runDir, "design", &Pipeline{
		Stages: []Stage{
			{ID: "plan"},
			{ID: "design", DependsOn: []string{"plan"}},
		},
	})
	if err != nil {
		t.Fatalf("loadArtifactBodies() = %v", err)
	}
	if bodies["plan"] != "plan summary" {
		t.Fatalf("plan body = %q, want plan summary", bodies["plan"])
	}
}

func TestLoadArtifactBodies_ReviewGetsImplementAndTestLog(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "plan", "summary"), "plan summary")
	writeFile(t, filepath.Join(runDir, "artifacts", "design", "summary"), "design summary")
	writeFile(t, filepath.Join(runDir, "artifacts", "implement", "summary"), "implement summary")
	writeFile(t, filepath.Join(runDir, "logs", "test.log"), strings.Repeat("a", 40*1024)+"test tail")

	bodies, err := loadArtifactBodies(runDir, "review", &Pipeline{
		Stages: []Stage{
			{ID: "plan"},
			{ID: "design", DependsOn: []string{"plan"}},
			{ID: "implement", DependsOn: []string{"design"}},
			{ID: "test", DependsOn: []string{"implement"}},
			{ID: "review", DependsOn: []string{"implement", "test"}},
		},
	})
	if err != nil {
		t.Fatalf("loadArtifactBodies() = %v", err)
	}
	if bodies["implement"] != "implement summary" {
		t.Fatalf("implement body = %q, want implement summary", bodies["implement"])
	}
	if bodies["plan"] != "plan summary" || bodies["design"] != "design summary" {
		t.Fatalf("transitive summaries missing: %#v", bodies)
	}
	if len(bodies["test"]) != 32*1024 {
		t.Fatalf("test log body length = %d, want 32768", len(bodies["test"]))
	}
	if !strings.HasSuffix(bodies["test"], "test tail") {
		t.Fatalf("test log body does not contain tail")
	}
}

func TestLoadArtifactBodies_ReviewEmptyTestLogStillRenders(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "implement", "summary"), "implement summary")
	writeFile(t, filepath.Join(runDir, "logs", "test.log"), "")

	pl := &Pipeline{
		Stages: []Stage{
			{ID: "implement"},
			{ID: "test", DependsOn: []string{"implement"}},
			{ID: "review", DependsOn: []string{"implement", "test"}},
		},
	}
	bodies, err := loadArtifactBodies(runDir, "review", pl)
	if err != nil {
		t.Fatalf("loadArtifactBodies() = %v", err)
	}
	if _, ok := bodies["test"]; !ok {
		t.Fatal("test key missing for empty test.log")
	}
	if bodies["test"] != "" {
		t.Fatalf("test body = %q, want empty string", bodies["test"])
	}

	_, err = Render("## Test log tail\n{{.ArtifactBodies.test}}", RenderContext{ArtifactBodies: bodies})
	if err != nil {
		t.Fatalf("Render() = %v, want empty test log to render", err)
	}
}

func TestLoadArtifactBodies_EmptyFileOmitted(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "spec"), "")
	writeFile(t, filepath.Join(runDir, "artifacts", "plan", "summary"), "")

	bodies, err := loadArtifactBodies(runDir, "design", &Pipeline{
		Stages: []Stage{
			{ID: "plan"},
			{ID: "design", DependsOn: []string{"plan"}},
		},
	})
	if err != nil {
		t.Fatalf("loadArtifactBodies() = %v", err)
	}
	if _, ok := bodies["spec"]; ok {
		t.Fatalf("empty spec loaded: %#v", bodies)
	}
	if _, ok := bodies["plan"]; ok {
		t.Fatalf("empty plan loaded: %#v", bodies)
	}
}

func TestLoadArtifactBodies_OversizedBodyFails(t *testing.T) {
	runDir := t.TempDir()
	writeFile(t, filepath.Join(runDir, "artifacts", "spec"), strings.Repeat("x", maxArtifactBodyBytes+1))

	_, err := loadArtifactBodies(runDir, "plan", &Pipeline{
		Stages: []Stage{{ID: "plan"}},
	})
	if err == nil {
		t.Fatal("loadArtifactBodies() = nil, want size error")
	}
	if !strings.Contains(err.Error(), "artifact spec exceeds 128 KiB") {
		t.Fatalf("loadArtifactBodies() = %v, want 128 KiB error", err)
	}
}

func TestRenderIncludesInjectedSpec(t *testing.T) {
	got, err := Render("Plan from spec:\n{{.ArtifactBodies.spec}}", RenderContext{
		ArtifactBodies: map[string]string{"spec": "build an SDLC pipeline"},
	})
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	if !strings.Contains(got, "build an SDLC pipeline") {
		t.Fatalf("Render() = %q, want injected spec", got)
	}
}

func TestArtifactBodiesInjectedBeforeStageRender(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	rec := &recordingWorker{
		results: map[string]worker.Result{
			"plan":   {Status: worker.StatusSucceeded, Summary: "plan ok", Outputs: map[string]string{"summary": "plan artifact"}},
			"design": {Status: worker.StatusSucceeded, Summary: "design ok"},
		},
	}
	reg := worker.NewRegistry(nil)
	reg.Register("record", rec)

	eng := NewEngine(db, reg, &config.Config{
		WorkRoot:      filepath.Join(dir, ".shuttle"),
		Concurrency:   1,
		DefaultWorker: "record",
	})
	runID, err := eng.StartRun(context.Background(), StartParams{
		TemplateID:  "tmpl",
		Pipeline:    &Pipeline{Stages: []Stage{{ID: "plan"}, {ID: "design", DependsOn: []string{"plan"}, Prompt: "Use {{.ArtifactBodies.plan}}."}}},
		CWD:         dir,
		SpecContent: "spec body",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.RunUntilBlockOrDone(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if rec.prompts["design"] != "Use plan artifact." {
		t.Fatalf("design prompt = %q, want injected plan artifact", rec.prompts["design"])
	}
}

type recordingWorker struct {
	results map[string]worker.Result
	prompts map[string]string
}

func (w *recordingWorker) Name() string {
	return "record"
}

func (w *recordingWorker) Execute(_ context.Context, req worker.Request) (worker.Result, error) {
	if w.prompts == nil {
		w.prompts = make(map[string]string)
	}
	w.prompts[req.StageID] = req.Prompt
	return w.results[req.StageID], nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
