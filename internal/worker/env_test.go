package worker

import (
	"strings"
	"testing"
)

func TestMergeProcessEnvOverridesDuplicateKeys(t *testing.T) {
	t.Setenv("SHUTTLE_RUN_DIR", "/wrong")
	t.Setenv("SHUTTLE_STAGE_ID", "wrong-stage")

	env := mergeProcessEnv(map[string]string{
		"SHUTTLE_RUN_DIR":  "/correct",
		"SHUTTLE_STAGE_ID": "correct-stage",
	})

	values := map[string][]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = append(values[key], value)
	}

	if got := values["SHUTTLE_RUN_DIR"]; len(got) != 1 || got[0] != "/correct" {
		t.Fatalf("SHUTTLE_RUN_DIR values = %v, want [/correct]", got)
	}
	if got := values["SHUTTLE_STAGE_ID"]; len(got) != 1 || got[0] != "correct-stage" {
		t.Fatalf("SHUTTLE_STAGE_ID values = %v, want [correct-stage]", got)
	}
}
