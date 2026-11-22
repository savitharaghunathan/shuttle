package ate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsResumeParkingHTTP(t *testing.T) {
	if !ate.IsResumeParkingHTTP(503, []byte("actor is still resuming, retry")) {
		t.Fatal("expected true for still resuming")
	}
	if ate.IsResumeParkingHTTP(503, []byte("credential provider slow, retry")) {
		t.Fatal("must not match bare retry")
	}
	if ate.IsResumeParkingHTTP(502, []byte("still resuming")) {
		t.Fatal("must require 503")
	}
}

func TestIsResumeParkingHTTPCaseInsensitive(t *testing.T) {
	if !ate.IsResumeParkingHTTP(503, []byte("Actor Is Still Resuming")) {
		t.Fatal("expected case-insensitive match")
	}
}

func TestIsRetryableResume(t *testing.T) {
	if !ate.IsRetryableResume(status.Error(codes.ResourceExhausted, "no room")) {
		t.Fatal("RESOURCE_EXHAUSTED")
	}
	if !ate.IsRetryableResume(status.Error(codes.Aborted, "in flight")) {
		t.Fatal("ABORTED")
	}
	if ate.IsRetryableResume(status.Error(codes.Unavailable, "empty")) {
		t.Fatal("must not retry Unavailable")
	}
	if ate.IsRetryableResume(errors.New("plain")) {
		t.Fatal("must not retry plain errors")
	}
}

func TestBackoffWaitRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := ate.Backoff{Initial: time.Second, Max: time.Second, Factor: 2}
	if err := b.Wait(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
