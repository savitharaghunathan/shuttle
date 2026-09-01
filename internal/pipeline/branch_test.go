package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestVerifyRemoteBranchFound(t *testing.T) {
	localDir := setupRepoWithOrigin(t, "shuttle/issue-42")
	found, err := VerifyRemoteBranch(localDir, "shuttle/issue-42")
	if err != nil {
		t.Fatalf("VerifyRemoteBranch returned error: %v", err)
	}
	if !found {
		t.Fatal("expected branch to be found on remote")
	}
}

func TestVerifyRemoteBranchAbsent(t *testing.T) {
	localDir := setupRepoWithOrigin(t, "shuttle/issue-7")
	found, err := VerifyRemoteBranch(localDir, "shuttle/issue-42")
	if err != nil {
		t.Fatalf("VerifyRemoteBranch returned error: %v", err)
	}
	if found {
		t.Fatal("expected branch to be absent on remote")
	}
}

func TestVerifyRemoteBranchInconclusiveOnRemoteListError(t *testing.T) {
	localDir := t.TempDir()
	repo, err := gogit.PlainInit(localDir, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{filepath.Join(localDir, "missing-remote.git")},
	})
	if err != nil {
		t.Fatal(err)
	}

	found, verifyErr := VerifyRemoteBranch(localDir, "shuttle/issue-42")
	if verifyErr == nil {
		t.Fatal("expected remote list error")
	}
	if found {
		t.Fatal("found=true with error should not happen")
	}
}

func setupRepoWithOrigin(t *testing.T, branchToPush string) string {
	t.Helper()

	remoteDir := filepath.Join(t.TempDir(), "origin.git")
	if _, err := gogit.PlainInit(remoteDir, true); err != nil {
		t.Fatal(err)
	}

	localDir := t.TempDir()
	repo, err := gogit.PlainInit(localDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("seed", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteDir},
	}); err != nil {
		t.Fatal(err)
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchToPush),
		Create: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "branch.txt"), []byte(branchToPush), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("branch.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("branch commit", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	src := plumbing.NewBranchReferenceName(branchToPush)
	dst := plumbing.NewBranchReferenceName(branchToPush)
	if err := repo.Push(&gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{gitconfig.RefSpec(src.String() + ":" + dst.String())},
	}); err != nil {
		t.Fatal(err)
	}

	return localDir
}
