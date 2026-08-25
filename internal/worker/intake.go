package worker

import (
	"context"

	"github.com/savitharaghunathan/shuttle/internal/artifact"
)

type IntakeWorker struct{}

func NewIntakeWorker() *IntakeWorker {
	return &IntakeWorker{}
}

func (w *IntakeWorker) Name() string {
	return "intake"
}

func (w *IntakeWorker) Execute(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusFailed}, err
	}
	if err := artifact.CopySpec(req.RunDir, req.SpecPath); err != nil {
		return Result{Status: StatusFailed}, err
	}

	return Result{
		Status:  StatusSucceeded,
		Summary: "copied spec",
		Outputs: map[string]string{
			"spec": "spec",
		},
	}, nil
}
