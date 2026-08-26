// Package workertest provides test doubles for the worker package.
// It is imported only from _test.go files, so it is never compiled into
// the production binary.
package workertest

import (
	"context"
	"fmt"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/worker"
)

// FakeWorker returns canned results keyed by stage ID, standing in for real
// agent runtimes so pipeline, engine, and API tests run deterministically.
type FakeWorker struct {
	results map[string]worker.Result
	Delay   time.Duration
}

func NewFakeWorker(results map[string]worker.Result) *FakeWorker {
	return &FakeWorker{results: results}
}

func (w *FakeWorker) Name() string {
	return "fake"
}

func (w *FakeWorker) Execute(ctx context.Context, req worker.Request) (worker.Result, error) {
	if w.Delay > 0 {
		select {
		case <-time.After(w.Delay):
		case <-ctx.Done():
			return worker.Result{Status: worker.StatusFailed}, ctx.Err()
		}
	}
	result, ok := w.results[req.StageID]
	if !ok {
		return worker.Result{Status: worker.StatusFailed}, fmt.Errorf("fake result for stage %q not found", req.StageID)
	}
	return result, nil
}
