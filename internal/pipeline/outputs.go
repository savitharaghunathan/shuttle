package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/savitharaghunathan/shuttle/internal/worker"
)

func ensureOutputDirs(runDir, stageID string, outputs []string) error {
	if len(outputs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(runDir, "artifacts", stageID), 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", stageID, err)
	}
	return nil
}

func finalizeStageOutputs(runDir, stageID string, outputs []string, result *worker.Result, strict bool) error {
	if len(outputs) == 0 {
		return nil
	}
	outDir := filepath.Join(runDir, "artifacts", stageID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", stageID, err)
	}

	summary := ""
	if result != nil {
		summary = result.Summary
	}

	for _, name := range outputs {
		path := filepath.Join(outDir, name)
		ok, err := nonEmptyFile(path)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if strings.TrimSpace(summary) != "" {
			if err := os.WriteFile(path, []byte(summary), 0o644); err != nil {
				return fmt.Errorf("write bridged output %s/%s: %w", stageID, name, err)
			}
			continue
		}
		if strict {
			return fmt.Errorf("missing declared output %s/%s: worker produced no file and no stdout summary", stageID, name)
		}
	}
	return nil
}

func nonEmptyFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat output %s: %w", path, err)
	}
	return !info.IsDir() && info.Size() > 0, nil
}

// IsStrictOutputTemplate reports whether a template enforces declared stage
// outputs. Only the embedded SDLC templates are strict; local overlays that
// copy them keep the permissive stdout bridge.
func IsStrictOutputTemplate(id string) bool {
	switch id {
	case "sdlc", "sdlc-headless":
		return true
	default:
		return false
	}
}

// OutputsMode reports how declared stage outputs are enforced: "strict" fails a
// stage that produced no artifact, "bridge" falls back to worker stdout. It
// returns "" when the pipeline declares no outputs.
func OutputsMode(templateID string, pl *Pipeline) string {
	if pl == nil {
		return ""
	}
	declaresOutputs := false
	for _, stage := range pl.Stages {
		if len(stage.Outputs) > 0 {
			declaresOutputs = true
			break
		}
	}
	if !declaresOutputs {
		return ""
	}
	if IsStrictOutputTemplate(templateID) {
		return "strict"
	}
	return "bridge"
}

func shuttleStageEnv(runDir, stageID string) map[string]string {
	return map[string]string{
		"SHUTTLE_RUN_DIR":  runDir,
		"SHUTTLE_STAGE_ID": stageID,
	}
}
