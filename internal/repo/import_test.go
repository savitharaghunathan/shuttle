package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/registry"
)

func TestImportRegistersExistingRepo(t *testing.T) {
	shuttleHome := t.TempDir()
	t.Setenv("SHUTTLE_HOME", shuttleHome)

	dest := filepath.Join(shuttleHome, "repos", "org", "demo")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dest, "go.mod"), "module demo\n")

	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	cloneCalled := false
	got, err := Import(context.Background(), "org/demo", reg, ImportOpts{
		CloneFn: func(ctx context.Context, url, dest string) error {
			cloneCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloneCalled {
		t.Fatal("clone should not run when dest already exists as git repo")
	}
	if got.Owner != "org" || got.Name != "demo" {
		t.Fatalf("got %+v", got)
	}

	shuttleYAML := filepath.Join(got.Path, "shuttle.yaml")
	if _, err := os.Stat(shuttleYAML); err != nil {
		t.Fatalf("shuttle.yaml missing: %v", err)
	}
}

func TestImportClonesWhenMissing(t *testing.T) {
	shuttleHome := t.TempDir()
	t.Setenv("SHUTTLE_HOME", shuttleHome)

	src := filepath.Join(t.TempDir(), "upstream")
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "README.md"), "# demo\n")

	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	got, err := Import(context.Background(), "https://github.com/org/demo", reg, ImportOpts{
		CloneFn: func(ctx context.Context, url, dest string) error {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			return copyDir(src, dest)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Join(shuttleHome, "repos", "org", "demo") {
		t.Fatalf("Path = %q", got.Path)
	}
}

func TestImportUsesSSHCloneURL(t *testing.T) {
	shuttleHome := t.TempDir()
	t.Setenv("SHUTTLE_HOME", shuttleHome)

	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	var clonedURL string
	_, err = Import(context.Background(), "git@github.com:org/demo.git", reg, ImportOpts{
		CloneFn: func(ctx context.Context, url, dest string) error {
			clonedURL = url
			if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
				return err
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if clonedURL != "git@github.com:org/demo.git" {
		t.Fatalf("clone URL = %q, want git@github.com:org/demo.git", clonedURL)
	}
}

func TestImportUsesHTTPSCloneURL(t *testing.T) {
	shuttleHome := t.TempDir()
	t.Setenv("SHUTTLE_HOME", shuttleHome)

	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	var clonedURL string
	_, err = Import(context.Background(), "https://github.com/org/demo", reg, ImportOpts{
		CloneFn: func(ctx context.Context, url, dest string) error {
			clonedURL = url
			if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
				return err
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if clonedURL != "https://github.com/org/demo.git" {
		t.Fatalf("clone URL = %q, want https://github.com/org/demo.git", clonedURL)
	}
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
