# Import Existing Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `shuttle import repo` register an existing local checkout via `--path` or `.` without cloning into `SHUTTLE_HOME`.

**Architecture:** Extend `ImportOpts` with `LocalPath`. In local mode, resolve absolute path, require a git repo, resolve owner/name from arg and/or origin remote via go-git, never clone, upsert registry with that path. CLI adds `--path` and treats `.` as cwd local mode.

**Tech Stack:** Go, SQLite registry, go-git (Apache-2.0) for clone + remotes

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged
- Prefer MIT / Apache-2.0 OSS libraries; use go-git instead of the git CLI
- Do not add `Co-authored-by: Cursor`; ask user for attribution before commit

---

## File Map

| File | Responsibility |
|---|---|
| `internal/repo/remote.go` | Read `origin` remote URL from a git checkout |
| `internal/repo/import.go` | Local-path import path; skip clone |
| `internal/repo/import_test.go` | Local import / mismatch / remote-only tests |
| `internal/cli/import.go` | `--path` flag; `.` arg; optional owner/name arg |
| `README.md` | Document `--path` and `.` |

---

### Task 1: Origin remote helper + Import LocalPath

**Files:**
- Create: `internal/repo/remote.go`
- Create: `internal/repo/remote_test.go`
- Modify: `internal/repo/import.go`
- Modify: `internal/repo/import_test.go`

**Interfaces:**
- Produces: `func OriginRemoteURL(repoPath string) (string, error)`
- Produces: `ImportOpts.LocalPath string` — when non-empty, never clone; use that path
- Consumes: `ParseGitHubURL`, `IsGitRepo`, `WriteShuttleYAMLIfAbsent`, `registry.Upsert`

- [ ] **Step 1: Write failing tests for OriginRemoteURL and local Import**

```go
func TestOriginRemoteURL(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "remote", "add", "origin", "git@github.com:org/demo.git")
	got, err := OriginRemoteURL(dir)
	if err != nil || got != "git@github.com:org/demo.git" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestImportLocalPathNoClone(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "remote", "add", "origin", "https://github.com/org/demo.git")
	reg := openReg(t)
	cloneCalled := false
	got, err := Import(context.Background(), "org/demo", reg, ImportOpts{
		LocalPath: dir,
		CloneFn: func(ctx context.Context, url, dest string) error {
			cloneCalled = true
			return nil
		},
	})
	if err != nil || cloneCalled || got.Path != dir {
		t.Fatalf("got=%+v clone=%v err=%v", got, cloneCalled, err)
	}
}

func TestImportLocalPathRemoteOnly(t *testing.T) { /* LocalPath set, rawURL empty → from origin */ }
func TestImportLocalPathMismatch(t *testing.T) { /* arg org/a vs remote org/b → error */ }
func TestImportLocalPathNotGit(t *testing.T) { /* empty dir → error */ }
```

- [ ] **Step 2: Implement `OriginRemoteURL` and extend `Import`**

```go
type ImportOpts struct {
	LocalPath string
	CloneFn   func(ctx context.Context, url, dest string) error
}

// When LocalPath != "": abs path, IsGitRepo, resolve owner/name, never clone.
// When both arg and origin present and disagree: error.
// When rawURL empty: require origin.
```

- [ ] **Step 3: `go test ./internal/repo/ -count=1` — PASS**

- [ ] **Step 4: Commit** (ask user for attribution first)

---

### Task 2: CLI `--path` and `.`

**Files:**
- Modify: `internal/cli/import.go`

**Interfaces:**
- Consumes: `repo.Import` with `ImportOpts{LocalPath: ...}`
- Args: `cobra.MaximumNArgs(1)`; `--path` string flag
- `.` alone ⇒ LocalPath = cwd; optional `org/repo` arg still allowed with `--path`

- [ ] **Step 1: Wire flag and arg rules**

```go
var localPath string
cmd.Flags().StringVar(&localPath, "path", "", "existing local checkout (no clone)")
Args: cobra.MaximumNArgs(1),
// if args[0]=="." → localPath = cwd (error if --path also set)
// if localPath set → abs, Import(..., rawOrEmpty, ImportOpts{LocalPath: abs})
```

- [ ] **Step 2: Manual smoke**

```bash
go build -o bin/shuttle ./cmd/shuttle
cd /some/clone && ../shuttle/bin/shuttle import repo .
shuttle import repo --path /some/clone org/repo
```

- [ ] **Step 3: Commit** (ask attribution)

---

### Task 3: README

**Files:**
- Modify: `README.md` Import section

Document:

```bash
shuttle import repo --path /path/to/checkout org/repo
shuttle import repo --path /path/to/checkout   # owner/name from origin
cd /path/to/checkout && shuttle import repo .
```

Note that URL-only import still uses `~/.shuttle/repos/...`.

- [ ] **Step 1: Update README**
- [ ] **Step 2: `go test ./... -count=1`**
- [ ] **Step 3: Commit** (ask attribution)

---

## Execution order

1 → 2 → 3
