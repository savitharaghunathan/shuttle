package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/config"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shuttle.yaml")
	if err := os.WriteFile(path, []byte("default_worker: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkRoot != ".shuttle" {
		t.Fatalf("WorkRoot=%q want .shuttle", cfg.WorkRoot)
	}
	if cfg.Concurrency != 2 {
		t.Fatalf("Concurrency=%d want 2", cfg.Concurrency)
	}
	if len(cfg.Test.Command) < 2 || cfg.Test.Command[0] != "go" {
		t.Fatalf("Test.Command=%v", cfg.Test.Command)
	}
	if cfg.OpenPR.Command[0] != "echo" {
		t.Fatalf("OpenPR default should be echo, got %v", cfg.OpenPR.Command)
	}
}
