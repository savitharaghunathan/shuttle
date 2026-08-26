package workertest_test

import (
	"context"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/savitharaghunathan/shuttle/internal/worker/workertest"
)

func TestFakeExecuteReturnsStageResult(t *testing.T) {
	w := workertest.NewFakeWorker(map[string]worker.Result{
		"test": {Status: worker.StatusSucceeded, Summary: "done"},
	})

	res, err := w.Execute(context.Background(), worker.Request{StageID: "test"})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if res.Summary != "done" {
		t.Fatalf("Summary = %q, want done", res.Summary)
	}
}

func TestFakeExecuteUnknownStageFails(t *testing.T) {
	w := workertest.NewFakeWorker(map[string]worker.Result{})

	res, err := w.Execute(context.Background(), worker.Request{StageID: "missing"})
	if err == nil {
		t.Fatal("Execute() error = nil, want not-found error")
	}
	if res.Status != worker.StatusFailed {
		t.Fatalf("Status = %q, want %q", res.Status, worker.StatusFailed)
	}
}
