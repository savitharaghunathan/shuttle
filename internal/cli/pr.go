package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/repo"
	"github.com/spf13/cobra"
)

type ghRunner interface {
	Run(ctx context.Context, cwd string, args ...string) ([]byte, error)
}

type execGHRunner struct{}

func (execGHRunner) Run(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
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

func newPRCommand(out, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Manage pull requests",
	}
	cmd.AddCommand(newPROpenCommand(out, errOut, execGHRunner{}))
	return cmd
}

func newPROpenCommand(out, errOut io.Writer, runner ghRunner) *cobra.Command {
	_ = errOut
	var branch string
	var issueRef string
	var cwd string

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Create a pull request if one does not exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(cwd) == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				cwd = wd
			}
			if _, err := gogit.PlainOpen(cwd); err != nil {
				return fmt.Errorf("open git repo %s: %w", cwd, err)
			}

			ownerRepo := ""
			issueNumber := 0
			if strings.TrimSpace(issueRef) != "" {
				owner, name, number, err := issue.ParseRef(issueRef)
				if err != nil {
					return err
				}
				ownerRepo = fmt.Sprintf("%s/%s", owner, name)
				issueNumber = number
			} else {
				inferred, err := inferOwnerRepoFromOrigin(cwd)
				if err == nil {
					ownerRepo = inferred
				}
			}

			existingURL, found, err := findExistingOpenPR(cmd.Context(), runner, cwd, ownerRepo, branch)
			if err != nil {
				return err
			}
			if found {
				fmt.Fprintf(out, "pr: %s\n", existingURL)
				return nil
			}

			createdURL, err := createPR(cmd.Context(), runner, cwd, ownerRepo, branch, issueNumber)
			if err != nil {
				return err
			}
			if createdURL != "" {
				fmt.Fprintf(out, "pr: %s\n", createdURL)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "head branch to open PR from")
	_ = cmd.MarkFlagRequired("branch")
	cmd.Flags().StringVar(&issueRef, "issue", "", "issue reference owner/repo#N")
	cmd.Flags().StringVar(&cwd, "cwd", "", "repository path (defaults to current directory)")
	return cmd
}

type ghPRListItem struct {
	URL string `json:"url"`
}

func findExistingOpenPR(ctx context.Context, runner ghRunner, cwd, ownerRepo, branch string) (string, bool, error) {
	args := []string{"pr", "list", "--head", branch, "--state", "open"}
	if ownerRepo != "" {
		args = append(args, "-R", ownerRepo)
	}
	args = append(args, "--json", "url")
	out, err := runner.Run(ctx, cwd, args...)
	if err != nil {
		return "", false, fmt.Errorf("check existing pull request: %w", err)
	}

	var items []ghPRListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return "", false, fmt.Errorf("parse gh pr list output: %w", err)
	}
	if len(items) == 0 {
		return "", false, nil
	}
	return strings.TrimSpace(items[0].URL), true, nil
}

type ghIssueTitle struct {
	Title string `json:"title"`
}

func createPR(ctx context.Context, runner ghRunner, cwd, ownerRepo, branch string, issueNumber int) (string, error) {
	if issueNumber > 0 {
		args := []string{"issue", "view", strconv.Itoa(issueNumber), "-R", ownerRepo, "--json", "title"}
		out, err := runner.Run(ctx, cwd, args...)
		if err != nil {
			return "", fmt.Errorf("fetch issue title: %w", err)
		}
		var view ghIssueTitle
		if err := json.Unmarshal(out, &view); err != nil {
			return "", fmt.Errorf("parse gh issue output: %w", err)
		}

		createArgs := []string{
			"pr", "create",
			"--head", branch,
			"-R", ownerRepo,
			"--title", view.Title,
			"--body", fmt.Sprintf("Closes #%d", issueNumber),
		}
		out, err = runner.Run(ctx, cwd, createArgs...)
		if err != nil {
			return "", fmt.Errorf("create pull request: %w", err)
		}
		return firstLine(out), nil
	}

	createArgs := []string{"pr", "create", "--head", branch}
	if ownerRepo != "" {
		createArgs = append(createArgs, "-R", ownerRepo)
	}
	createArgs = append(createArgs, "--fill")

	out, err := runner.Run(ctx, cwd, createArgs...)
	if err != nil {
		return "", fmt.Errorf("create pull request: %w", err)
	}
	return firstLine(out), nil
}

func inferOwnerRepoFromOrigin(cwd string) (string, error) {
	originURL, err := repo.OriginRemoteURL(cwd)
	if err != nil {
		return "", err
	}
	owner, name, err := repo.ParseGitHubURL(originURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", owner, name), nil
}

func firstLine(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}

func filterEmptyFlags(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		cur := argv[i]
		if strings.HasPrefix(cur, "--") && i+1 < len(argv) && argv[i+1] == "" {
			i++
			continue
		}
		filtered = append(filtered, cur)
	}
	return filtered
}
