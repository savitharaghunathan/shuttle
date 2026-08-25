package pipeline_test

import (
	"slices"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

func TestReadyIDsDepGate(t *testing.T) {
	stages := []pipeline.Stage{
		{ID: "a", Worker: "shell"},
		{ID: "b", Worker: "shell", DependsOn: []string{"a"}},
		{ID: "c", Worker: "shell", DependsOn: []string{"b"}},
	}

	status := map[string]string{
		"a": "pending",
		"b": "pending",
		"c": "pending",
	}
	got := pipeline.ReadyIDs(stages, status)
	if !slices.Equal(got, []string{"a"}) {
		t.Fatalf("ReadyIDs() = %v, want [a]", got)
	}

	status["a"] = "succeeded"
	got = pipeline.ReadyIDs(stages, status)
	if !slices.Equal(got, []string{"b"}) {
		t.Fatalf("ReadyIDs() after a succeeded = %v, want [b]", got)
	}

	status["b"] = "succeeded"
	got = pipeline.ReadyIDs(stages, status)
	if !slices.Equal(got, []string{"c"}) {
		t.Fatalf("ReadyIDs() after b succeeded = %v, want [c]", got)
	}
}

func TestReadyIDsExcludesTerminal(t *testing.T) {
	stages := []pipeline.Stage{
		{ID: "a", Worker: "shell"},
		{ID: "b", Worker: "shell", DependsOn: []string{"a"}},
	}

	status := map[string]string{
		"a": "succeeded",
		"b": "succeeded",
	}
	got := pipeline.ReadyIDs(stages, status)
	if len(got) != 0 {
		t.Fatalf("ReadyIDs() = %v, want empty", got)
	}
}

func TestReadyIDsRequiresAllDepsSucceeded(t *testing.T) {
	stages := []pipeline.Stage{
		{ID: "a", Worker: "shell"},
		{ID: "b", Worker: "shell"},
		{ID: "c", Worker: "shell", DependsOn: []string{"a", "b"}},
	}

	status := map[string]string{
		"a": "succeeded",
		"b": "running",
		"c": "pending",
	}
	got := pipeline.ReadyIDs(stages, status)
	if len(got) != 0 {
		t.Fatalf("ReadyIDs() = %v, want empty while b still running", got)
	}

	status["b"] = "succeeded"
	got = pipeline.ReadyIDs(stages, status)
	if !slices.Equal(got, []string{"c"}) {
		t.Fatalf("ReadyIDs() = %v, want [c]", got)
	}
}
