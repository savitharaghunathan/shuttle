package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"

	"github.com/savitharaghunathan/shuttle/internal/home"
	"github.com/savitharaghunathan/shuttle/internal/registry"
)

type ImportOpts struct {
	// LocalPath, when set, registers an existing checkout and never clones.
	LocalPath string
	CloneFn   func(ctx context.Context, url, dest string) error
}

// Import clones (or reuses) a GitHub repo, scaffolds shuttle.yaml, and registers it.
// When opts.LocalPath is set, the existing checkout is registered instead of cloning.
func Import(ctx context.Context, rawURL string, reg *registry.Registry, opts ImportOpts) (registry.Repo, error) {
	if strings.TrimSpace(opts.LocalPath) != "" {
		return importLocal(rawURL, reg, opts.LocalPath)
	}
	return importClone(ctx, rawURL, reg, opts)
}

func importLocal(rawURL string, reg *registry.Registry, localPath string) (registry.Repo, error) {
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return registry.Repo{}, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return registry.Repo{}, fmt.Errorf("path does not exist: %s", abs)
	}
	if !IsGitRepo(abs) {
		return registry.Repo{}, fmt.Errorf("not a git repository: %s", abs)
	}

	owner, name, err := resolveOwnerName(abs, rawURL)
	if err != nil {
		return registry.Repo{}, err
	}

	testCmd := DetectTestCommand(abs)
	if err := WriteShuttleYAMLIfAbsent(abs, testCmd); err != nil {
		return registry.Repo{}, err
	}

	entry := registry.Repo{
		Owner: owner,
		Name:  name,
		URL:   NormalizeRemoteURL(owner, name),
		Path:  abs,
	}
	if err := reg.Upsert(entry); err != nil {
		return registry.Repo{}, err
	}
	return reg.Get(owner, name)
}

func resolveOwnerName(repoPath, rawURL string) (owner, name string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	var argOwner, argName string
	hasArg := rawURL != ""
	if hasArg {
		argOwner, argName, err = ParseGitHubURL(rawURL)
		if err != nil {
			return "", "", err
		}
	}

	remoteURL, remoteErr := OriginRemoteURL(repoPath)
	hasRemote := remoteErr == nil
	var remOwner, remName string
	if hasRemote {
		remOwner, remName, err = ParseGitHubURL(remoteURL)
		if err != nil {
			return "", "", err
		}
	}

	switch {
	case hasArg && hasRemote:
		if argOwner != remOwner || argName != remName {
			return "", "", fmt.Errorf("path remote is %s/%s, expected %s/%s", remOwner, remName, argOwner, argName)
		}
		return argOwner, argName, nil
	case hasArg:
		return argOwner, argName, nil
	case hasRemote:
		return remOwner, remName, nil
	default:
		return "", "", fmt.Errorf("cannot determine GitHub owner/name; pass org/repo")
	}
}

func importClone(ctx context.Context, rawURL string, reg *registry.Registry, opts ImportOpts) (registry.Repo, error) {
	owner, name, err := ParseGitHubURL(rawURL)
	if err != nil {
		return registry.Repo{}, err
	}

	dest := home.RepoPath(owner, name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return registry.Repo{}, fmt.Errorf("create repos dir: %w", err)
	}

	if !IsGitRepo(dest) {
		clone := opts.CloneFn
		if clone == nil {
			clone = defaultClone
		}
		if err := clone(ctx, CloneURLFromImport(rawURL, owner, name), dest); err != nil {
			return registry.Repo{}, fmt.Errorf("clone %s/%s: %w", owner, name, err)
		}
	}

	testCmd := DetectTestCommand(dest)
	if err := WriteShuttleYAMLIfAbsent(dest, testCmd); err != nil {
		return registry.Repo{}, err
	}

	entry := registry.Repo{
		Owner: owner,
		Name:  name,
		URL:   NormalizeRemoteURL(owner, name),
		Path:  dest,
	}
	if err := reg.Upsert(entry); err != nil {
		return registry.Repo{}, err
	}
	return reg.Get(owner, name)
}

func defaultClone(ctx context.Context, url, dest string) error {
	_, err := git.PlainCloneContext(ctx, dest, false, &git.CloneOptions{
		URL:      url,
		Progress: os.Stdout,
	})
	if err != nil {
		return err
	}
	return nil
}
