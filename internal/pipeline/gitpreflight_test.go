package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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
	warns := GitPreflight(dir, "")
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
	warns := GitPreflight(dir, "")
	if len(warns) != 0 {
		t.Fatalf("want no warnings on clean default branch, got %v", warns)
	}
}

func TestGitPreflightWarnsWhenTargetBranchExistsLocally(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt")
	_, _ = wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("shuttle/issue-42"),
		Create: true,
	}); err != nil {
		t.Fatal(err)
	}
	warns := GitPreflight(dir, "shuttle/issue-42")
	want := "branch shuttle/issue-42 already exists locally"
	if !containsWarning(warns, want) {
		t.Fatalf("warnings=%v want %q", warns, want)
	}
}

func TestGitPreflightWarnsOnOpenPRAndContinuesOnGHError(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	must(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644))
	_, _ = wt.Add("f.txt")
	_, _ = wt.Commit("init", &gogit.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})

	origRunner := preflightGHRunner
	t.Cleanup(func() { preflightGHRunner = origRunner })
	preflightGHRunner = ghRunnerFunc(func(cwd string, args ...string) ([]byte, error) {
		return []byte(`[{"url":"https://github.com/acme/widget/pull/9"}]`), nil
	})

	warns := GitPreflight(dir, "shuttle/issue-42")
	wantPR := "open PR already exists for branch shuttle/issue-42: https://github.com/acme/widget/pull/9"
	if !containsWarning(warns, wantPR) {
		t.Fatalf("warnings=%v want %q", warns, wantPR)
	}

	preflightGHRunner = ghRunnerFunc(func(cwd string, args ...string) ([]byte, error) {
		return nil, assertErr("gh unavailable")
	})
	warns = GitPreflight(dir, "shuttle/issue-42")
	wantGH := "could not check for open PR: gh unavailable"
	if !containsWarning(warns, wantGH) {
		t.Fatalf("warnings=%v want %q", warns, wantGH)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func containsWarning(warns []string, want string) bool {
	for _, w := range warns {
		if strings.Contains(w, want) {
			return true
		}
	}
	return false
}
