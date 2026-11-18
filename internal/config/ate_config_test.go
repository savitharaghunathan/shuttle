package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/config"
)

func TestLoadAteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shuttle.yaml")
	content := `
default_worker: claude
ate:
  endpoint: localhost:8443
  atespace: shuttle
  actor_template: shuttle-claude
  task_url: http://127.0.0.1:8000
  insecure: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Ate.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Ate.Atespace != "shuttle" || !cfg.Ate.Insecure {
		t.Fatalf("%+v", cfg.Ate)
	}
}
