package registry

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Repo struct {
	Owner     string
	Name      string
	URL       string
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Registry struct {
	db *sql.DB
}

func Open(dbPath string) (*Registry, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open registry %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate registry %s: %w", dbPath, err)
	}

	return &Registry{db: db}, nil
}

func (r *Registry) Close() error {
	return r.db.Close()
}

func (r *Registry) Upsert(repo Repo) error {
	now := time.Now().UTC()
	existing, err := r.Get(repo.Owner, repo.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, ErrNotFound) {
		repo.CreatedAt = now
	} else {
		repo.CreatedAt = existing.CreatedAt
	}
	repo.UpdatedAt = now

	_, err = r.db.Exec(`
INSERT INTO repos (owner, name, url, path, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(owner, name) DO UPDATE SET
  url = excluded.url,
  path = excluded.path,
  updated_at = excluded.updated_at`,
		repo.Owner,
		repo.Name,
		repo.URL,
		repo.Path,
		formatTime(repo.CreatedAt),
		formatTime(repo.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert repo %s/%s: %w", repo.Owner, repo.Name, err)
	}
	return nil
}

func (r *Registry) Get(owner, name string) (Repo, error) {
	row := r.db.QueryRow(`
SELECT owner, name, url, path, created_at, updated_at
FROM repos
WHERE owner = ? AND name = ?`, owner, name)

	var repo Repo
	var createdAt, updatedAt string
	if err := row.Scan(&repo.Owner, &repo.Name, &repo.URL, &repo.Path, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repo{}, ErrNotFound
		}
		return Repo{}, fmt.Errorf("get repo %s/%s: %w", owner, name, err)
	}
	repo.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	repo.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return repo, nil
}

func (r *Registry) List() ([]Repo, error) {
	rows, err := r.db.Query(`
SELECT owner, name, url, path, created_at, updated_at
FROM repos
ORDER BY owner, name`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer rows.Close()

	var repos []Repo
	for rows.Next() {
		var repo Repo
		var createdAt, updatedAt string
		if err := rows.Scan(&repo.Owner, &repo.Name, &repo.URL, &repo.Path, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		repo.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		repo.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

var ErrNotFound = errors.New("repo not found")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS repos (
  owner TEXT NOT NULL,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (owner, name)
);`

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
