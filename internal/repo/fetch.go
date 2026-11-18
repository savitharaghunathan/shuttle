package repo

import (
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// FetchAndCheckoutBranch fetches origin and checks out refs/heads/<branch>
// matching origin/<branch>. Uses go-git; does not shell out to the git CLI.
func FetchAndCheckoutBranch(repoPath, branch string) error {
	if branch == "" {
		return fmt.Errorf("branch is required")
	}
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open git repo %s: %w", repoPath, err)
	}

	err = r.Fetch(&git.FetchOptions{RemoteName: "origin"})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("fetch origin in %s: %w", repoPath, err)
	}

	remoteRefName := plumbing.NewRemoteReferenceName("origin", branch)
	remoteRef, err := r.Reference(remoteRefName, true)
	if err != nil {
		return fmt.Errorf("resolve %s after fetch: %w", remoteRefName, err)
	}

	localRefName := plumbing.NewBranchReferenceName(branch)
	if err := r.Storer.SetReference(plumbing.NewHashReference(localRefName, remoteRef.Hash())); err != nil {
		return fmt.Errorf("set local ref %s: %w", localRefName, err)
	}

	// Ensure local branch tracks origin/<branch> when possible.
	_ = r.CreateBranch(&config.Branch{
		Name:   branch,
		Remote: "origin",
		Merge:  localRefName,
	})

	wt, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Branch: localRefName, Force: true}); err != nil {
		return fmt.Errorf("checkout %s: %w", branch, err)
	}
	return nil
}
