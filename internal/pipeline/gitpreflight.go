package pipeline

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
)

func GitPreflight(cwd string) []string {
	var warns []string
	repo, err := gogit.PlainOpen(cwd)
	if err != nil {
		return nil // not a git repo (spec-only run) → no warnings
	}
	if wt, err := repo.Worktree(); err == nil {
		if st, err := wt.Status(); err == nil && !st.IsClean() {
			warns = append(warns, fmt.Sprintf("uncommitted changes in %s", cwd))
		}
	}
	if head, err := repo.Head(); err == nil && head.Name().IsBranch() {
		branch := head.Name().Short()
		if branch != "main" && branch != "master" {
			warns = append(warns, fmt.Sprintf("on non-default branch %s", branch))
		}
	}
	return warns
}
