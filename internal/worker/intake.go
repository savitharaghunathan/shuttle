package worker

import (
	"context"
	"fmt"

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

	summary := "copied spec"
	if req.SpecPath != "" {
		if err := artifact.CopySpec(req.RunDir, req.SpecPath); err != nil {
			return Result{Status: StatusFailed}, err
		}
	} else if !artifact.SpecReady(req.RunDir) {
		return Result{Status: StatusFailed}, fmt.Errorf("spec not provided and not present in run dir")
	} else {
		summary = "spec ready"
	}

	return Result{
		Status:  StatusSucceeded,
		Summary: summary,
		Outputs: map[string]string{
			"spec": "spec",
		},
	}, nil
}
