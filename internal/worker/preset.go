package worker

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/savitharaghunathan/shuttle/internal/config"
)

type PresetWorker struct {
	name   string
	preset config.WorkerPreset
}

func NewPresetWorker(name string, preset config.WorkerPreset) *PresetWorker {
	return &PresetWorker{name: name, preset: preset}
}

func (w *PresetWorker) Name() string {
	return w.name
}

func (w *PresetWorker) Execute(ctx context.Context, req Request) (Result, error) {
	if w.preset.Bin == "" {
		return Result{Status: StatusFailed}, fmt.Errorf("worker %q bin is not configured", w.name)
	}

	args := make([]string, 0, len(w.preset.Args))
	for i, arg := range w.preset.Args {
		rendered, err := renderPresetArg(arg, req)
		if err != nil {
			return Result{Status: StatusFailed}, fmt.Errorf("worker %q render arg %d: %w", w.name, i, err)
		}
		if rendered == "" {
			continue
		}
		args = append(args, rendered)
	}
	if req.ReadOnly {
		args = dropYolo(args)
	}

	argv := make([]string, 0, 1+len(args))
	argv = append(argv, w.preset.Bin)
	argv = append(argv, args...)
	res, err := runArgv(ctx, req, argv)
	if err != nil {
		return res, fmt.Errorf("worker %q: %w", w.name, err)
	}
	return res, nil
}

func dropYolo(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--yolo" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func renderPresetArg(arg string, req Request) (string, error) {
	tmpl, err := template.New("arg").Parse(arg)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, req); err != nil {
		return "", err
	}
	return out.String(), nil
}
