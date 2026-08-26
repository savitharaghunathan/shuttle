package cli

import (
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

func TestParseSince(t *testing.T) {
	now := time.Now()
	got, err := parseSince("7d")
	if err != nil || now.Sub(got) < 6*24*time.Hour {
		t.Fatalf("7d=%v err=%v", got, err)
	}
	if _, err := parseSince("24h"); err != nil {
		t.Fatalf("24h err=%v", err)
	}
	if got2, err := parseSince("2026-08-01"); err != nil || got2.Year() != 2026 {
		t.Fatalf("date=%v err=%v", got2, err)
	}
	if _, err := parseSince("garbage"); err == nil {
		t.Fatalf("garbage should error")
	}
}

func TestParseStatuses(t *testing.T) {
	got := parseStatuses("failed,running")
	if len(got) != 2 || got[0] != store.RunStatusFailed || got[1] != store.RunStatusRunning {
		t.Fatalf("got=%v", got)
	}
}
