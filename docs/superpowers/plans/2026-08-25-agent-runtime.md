# Agent Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform Shuttle from a local POC into an issue-driven, headless agent factory with `shuttle import repo` and `shuttle run --issue`.

**Architecture:** Global `SHUTTLE_HOME` (~/.shuttle) holds a repo registry and cloned checkouts. Import clones + scaffolds config. Run resolves issue → spec, dispatches agent runtime workers, optionally skips manual gates with `--headless`.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), gh CLI for issues, text/template for worker args

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- No vendor SDKs in core — agent CLIs only
- Single SQLite writer per DB file
- Workers run as invoking user (trust boundary unchanged)
- Git commits: Signed-off-by + `Assisted-by: Cursor <cursoragent@cursor.com>`

---

## File Map

| File | Responsibility |
|---|---|
| `internal/home/home.go` | SHUTTLE_HOME path resolution |
| `internal/registry/registry.go` | Global repo registry SQLite |
| `internal/repo/parse.go` | GitHub URL parsing |
| `internal/repo/detect.go` | Project type detection → test command |
| `internal/repo/import.go` | Clone + scaffold shuttle.yaml |
| `internal/issue/parse.go` | Issue URL parsing |
| `internal/issue/fetch.go` | gh issue view → spec markdown |
| `internal/cli/import.go` | `shuttle import repo` command |
| `internal/cli/repos.go` | `shuttle repos list` command |
| `internal/worker/worker.go` | Add Agent, Model to Request |
| `internal/worker/preset.go` | Template Agent, Model in args |
| `internal/pipeline/engine.go` | Thread run opts to worker Request |
| `internal/templates/pr-factory-headless.yaml` | No manual gate pipeline |
| `internal/cli/root.go` | Extend run with --issue, --agent, --model, --headless |

---

### Task 1: SHUTTLE_HOME paths

**Files:**
- Create: `internal/home/home.go`
- Create: `internal/home/home_test.go`

**Interfaces:**
- Produces: `func Dir() string`, `func ReposDir() string`, `func RegistryDB() string`

- [ ] **Step 1: Write failing test**

```go
func TestDirDefaultsToHomeShuttle(t *testing.T) {
    t.Setenv("SHUTTLE_HOME", "")
    home := os.UserHomeDir() // use in test
    if !strings.HasSuffix(Dir(), filepath.Join(home, ".shuttle")) {
        t.Fatalf("Dir() = %q", Dir())
    }
}
```

- [ ] **Step 2: Run test — expect FAIL**
- [ ] **Step 3: Implement home.go**
- [ ] **Step 4: Run test — expect PASS**

---

### Task 2: GitHub URL parsing

**Files:**
- Create: `internal/repo/parse.go`
- Create: `internal/repo/parse_test.go`

**Interfaces:**
- Produces: `func ParseGitHubURL(raw string) (owner, name string, err error)`

- [ ] Parse `https://github.com/org/repo`, `git@github.com:org/repo.git`, `org/repo`
- [ ] Reject non-GitHub URLs with clear error

---

### Task 3: Project detection

**Files:**
- Create: `internal/repo/detect.go`
- Create: `internal/repo/detect_test.go`

**Interfaces:**
- Produces: `func DetectTestCommand(repoPath string) []string`

- [ ] go.mod → `["go","test","./..."]`
- [ ] package.json → `["npm","test"]`
- [ ] pyproject.toml → `["python","-m","pytest"]`
- [ ] else → `["true"]`

---

### Task 4: Repo registry

**Files:**
- Create: `internal/registry/registry.go`
- Create: `internal/registry/registry_test.go`

**Interfaces:**
- Produces: `Open(path) (*Registry, error)`, `Upsert(Repo)`, `Get(owner, name)`, `List()`

```go
type Repo struct {
    Owner     string
    Name      string
    URL       string
    Path      string
    CreatedAt time.Time
}
```

---

### Task 5: Import repo

**Files:**
- Create: `internal/repo/import.go`
- Create: `internal/repo/import_test.go` (mock git with temp dirs)
- Create: `internal/repo/scaffold.go` — write shuttle.yaml

**Interfaces:**
- Produces: `func Import(ctx, url string, opts ImportOpts) (Repo, error)`

- [ ] Clone via `git clone` to `{ReposDir}/{owner}/{name}`
- [ ] Skip clone if path exists and is git repo (update registry only)
- [ ] Write shuttle.yaml if absent
- [ ] Register in registry

---

### Task 6: CLI import + repos list

**Files:**
- Create: `internal/cli/import.go`
- Create: `internal/cli/repos.go`
- Modify: `internal/cli/root.go` — register commands

```bash
shuttle import repo https://github.com/org/repo
shuttle repos list
```

---

### Task 7: Issue URL parse + fetch

**Files:**
- Create: `internal/issue/parse.go`
- Create: `internal/issue/fetch.go`
- Tests for both

**Interfaces:**
- Produces: `ParseIssueURL(raw) (owner, repo string, number int, err error)`
- Produces: `FetchSpec(ctx, owner, repo string, number int) (title, body string, err error)` via `gh issue view`

---

### Task 8: Run-level agent + model

**Files:**
- Modify: `internal/worker/worker.go` — Agent, Model on Request
- Modify: `internal/worker/preset.go` — template fields
- Modify: `internal/pipeline/engine.go` — StartParams + Request threading
- Modify: `internal/config/config.go` — model arg in cursor preset default

---

### Task 9: pr-factory-headless template

**Files:**
- Create: `internal/templates/pr-factory-headless.yaml`
- Modify: `internal/templates/embed.go` — embed new template
- Modify: `internal/cli/root.go` — `--headless` selects template; `--issue` resolves repo + writes spec

```bash
shuttle run --issue https://github.com/org/repo/issues/1 \
           --agent cursor --model claude-4-sonnet --headless
```

---

### Task 10: README update

**Files:**
- Modify: `README.md`

Document import, issue-driven runs, headless mode, SHUTTLE_HOME.

---

## Execution order

1 → 2 → 3 → 4 → 5 → 6 (import path complete)
7 → 8 → 9 → 10 (issue-driven headless runs)

Each task is independently testable before moving on.
