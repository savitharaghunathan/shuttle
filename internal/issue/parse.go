package issue

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseIssueURL extracts owner, repo, and issue number from a GitHub issue URL.
func ParseIssueURL(raw string) (owner, repo string, number int, err error) {
	raw = strings.TrimSpace(raw)
	const prefix = "https://github.com/"
	if !strings.HasPrefix(raw, prefix) {
		return "", "", 0, fmt.Errorf("unsupported issue url %q (expected https://github.com/owner/repo/issues/N)", raw)
	}
	path := strings.TrimPrefix(raw, prefix)
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[2] != "issues" {
		return "", "", 0, fmt.Errorf("unsupported issue url %q", raw)
	}
	number, err = strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("invalid issue number in %q", raw)
	}
	return parts[0], parts[1], number, nil
}
