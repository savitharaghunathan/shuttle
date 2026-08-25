package worker

import (
	"context"
	"fmt"
)

type FakeWorker struct {
	results map[string]Result
}

func NewFakeWorker(results map[string]Result) *FakeWorker {
	return &FakeWorker{results: results}
}

func (w *FakeWorker) Name() string {
	return "fake"
}

func (w *FakeWorker) Execute(_ context.Context, req Request) (Result, error) {
	result, ok := w.results[req.StageID]
	if !ok {
		return Result{Status: StatusFailed}, fmt.Errorf("fake result for stage %q not found", req.StageID)
	}
	return result, nil
}
