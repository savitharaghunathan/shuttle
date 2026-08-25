package worker

import (
	"context"
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
	preset := config.WorkerPreset{}
	if cfg != nil {
		preset = cfg.Workers["shell"]
	}

	return &Registry{
		workers: map[string]Worker{
			"shell":  NewShellWorker(preset),
			"exec":   NewExecWorker(),
			"intake": NewIntakeWorker(),
		},
	}
}

func (r *Registry) Get(name string) (Worker, bool) {
	w, ok := r.workers[name]
	return w, ok
}
