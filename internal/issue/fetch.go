package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ghIssueView struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FetchSpec fetches an issue via gh and returns markdown suitable for intake.
func FetchSpec(ctx context.Context, owner, repo string, number int) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "issue", "view", fmt.Sprintf("%d", number),
		"-R", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "title,body",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh issue view: %w", err)
	}

	var view ghIssueView
	if err := json.Unmarshal(out, &view); err != nil {
		return "", fmt.Errorf("parse gh output: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", view.Title)
	b.WriteString(strings.TrimSpace(view.Body))
	b.WriteByte('\n')
	return b.String(), nil
}
