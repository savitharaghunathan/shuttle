package repo

import (
	"fmt"

	git "github.com/go-git/go-git/v5"
)

// OriginRemoteURL returns the URL configured for the origin remote.
// Uses go-git (Apache-2.0); does not shell out to the git CLI.
func OriginRemoteURL(repoPath string) (string, error) {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", fmt.Errorf("open git repo %s: %w", repoPath, err)
	}
	remote, err := r.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("git remote origin in %s: %w", repoPath, err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 || urls[0] == "" {
		return "", fmt.Errorf("empty origin remote in %s", repoPath)
	}
	return urls[0], nil
}
