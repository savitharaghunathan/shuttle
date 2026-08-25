package worker

import "context"

type ExecWorker struct{}

func NewExecWorker() *ExecWorker {
	return &ExecWorker{}
}

func (w *ExecWorker) Name() string {
	return "exec"
}

func (w *ExecWorker) Execute(ctx context.Context, req Request) (Result, error) {
	return runArgv(ctx, req, req.Command)
}
