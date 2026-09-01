package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type ghRunner interface {
	Run(cwd string, args ...string) ([]byte, error)
}

type execGHRunner struct{}

func (execGHRunner) Run(cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "gh", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return out, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
		}
		return out, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return out, nil
}

type ghRunnerFunc func(cwd string, args ...string) ([]byte, error)

func (f ghRunnerFunc) Run(cwd string, args ...string) ([]byte, error) {
	return f(cwd, args...)
}

type ghPRListItem struct {
	URL string `json:"url"`
}

var preflightGHRunner ghRunner = execGHRunner{}

func GitPreflight(cwd, branch string) []string {
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
	if branch != "" {
		if _, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true); err == nil {
			warns = append(warns, fmt.Sprintf("branch %s already exists locally", branch))
		}

		out, err := preflightGHRunner.Run(cwd, "pr", "list", "--head", branch, "--state", "open", "--json", "url")
		if err != nil {
			warns = append(warns, fmt.Sprintf("could not check for open PR: %v", err))
		} else {
			var prs []ghPRListItem
			if err := json.Unmarshal(out, &prs); err != nil {
				warns = append(warns, fmt.Sprintf("could not check for open PR: parse gh pr list output: %v", err))
			} else if len(prs) > 0 && strings.TrimSpace(prs[0].URL) != "" {
				warns = append(warns, fmt.Sprintf("open PR already exists for branch %s: %s", branch, strings.TrimSpace(prs[0].URL)))
			}
		}
	}
	return warns
}
