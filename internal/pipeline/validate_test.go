package pipeline_test

import (
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

func TestValidateLinearOK(t *testing.T) {
	p := &pipeline.Pipeline{
		ID:      "linear",
		Version: "1",
		Stages: []pipeline.Stage{
			{ID: "a", Worker: "shell"},
			{ID: "b", Worker: "shell", DependsOn: []string{"a"}},
			{ID: "c", Worker: "shell", DependsOn: []string{"b"}},
		},
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateDuplicateIDRejected(t *testing.T) {
	p := &pipeline.Pipeline{
		Stages: []pipeline.Stage{
			{ID: "dup", Worker: "shell"},
			{ID: "dup", Worker: "shell"},
		},
	}
	err := pipeline.Validate(p)
	if err == nil {
		t.Fatal("Validate() = nil, want duplicate id error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() = %v, want duplicate id error", err)
	}
}

func TestValidateMissingDepRejected(t *testing.T) {
	p := &pipeline.Pipeline{
		Stages: []pipeline.Stage{
			{ID: "a", Worker: "shell", DependsOn: []string{"missing"}},
		},
	}
	err := pipeline.Validate(p)
	if err == nil {
		t.Fatal("Validate() = nil, want missing dependency error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Validate() = %v, want missing dependency error", err)
	}
}

func TestValidateCycleRejected(t *testing.T) {
	p := &pipeline.Pipeline{
		Stages: []pipeline.Stage{
			{ID: "a", Worker: "shell", DependsOn: []string{"b"}},
			{ID: "b", Worker: "shell", DependsOn: []string{"a"}},
		},
	}
	err := pipeline.Validate(p)
	if err == nil {
		t.Fatal("Validate() = nil, want cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate() = %v, want cycle error", err)
	}
}

func TestLoadBytesParsesPipeline(t *testing.T) {
	const yamlDoc = `
id: example
version: "1"
stages:
  - id: build
    worker: shell
    command: ["make"]
  - id: test
    worker: shell
    depends_on: [build]
    command: ["go", "test", "./..."]
`
	p, err := pipeline.LoadBytes([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("LoadBytes() = %v", err)
	}
	if p.ID != "example" || p.Version != "1" {
		t.Fatalf("pipeline metadata = id=%q version=%q", p.ID, p.Version)
	}
	if len(p.Stages) != 2 || p.Stages[1].DependsOn[0] != "build" {
		t.Fatalf("stages = %+v", p.Stages)
	}
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}
