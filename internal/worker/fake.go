package worker

import (
	"context"
	"fmt"
	"time"
)

type FakeWorker struct {
	results map[string]Result
	Delay   time.Duration
}

func NewFakeWorker(results map[string]Result) *FakeWorker {
	return &FakeWorker{results: results}
}

func (w *FakeWorker) Name() string {
	return "fake"
}

func (w *FakeWorker) Execute(ctx context.Context, req Request) (Result, error) {
	if w.Delay > 0 {
		select {
		case <-time.After(w.Delay):
		case <-ctx.Done():
			return Result{Status: StatusFailed}, ctx.Err()
		}
	}
	result, ok := w.results[req.StageID]
	if !ok {
		return Result{Status: StatusFailed}, fmt.Errorf("fake result for stage %q not found", req.StageID)
	}
	return result, nil
}
