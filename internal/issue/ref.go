package issue

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ParseRef parses a stored issue ref of the form "owner/repo#N".
func ParseRef(ref string) (owner, repo string, num int, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", 0, fmt.Errorf("empty ref")
	}
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("malformed ref %q", ref)
	}
	left, right := parts[0], parts[1]
	slash := strings.SplitN(left, "/", 2)
	if len(slash) != 2 {
		return "", "", 0, fmt.Errorf("malformed ref %q", ref)
	}
	owner, repo = slash[0], slash[1]
	if !isValidName(owner) || !isValidName(repo) {
		return "", "", 0, fmt.Errorf("invalid owner or repo in %q", ref)
	}
	n, err := strconv.Atoi(right)
	if err != nil || n <= 0 {
		return "", "", 0, fmt.Errorf("invalid issue number in %q", ref)
	}
	return owner, repo, n, nil
}

func isValidName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// BranchName returns the deterministic branch name for either an issue ref or a run id.
func BranchName(issueRef, runID string) (string, error) {
	if issueRef != "" {
		_, _, num, err := ParseRef(issueRef)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("shuttle/issue-%d", num), nil
	}
	if runID == "" {
		return "", fmt.Errorf("either issueRef or runID must be provided")
	}
	// normalize runID by removing hyphens, then take first 8 chars
	short := strings.ReplaceAll(runID, "-", "")
	if len(short) < 8 {
		return "", fmt.Errorf("runID too short")
	}
	short = short[:8]
	return "shuttle/run-" + short, nil
}

// ResolveBranch returns the feature branch for a run.
// It walks the parent chain (via loadParent) until a root is found.
// If any entry in the chain contains an issueRef, that issue branch is returned.
func ResolveBranch(issueRef, runID, parentRunID string, loadParent func(id string) (string, string, string, error)) (string, error) {
	if issueRef != "" {
		return BranchName(issueRef, "")
	}
	// no issue on this run
	if parentRunID == "" {
		return BranchName("", runID)
	}
	if loadParent == nil {
		return "", fmt.Errorf("loadParent required to resolve parent_run_id")
	}
	curParent := parentRunID
	for curParent != "" {
		pIssue, pRun, pParent, err := loadParent(curParent)
		if err != nil {
			return "", fmt.Errorf("failed loading parent %q: %w", curParent, err)
		}
		if pIssue != "" {
			// parent is issue-driven
			return BranchName(pIssue, "")
		}
		if pParent == "" {
			// reached root; use its run id
			return BranchName("", pRun)
		}
		curParent = pParent
	}
	return "", fmt.Errorf("could not resolve branch")
}
