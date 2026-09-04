package templates_test

import (
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/templates"
)

func TestGetPrFactoryTemplate(t *testing.T) {
	p, err := templates.Get("pr-factory")
	if err != nil {
		t.Fatalf("Get(pr-factory) = %v", err)
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate(pr-factory) = %v", err)
	}

	wantIDs := []string{"intake", "implement", "test", "approve_pr", "open_pr"}
	if got := stageIDs(p); strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("stage ids = %v, want %v", got, wantIDs)
	}
	implement := p.Stages[1]
	if implement.Worker != "" {
		t.Fatalf("implement worker = %q, want empty for default worker injection", implement.Worker)
	}
	if !strings.Contains(implement.Prompt, "You MUST use branch {{.Branch}} exactly.") {
		t.Fatalf("implement prompt missing required branch instruction: %q", implement.Prompt)
	}
	if !contains(implement.Outputs, "summary") {
		t.Fatalf("implement outputs = %v, want summary", implement.Outputs)
	}
	if p.Stages[3].Worker != "manual" {
		t.Fatalf("approve_pr worker = %q, want manual", p.Stages[3].Worker)
	}
	if got := p.Stages[4].DependsOn; len(got) != 1 || got[0] != "approve_pr" {
		t.Fatalf("open_pr depends_on = %v, want [approve_pr]", got)
	}
	if len(p.Stages[2].Command) != 0 || len(p.Stages[4].Command) != 0 {
		t.Fatalf("safe commands = test:%v open_pr:%v, want empty for config injection", p.Stages[2].Command, p.Stages[4].Command)
	}
}

func TestGetSDLCTemplate(t *testing.T) {
	p, err := templates.Get("sdlc")
	if err != nil {
		t.Fatalf("Get(sdlc) = %v", err)
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate(sdlc) = %v", err)
	}

	wantIDs := []string{"plan", "design", "implement", "test", "review", "approve_ship", "ship"}
	if got := stageIDs(p); strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("stage ids = %v, want %v", got, wantIDs)
	}
	plan := stageByID(t, p, "plan")
	if !plan.ReadOnly {
		t.Fatal("plan readonly = false, want true")
	}
	if plan.Worker != "" {
		t.Fatalf("plan worker = %q, want empty for readonly worker injection", plan.Worker)
	}
	if !strings.Contains(plan.Prompt, "Do NOT modify, create, or delete any files in {{.Cwd}}.") {
		t.Fatalf("plan prompt missing readonly preamble: %q", plan.Prompt)
	}
	if !strings.Contains(plan.Prompt, "{{.ArtifactBodies.spec}}") {
		t.Fatalf("plan prompt missing spec artifact body: %q", plan.Prompt)
	}
	if !contains(plan.Outputs, "summary") {
		t.Fatalf("plan outputs = %v, want summary", plan.Outputs)
	}

	design := stageByID(t, p, "design")
	if !design.ReadOnly {
		t.Fatal("design readonly = false, want true")
	}
	if design.Worker != "" {
		t.Fatalf("design worker = %q, want empty for readonly worker injection", design.Worker)
	}
	if !strings.Contains(design.Prompt, "{{.ArtifactBodies.plan}}") {
		t.Fatalf("design prompt missing plan artifact body: %q", design.Prompt)
	}
	if !contains(design.Outputs, "summary") {
		t.Fatalf("design outputs = %v, want summary", design.Outputs)
	}

	implement := stageByID(t, p, "implement")
	if implement.Worker != "" {
		t.Fatalf("implement worker = %q, want empty for default worker injection", implement.Worker)
	}
	if !strings.Contains(implement.Prompt, "{{.ArtifactBodies.design}}") {
		t.Fatalf("implement prompt missing design artifact body: %q", implement.Prompt)
	}
	if !strings.Contains(implement.Prompt, "You MUST use branch {{.Branch}} exactly.") {
		t.Fatalf("implement prompt missing required branch instruction: %q", implement.Prompt)
	}
	if !contains(implement.Outputs, "summary") {
		t.Fatalf("implement outputs = %v, want summary", implement.Outputs)
	}

	review := stageByID(t, p, "review")
	if !review.ReadOnly {
		t.Fatal("review readonly = false, want true")
	}
	if review.Worker != "" {
		t.Fatalf("review worker = %q, want empty for readonly worker injection", review.Worker)
	}
	if !strings.Contains(review.Prompt, "{{.ArtifactBodies.implement}}") {
		t.Fatalf("review prompt missing implement artifact body: %q", review.Prompt)
	}
	if !strings.Contains(review.Prompt, "{{.ArtifactBodies.test}}") {
		t.Fatalf("review prompt missing test artifact body: %q", review.Prompt)
	}
	if !contains(review.Outputs, "summary") {
		t.Fatalf("review outputs = %v, want summary", review.Outputs)
	}

	if p.Stages[5].Worker != "manual" {
		t.Fatalf("approve_ship worker = %q, want manual", p.Stages[5].Worker)
	}
	if got := p.Stages[6].DependsOn; len(got) != 1 || got[0] != "approve_ship" {
		t.Fatalf("ship depends_on = %v, want [approve_ship]", got)
	}
	if len(p.Stages[3].Command) != 0 || len(p.Stages[6].Command) != 0 {
		t.Fatalf("safe commands = test:%v ship:%v, want empty for config injection", p.Stages[3].Command, p.Stages[6].Command)
	}
}

func TestGetSDLCHeadlessTemplate(t *testing.T) {
	p, err := templates.Get("sdlc-headless")
	if err != nil {
		t.Fatalf("Get(sdlc-headless) = %v", err)
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate(sdlc-headless) = %v", err)
	}

	wantIDs := []string{"plan", "design", "implement", "test", "review", "ship"}
	if got := stageIDs(p); strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("stage ids = %v, want %v", got, wantIDs)
	}
	if stageByID(t, p, "plan").Worker != "" {
		t.Fatalf("plan worker = %q, want empty", stageByID(t, p, "plan").Worker)
	}
	if !stageByID(t, p, "review").ReadOnly {
		t.Fatal("review readonly = false, want true")
	}
	ship := stageByID(t, p, "ship")
	if got := ship.DependsOn; len(got) != 1 || got[0] != "review" {
		t.Fatalf("ship depends_on = %v, want [review]", got)
	}
	if len(ship.Command) != 0 {
		t.Fatalf("ship command = %v, want empty for config injection", ship.Command)
	}
}

func TestIDsIncludesSDLCHeadless(t *testing.T) {
	got := templates.IDs()
	if !contains(got, "sdlc-headless") {
		t.Fatalf("IDs() = %v, want sdlc-headless", got)
	}
}

func TestGetUnknownTemplate(t *testing.T) {
	_, err := templates.Get("missing")
	if err == nil {
		t.Fatal("Get(missing) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Get(missing) = %v, want id in error", err)
	}
}

func stageIDs(p *pipeline.Pipeline) []string {
	ids := make([]string, 0, len(p.Stages))
	for _, stage := range p.Stages {
		ids = append(ids, stage.ID)
	}
	return ids
}

func stageByID(t *testing.T, p *pipeline.Pipeline, id string) pipeline.Stage {
	t.Helper()
	for _, stage := range p.Stages {
		if stage.ID == id {
			return stage
		}
	}
	t.Fatalf("stage %q not found in %v", id, stageIDs(p))
	return pipeline.Stage{}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
