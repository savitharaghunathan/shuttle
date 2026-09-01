package pipeline

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

var (
	isGitRepo = func(cwd string) bool {
		_, err := gogit.PlainOpen(cwd)
		return err == nil
	}
	verifyRemoteBranch = VerifyRemoteBranch
)

// VerifyRemoteBranch checks whether refs/heads/<branch> exists on the remote.
// It prefers origin and falls back to the first configured remote.
// Any returned error means verification was inconclusive (auth/network/etc).
func VerifyRemoteBranch(cwd, branch string) (bool, error) {
	r, err := gogit.PlainOpen(cwd)
	if err != nil {
		return false, fmt.Errorf("open git repo: %w", err)
	}

	remote, err := r.Remote("origin")
	if err != nil {
		remotes, remotesErr := r.Remotes()
		if remotesErr != nil {
			return false, fmt.Errorf("list remotes: %w", remotesErr)
		}
		if len(remotes) == 0 {
			return false, fmt.Errorf("no git remotes configured")
		}
		remote = remotes[0]
	}

	refs, err := remote.List(&gogit.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("list remote refs for %s: %w", remote.Config().Name, err)
	}

	target := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == target {
			return true, nil
		}
	}
	return false, nil
}
