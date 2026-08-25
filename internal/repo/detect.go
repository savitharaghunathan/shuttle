package repo

import (
	"os"
	"path/filepath"
)

// DetectTestCommand returns a sensible test command based on project markers.
func DetectTestCommand(repoPath string) []string {
	switch {
	case fileExists(filepath.Join(repoPath, "go.mod")):
		return []string{"go", "test", "./..."}
	case fileExists(filepath.Join(repoPath, "package.json")):
		return []string{"npm", "test"}
	case fileExists(filepath.Join(repoPath, "pyproject.toml")):
		return []string{"python", "-m", "pytest"}
	default:
		return []string{"true"}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
