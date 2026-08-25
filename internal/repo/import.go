package repo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/savitharaghunathan/shuttle/internal/home"
	"github.com/savitharaghunathan/shuttle/internal/registry"
)

type ImportOpts struct {
	CloneFn func(ctx context.Context, url, dest string) error
}

// Import clones (or reuses) a GitHub repo, scaffolds shuttle.yaml, and registers it.
func Import(ctx context.Context, rawURL string, reg *registry.Registry, opts ImportOpts) (registry.Repo, error) {
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
	cmd := exec.CommandContext(ctx, "git", "clone", url, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}
