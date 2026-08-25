package home

import (
	"os"
	"path/filepath"
)

const envShuttleHome = "SHUTTLE_HOME"

// Dir returns the global Shuttle home directory (~/.shuttle or SHUTTLE_HOME).
func Dir() string {
	if v := os.Getenv(envShuttleHome); v != "" {
		return v
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ".shuttle"
	}
	return filepath.Join(userHome, ".shuttle")
}

// ReposDir returns where imported git clones are stored.
func ReposDir() string {
	return filepath.Join(Dir(), "repos")
}

// RegistryDB returns the path to the global repo registry database.
func RegistryDB() string {
	return filepath.Join(Dir(), "registry.db")
}

// RepoPath returns the checkout path for owner/name under ReposDir.
func RepoPath(owner, name string) string {
	return filepath.Join(ReposDir(), owner, name)
}
