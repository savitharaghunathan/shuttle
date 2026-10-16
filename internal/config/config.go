package config

import (
	"fmt"
	"os"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

// WorkerPreset describes a config-driven worker argv template.
type WorkerPreset struct {
	Bin  string   `yaml:"bin,omitempty"`
	Args []string `yaml:"args,omitempty"`
}

// StageConfig holds a stage-level command override.
type StageConfig struct {
	Command []string `yaml:"command,omitempty"`
}

// Config is the top-level shuttle.yaml configuration.
type Config struct {
	DefaultWorker  string                  `yaml:"default_worker,omitempty"`
	ReadOnlyWorker string                  `yaml:"readonly_worker,omitempty"`
	WorkRoot       string                  `yaml:"work_root,omitempty"`
	Concurrency    int                     `yaml:"concurrency,omitempty"`
	DefaultTimeout time.Duration           `yaml:"default_timeout,omitempty"`
	Test           StageConfig             `yaml:"test,omitempty"`
	OpenPR         StageConfig             `yaml:"open_pr,omitempty"`
	Ship           StageConfig             `yaml:"ship,omitempty"`
	Workers        map[string]WorkerPreset `yaml:"workers,omitempty"`
}

type fileConfig struct {
	DefaultWorker  string                      `yaml:"default_worker"`
	ReadOnlyWorker string                      `yaml:"readonly_worker"`
	WorkRoot       string                      `yaml:"work_root"`
	Concurrency    int                         `yaml:"concurrency"`
	DefaultTimeout time.Duration               `yaml:"default_timeout"`
	Test           StageConfig                 `yaml:"test"`
	OpenPR         StageConfig                 `yaml:"open_pr"`
	Ship           StageConfig                 `yaml:"ship"`
	Workers        map[string]workerPresetYAML `yaml:"workers"`
}

type workerPresetYAML struct {
	Bin   string   `yaml:"bin"`
	Shell string   `yaml:"shell"`
	Args  []string `yaml:"args"`
}

// Load reads shuttle.yaml from path and applies design defaults for unset fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var raw fileConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg := applyDefaults(&raw)
	return cfg, nil
}

// LoadDefaults returns the design defaults without reading a shuttle.yaml file.
func LoadDefaults() *Config {
	return applyDefaults(&fileConfig{})
}

func applyDefaults(raw *fileConfig) *Config {
	cfg := &Config{
		DefaultWorker:  raw.DefaultWorker,
		ReadOnlyWorker: raw.ReadOnlyWorker,
		WorkRoot:       raw.WorkRoot,
		Concurrency:    raw.Concurrency,
		DefaultTimeout: raw.DefaultTimeout,
		Test:           raw.Test,
		OpenPR:         raw.OpenPR,
		Ship:           raw.Ship,
	}

	if cfg.DefaultWorker == "" {
		cfg.DefaultWorker = "claude"
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = ".shuttle"
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 2
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if len(cfg.Test.Command) == 0 {
		cfg.Test.Command = []string{"go", "test", "./..."}
	}
	if len(cfg.OpenPR.Command) == 0 {
		cfg.OpenPR.Command = []string{
			"{{.ShuttleBin}}", "pr", "open",
			"--branch", "{{.Branch}}",
			"--issue", "{{.IssueRef}}",
		}
	}
	if len(cfg.Ship.Command) == 0 {
		cfg.Ship.Command = []string{"echo", "ship skipped: configure ship.command"}
	}

	cfg.Workers = DefaultWorkerPresets()
	for name, preset := range raw.Workers {
		wp := WorkerPreset{Bin: preset.Bin, Args: preset.Args}
		if wp.Bin == "" && preset.Shell != "" {
			wp.Bin = preset.Shell
		}
		if wp.Bin != "" {
			cfg.Workers[name] = wp
		} else if len(wp.Args) > 0 {
			existing := cfg.Workers[name]
			existing.Args = wp.Args
			cfg.Workers[name] = existing
		}
	}

	return cfg
}

var nonProductionShipCommands = [][]string{
	{"echo", "ship skipped: configure ship.command"}, // LoadDefaults
	{"echo", "ship skipped"},                         // legacy scaffold
}

// IsNonProductionShipCommand reports whether the provided command matches
// known non-production (echo) ship command stubs used in defaults/scaffold.
func IsNonProductionShipCommand(cmd []string) bool {
	for _, stub := range nonProductionShipCommands {
		if slices.Equal(cmd, stub) {
			return true
		}
	}
	return false
}

func (c *Config) EffectiveReadOnlyWorker() string {
	if c.ReadOnlyWorker != "" {
		return c.ReadOnlyWorker
	}
	return c.DefaultWorker
}

func DefaultWorkerPresets() map[string]WorkerPreset {
	return map[string]WorkerPreset{
		"shell": {
			Bin: "/bin/bash",
		},
		"claude": {
			Bin: "claude",
			Args: []string{
				"{{if .Model}}--model{{end}}", "{{.Model}}",
				"-p", "{{.Prompt}}",
			},
		},
		"codex": {
			Bin: "codex",
			Args: []string{
				"{{if .Model}}--model{{end}}", "{{.Model}}",
				"exec", "{{.Prompt}}",
			},
		},
		"opencode": {
			Bin: "opencode",
			Args: []string{
				"{{if .Model}}--model{{end}}", "{{.Model}}",
				"run", "{{.Prompt}}",
			},
		},
		"cursor": {
			Bin: "cursor-agent",
			Args: []string{
				"--trust", "--yolo",
				"{{if .Model}}--model{{end}}", "{{.Model}}",
				"--print", "{{.Prompt}}",
			},
		},
	}
}
