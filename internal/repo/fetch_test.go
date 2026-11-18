package repo_test

import (
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/savitharaghunathan/shuttle/internal/repo"
)

func TestFetchAndCheckoutBranch(t *testing.T) {
	remoteDir := t.TempDir()
	localDir := t.TempDir()

	remote, err := git.PlainInit(remoteDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := remote.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCommit(wt, "README", "v1"); err != nil {
		t.Fatal(err)
	}
	// Create feature branch on remote and push a commit.
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("shuttle/issue-1"),
		Create: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeCommit(wt, "feature.txt", "from-actor"); err != nil {
		t.Fatal(err)
	}

	local, err := git.PlainClone(localDir, false, &git.CloneOptions{URL: remoteDir})
	if err != nil {
		t.Fatal(err)
	}
	// Ensure origin exists (clone sets it).
	if _, err := local.Remote("origin"); err != nil {
		t.Fatal(err)
	}
	_ = local.CreateBranch(&config.Branch{Name: "main", Remote: "origin", Merge: "refs/heads/master"})

	if err := repo.FetchAndCheckoutBranch(localDir, "shuttle/issue-1"); err != nil {
		t.Fatal(err)
	}
	head, err := local.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Name().Short() != "shuttle/issue-1" {
		t.Fatalf("head=%s", head.Name().Short())
	}
}

func writeCommit(wt *git.Worktree, name, content string) error {
	f, err := wt.Filesystem.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		return err
	}
	_ = f.Close()
	if _, err := wt.Add(name); err != nil {
		return err
	}
	_, err = wt.Commit("msg", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
	})
	return err
}
