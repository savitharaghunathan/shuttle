package repo

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseGitHubURL extracts owner and repo name from common GitHub URL forms.
func ParseGitHubURL(raw string) (owner, name string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty github url")
	}

	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "git@") {
		return parseRemoteURL(raw)
	}

	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) == 2 && !strings.Contains(parts[0], ".") {
		return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
	}

	return "", "", fmt.Errorf("unsupported github url %q", raw)
}

func parseRemoteURL(raw string) (owner, name string, err error) {
	if strings.HasPrefix(raw, "git@github.com:") {
		path := strings.TrimPrefix(raw, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("unsupported github ssh url %q", raw)
		}
		return parts[0], parts[1], nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if u.Host != "github.com" {
		return "", "", fmt.Errorf("unsupported host %q (github.com only)", u.Host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("github url missing owner/repo: %q", raw)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// CloneURLFromImport returns the git remote URL used for cloning, honoring SSH vs HTTPS input.
func CloneURLFromImport(raw, owner, name string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "git@github.com:") {
		return fmt.Sprintf("git@github.com:%s/%s.git", owner, name)
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
}
