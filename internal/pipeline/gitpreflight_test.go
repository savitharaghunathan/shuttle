package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestGitPreflightDirty(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt") // staged but uncommitted → dirty
	warns := GitPreflight(dir)
	if len(warns) == 0 {
		t.Fatalf("want dirty warning")
	}
}

func TestGitPreflightCleanMain(t *testing.T) {
	dir := t.TempDir()
	repo, _ := gogit.PlainInit(dir, false)
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt")
	_, _ = wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	// default branch after init is "master" (go-git) — treated as default
	warns := GitPreflight(dir)
	if len(warns) != 0 {
		t.Fatalf("want no warnings on clean default branch, got %v", warns)
	}
}
