package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirDefaultsToHomeShuttle(t *testing.T) {
	t.Setenv("SHUTTLE_HOME", "")
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(userHome, ".shuttle")
	if got := Dir(); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestDirRespectsShuttleHomeEnv(t *testing.T) {
	t.Setenv("SHUTTLE_HOME", "/tmp/custom-shuttle")
	if got := Dir(); got != "/tmp/custom-shuttle" {
		t.Fatalf("Dir() = %q, want /tmp/custom-shuttle", got)
	}
}

func TestReposDirUnderHome(t *testing.T) {
	t.Setenv("SHUTTLE_HOME", "/tmp/sh")
	if got := ReposDir(); got != "/tmp/sh/repos" {
		t.Fatalf("ReposDir() = %q", got)
	}
	if !strings.HasSuffix(RegistryDB(), filepath.Join("sh", "registry.db")) {
		t.Fatalf("RegistryDB() = %q", RegistryDB())
	}
}
