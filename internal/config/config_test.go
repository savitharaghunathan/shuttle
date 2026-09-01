package config_test

import (
	"os"
	"path/filepath"
	"reflect"
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
	wantOpenPR := []string{
		"{{.ShuttleBin}}", "pr", "open",
		"--branch", "{{.Branch}}",
		"--issue", "{{.IssueRef}}",
	}
	if !reflect.DeepEqual(cfg.OpenPR.Command, wantOpenPR) {
		t.Fatalf("OpenPR.Command=%v want %v", cfg.OpenPR.Command, wantOpenPR)
	}
}

func TestLoadDefaultsOpenPRCommand(t *testing.T) {
	cfg := config.LoadDefaults()
	wantOpenPR := []string{
		"{{.ShuttleBin}}", "pr", "open",
		"--branch", "{{.Branch}}",
		"--issue", "{{.IssueRef}}",
	}
	if !reflect.DeepEqual(cfg.OpenPR.Command, wantOpenPR) {
		t.Fatalf("OpenPR.Command=%v want %v", cfg.OpenPR.Command, wantOpenPR)
	}
}
