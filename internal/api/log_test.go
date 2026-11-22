package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStageLogAllowsUnderRunDir(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(runDir, "stage.log")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, trunc, err := readStageLog(runDir, p, 0)
	if err != nil || trunc || string(body) != "hello\nworld\n" {
		t.Fatalf("body=%q trunc=%v err=%v", body, trunc, err)
	}
}

func TestReadStageLogRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	outside := filepath.Join(dir, "secret.log")
	_ = os.WriteFile(outside, []byte("nope"), 0o644)
	_, _, err := readStageLog(runDir, outside, 0)
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestReadStageLogRejectsMissingEscapeBeforeNotExist(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	outside := filepath.Join(dir, "missing.log")

	_, _, err := readStageLog(runDir, outside, 0)
	if err == nil {
		t.Fatal("expected escape error")
	}
	if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		t.Fatalf("expected escape error, got not-exist error: %v", err)
	}
}

func TestReadStageLogRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	outside := filepath.Join(dir, "secret.log")
	_ = os.WriteFile(outside, []byte("nope"), 0o644)
	link := filepath.Join(runDir, "linked.log")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, _, err := readStageLog(runDir, link, 0)
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
}

func TestReadStageLogTailAndCap(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	p := filepath.Join(runDir, "stage.log")
	_ = os.WriteFile(p, []byte("a\nb\nc\n"), 0o644)
	body, _, err := readStageLog(runDir, p, 2)
	if err != nil || string(body) != "b\nc\n" {
		t.Fatalf("tail got %q err=%v", body, err)
	}
	big := strings.Repeat("x", MaxLogBytes+10)
	_ = os.WriteFile(p, []byte(big), 0o644)
	body, trunc, err := readStageLog(runDir, p, 0)
	if err != nil || !trunc || len(body) != MaxLogBytes {
		t.Fatalf("cap len=%d trunc=%v err=%v", len(body), trunc, err)
	}
}
