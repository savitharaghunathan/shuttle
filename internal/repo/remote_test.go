package repo

import (
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

func TestOriginRemoteURL(t *testing.T) {
	dir := t.TempDir()
	initRepoWithOrigin(t, dir, "git@github.com:org/demo.git")

	got, err := OriginRemoteURL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "git@github.com:org/demo.git" {
		t.Fatalf("OriginRemoteURL = %q", got)
	}
}

func TestOriginRemoteURLMissing(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	if _, err := OriginRemoteURL(dir); err == nil {
		t.Fatal("expected error when origin is missing")
	}
}

func initRepo(t *testing.T, dir string) *git.Repository {
	t.Helper()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	return r
}

func initRepoWithOrigin(t *testing.T, dir, originURL string) *git.Repository {
	t.Helper()
	r := initRepo(t, dir)
	_, err := r.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{originURL},
	})
	if err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	return r
}

func absPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
