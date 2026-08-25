package templates_test

import (
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/templates"
)

func TestGetPrFactoryHeadlessTemplate(t *testing.T) {
	p, err := templates.Get("pr-factory-headless")
	if err != nil {
		t.Fatalf("Get(pr-factory-headless) = %v", err)
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate(pr-factory-headless) = %v", err)
	}

	wantIDs := []string{"intake", "implement", "test", "open_pr"}
	if got := stageIDs(p); strings.Join(got, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("stage ids = %v, want %v", got, wantIDs)
	}
	for _, stage := range p.Stages {
		if stage.Worker == "manual" {
			t.Fatalf("headless pipeline must not contain manual stage %q", stage.ID)
		}
	}
}
