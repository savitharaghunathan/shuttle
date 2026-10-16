package repo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWriteShuttleYAMLIfAbsentSetsOpenPRCommand(t *testing.T) {
	dir := t.TempDir()
	testCmd := []string{"go", "test", "./..."}

	if err := WriteShuttleYAMLIfAbsent(dir, testCmd); err != nil {
		t.Fatalf("WriteShuttleYAMLIfAbsent error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "shuttle.yaml"))
	if err != nil {
		t.Fatalf("read shuttle.yaml: %v", err)
	}

	var cfg scaffoldConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal shuttle.yaml: %v", err)
	}

	want := []string{
		"{{.ShuttleBin}}", "pr", "open",
		"--branch", "{{.Branch}}",
		"--issue", "{{.IssueRef}}",
	}
	if !reflect.DeepEqual(cfg.OpenPR.Command, want) {
		t.Fatalf("OpenPR.Command=%v want %v", cfg.OpenPR.Command, want)
	}
	if cfg.ReadOnlyWorker != cfg.DefaultWorker || cfg.ReadOnlyWorker != "cursor" {
		t.Fatalf("ReadOnlyWorker=%q DefaultWorker=%q want both cursor", cfg.ReadOnlyWorker, cfg.DefaultWorker)
	}
}
