package worker

import (
	"context"
	"io"
	"sort"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/config"
)

type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type Request struct {
	RunID    string
	StageID  string
	WorkDir  string
	RunDir   string
	Prompt   string
	SpecPath string
	Command  []string
	Env      map[string]string
	Timeout  time.Duration
	Agent    string
	Model    string
	Output   io.Writer
	LogPath  string
}

type Result struct {
	Status  Status
	Summary string
	Outputs map[string]string
}

type Worker interface {
	Name() string
	Execute(ctx context.Context, req Request) (Result, error)
}

type Registry struct {
	workers map[string]Worker
}

func NewRegistry(cfg *config.Config) *Registry {
	presets := config.DefaultWorkerPresets()
	if cfg != nil {
		for name, preset := range cfg.Workers {
			presets[name] = preset
		}
	}

	workers := map[string]Worker{
		"shell":  NewShellWorker(presets["shell"]),
		"exec":   NewExecWorker(),
		"intake": NewIntakeWorker(),
	}
	for _, name := range []string{"claude", "codex", "opencode", "cursor"} {
		workers[name] = NewPresetWorker(name, presets[name])
	}

	return &Registry{workers: workers}
}

func (r *Registry) Get(name string) (Worker, bool) {
	w, ok := r.workers[name]
	return w, ok
}

func (r *Registry) Register(name string, w Worker) {
	r.workers[name] = w
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.workers))
	for name := range r.workers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
