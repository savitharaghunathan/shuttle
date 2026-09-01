package pipeline

import (
	"errors"
	"testing"
)

func TestShuttleBinPrefersEnv(t *testing.T) {
	t.Setenv("SHUTTLE_BIN", "/tmp/custom-shuttle")
	orig := osExecutable
	osExecutable = func() (string, error) { return "/tmp/current", nil }
	t.Cleanup(func() { osExecutable = orig })

	if got := ShuttleBin(); got != "/tmp/custom-shuttle" {
		t.Fatalf("ShuttleBin() = %q, want /tmp/custom-shuttle", got)
	}
}

func TestShuttleBinUsesExecutable(t *testing.T) {
	t.Setenv("SHUTTLE_BIN", "")
	orig := osExecutable
	osExecutable = func() (string, error) { return "/tmp/current-shuttle", nil }
	t.Cleanup(func() { osExecutable = orig })

	if got := ShuttleBin(); got != "/tmp/current-shuttle" {
		t.Fatalf("ShuttleBin() = %q, want /tmp/current-shuttle", got)
	}
}

func TestShuttleBinFallsBackToCommand(t *testing.T) {
	t.Setenv("SHUTTLE_BIN", "")
	orig := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { osExecutable = orig })

	if got := ShuttleBin(); got != "shuttle" {
		t.Fatalf("ShuttleBin() = %q, want shuttle", got)
	}
}
