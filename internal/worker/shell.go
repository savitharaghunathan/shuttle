package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/savitharaghunathan/shuttle/internal/config"
)

type ShellWorker struct {
	preset config.WorkerPreset
}

func NewShellWorker(preset config.WorkerPreset) *ShellWorker {
	return &ShellWorker{preset: preset}
}

func (w *ShellWorker) Name() string {
	return "shell"
}

func (w *ShellWorker) Execute(ctx context.Context, req Request) (Result, error) {
	return runArgv(ctx, req, fallbackCommand(req.Command, w.preset))
}

func fallbackCommand(command []string, preset config.WorkerPreset) []string {
	if len(command) > 0 {
		return command
	}
	if preset.Bin == "" {
		return nil
	}

	argv := make([]string, 0, 1+len(preset.Args))
	argv = append(argv, preset.Bin)
	argv = append(argv, preset.Args...)
	return argv
}

func runArgv(ctx context.Context, req Request, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{Status: StatusFailed}, fmt.Errorf("worker command is empty")
	}

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.WorkDir
	cmd.Env = os.Environ()
	for key, value := range req.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, err := cmd.CombinedOutput()
	result := Result{
		Status:  StatusSucceeded,
		Summary: string(output),
	}
	if err == nil {
		return result, nil
	}

	result.Status = StatusFailed
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if _, ok := err.(*exec.ExitError); ok {
		return result, nil
	}
	return result, err
}
