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

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
