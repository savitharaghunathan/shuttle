package registry

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRegistryUpsertGetList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "registry.db")
	reg, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	repo := Repo{
		Owner: "org",
		Name:  "repo",
		URL:   "https://github.com/org/repo",
		Path:  "/tmp/org/repo",
	}
	if err := reg.Upsert(repo); err != nil {
		t.Fatal(err)
	}

	got, err := reg.Get("org", "repo")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != repo.URL || got.Path != repo.Path {
		t.Fatalf("Get() = %+v", got)
	}

	list, err := reg.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("List() len = %d", len(list))
	}
}

func TestRegistryGetMissing(t *testing.T) {
	reg, err := Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	_, err = reg.Get("missing", "repo")
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestRegistryUpsertUpdatesPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "registry.db")
	reg, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	_ = reg.Upsert(Repo{Owner: "o", Name: "r", URL: "u1", Path: "/a"})
	_ = reg.Upsert(Repo{Owner: "o", Name: "r", URL: "u1", Path: "/b"})

	got, err := reg.Get("o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/b" {
		t.Fatalf("Path = %q, want /b", got.Path)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set")
	}
	if got.UpdatedAt.Before(got.CreatedAt.Add(-time.Second)) {
		t.Fatalf("UpdatedAt not refreshed: %+v", got)
	}
}
