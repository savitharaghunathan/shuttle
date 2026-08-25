package artifact

import (
	"fmt"
	"os"
	"path/filepath"
)

func InitRun(workRoot, runID string) (string, error) {
	runDir := filepath.Join(workRoot, "runs", runID)
	for _, sub := range []string{"inputs", "artifacts", "logs", "approvals"} {
		if err := os.MkdirAll(filepath.Join(runDir, sub), 0o755); err != nil {
			return "", fmt.Errorf("create run dir %s: %w", sub, err)
		}
	}
	return runDir, nil
}

func CopySpec(runDir, specPath string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read spec %s: %w", specPath, err)
	}
	return WriteSpec(runDir, data)
}

func WriteSpec(runDir string, data []byte) error {
	inputsPath := filepath.Join(runDir, "inputs", "spec.md")
	if err := os.WriteFile(inputsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", inputsPath, err)
	}

	artifactsPath := filepath.Join(runDir, "artifacts", "spec")
	if err := os.WriteFile(artifactsPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", artifactsPath, err)
	}

	return nil
}

// SpecReady reports whether intake spec artifacts already exist in the run dir.
func SpecReady(runDir string) bool {
	_, err := os.Stat(filepath.Join(runDir, "inputs", "spec.md"))
	return err == nil
}
