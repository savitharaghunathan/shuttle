# Smarter PR Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deterministic branch names (with retry inheritance), post-implement compliance, idempotent `open_pr` via `shuttle pr open` using `{{.ShuttleBin}}`, and extended retry git preflight — without Shuttle mutating user git state.

**Architecture:** `issue.ResolveBranch` derives branch from `issue_ref`, run id, or `parent_run_id` chain. Engine populates `RenderContext` including `ShuttleBin`. After `implement` succeeds, `VerifyRemoteBranch` (go-git) gates on remote ref. `shuttle pr open` wraps `gh` for create-or-skip.

**Tech Stack:** Go, go-git (Apache-2.0), `gh` CLI, cobra, text/template

**Spec:** [`docs/superpowers/specs/2026-08-31-smarter-pr-lifecycle-design.md`](../specs/2026-08-31-smarter-pr-lifecycle-design.md) (rev 4)

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged: Shuttle does not checkout, push, or reset git
- Prefer MIT / Apache-2.0 OSS libraries; go-git for git reads; `gh` subprocess for PR APIs
- Do not add `Co-authored-by: Cursor`; ask user for attribution before each commit

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/issue/ref.go` | `ParseRef`, `BranchName`, `ResolveBranch` |
| `internal/issue/ref_test.go` | Branch naming, parse, parent inheritance tests |
| `internal/pipeline/bin.go` | `ShuttleBin()` — `SHUTTLE_BIN` / `os.Executable()` / fallback |
| `internal/pipeline/bin_test.go` | Bin resolution tests |
| `internal/pipeline/render.go` | Add `IssueRef`, `Branch`, `ShuttleBin` to `RenderContext` |
| `internal/pipeline/branch.go` | `VerifyRemoteBranch(cwd, branch string) error` |
| `internal/pipeline/branch_test.go` | Remote ref compliance tests |
| `internal/pipeline/engine.go` | Render fields, compliance gate after implement, concurrent-issue warning |
| `internal/pipeline/gitpreflight.go` | Extended checks: local/remote branch, open PR |
| `internal/pipeline/gitpreflight_test.go` | Preflight tests |
| `internal/cli/pr.go` | `shuttle pr open` subcommand |
| `internal/cli/pr_test.go` | pr open tests (injectable gh, empty flags, slash branches) |
| `internal/cli/root.go` | Register `pr` command group |
| `internal/repo/scaffold.go` | Default `open_pr` → `{{.ShuttleBin}} pr open …` |
| `internal/config/config.go` | `LoadDefaults` open_pr aligned with scaffold |
| `internal/templates/pr-factory-headless.yaml` | Implement prompt + git MUST instructions |
| `internal/templates/pr-factory.yaml` | Same git instructions (gated) |
| `internal/templates/embed_test.go` | Assert prompts contain `{{.Branch}}` |
| `internal/pipeline/recovery.go` | `ResolveBranch` + extended `GitPreflight` on retry |
| `internal/pipeline/recovery_test.go` | Spec retry inherits parent branch |
| `internal/cli/integration_test.go` | E2E: open_pr via ShuttleBin template vars |
| `README.md` | Branch lifecycle, migration, resume vs retry |

---

### Task 1: Branch naming + ResolveBranch

**Files:**
- Create: `internal/issue/ref.go`
- Create: `internal/issue/ref_test.go`

**Interfaces:**
- `ParseRef(ref string) (owner, repo string, num int, err error)`
- `BranchName(issueRef, runID string) (string, error)` — issue or spec root branch
- `ResolveBranch(issueRef, runID, parentRunID string, loadParent func(id string) (issueRef, runID, parentRunID string, err error)) (string, error)`

- [ ] **Step 1: Write failing tests**

```go
func TestParseRef(t *testing.T) { /* org/repo#42 */ }
func TestBranchNameIssue(t *testing.T) { /* shuttle/issue-7 */ }
func TestBranchNameSpecRun(t *testing.T) { /* shuttle/run-a1b2c3d4 */ }
func TestResolveBranchSpecRetry(t *testing.T) {
	// child runID=new-uuid, parentRunID=root → shuttle/run-<rootShort>, not child short
}
func TestResolveBranchMalformedIssue(t *testing.T) { /* error */ }
```

- [ ] **Step 2: Implement**

- [ ] **Step 3: `go test ./internal/issue/ -count=1` — PASS**

- [ ] **Step 4: Commit** (ask user for attribution first)

---

### Task 2: ShuttleBin + RenderContext

**Files:**
- Create: `internal/pipeline/bin.go`
- Create: `internal/pipeline/bin_test.go`
- Modify: `internal/pipeline/render.go`
- Modify: `internal/pipeline/render_test.go`
- Modify: `internal/pipeline/engine.go`
- Modify: `internal/pipeline/engine_test.go`

- [ ] **Step 1: `ShuttleBin()` — honor `SHUTTLE_BIN`, else `os.Executable()`, else `"shuttle"`**

- [ ] **Step 2: Add `IssueRef`, `Branch`, `ShuttleBin` to `RenderContext`**

- [ ] **Step 3: Engine tick: `ResolveBranch` from run (+ parent loader); set render fields; render errors fail stage**

- [ ] **Step 4: Engine test: prompt contains branch; open_pr command contains absolute ShuttleBin**

- [ ] **Step 5: `go test ./internal/pipeline/ -count=1 -run 'Render|Engine|Bin'` — PASS**

- [ ] **Step 6: Commit** (ask attribution)

---

### Task 3: Post-implement branch compliance

**Files:**
- Create: `internal/pipeline/branch.go`
- Create: `internal/pipeline/branch_test.go`
- Modify: `internal/pipeline/engine.go`
- Modify: `internal/pipeline/engine_test.go`

- [ ] **Step 1: `VerifyRemoteBranch(cwd, branch string) (found bool, err error)` — go-git `remote.List` (network fetch), not stale local tracking refs. `err != nil` means inconclusive (auth/network), NOT absent.**

- [ ] **Step 2: After implement worker success, before marking succeeded, branch on the tri-state result:**
  - `found` → succeed
  - `!found && err == nil` (list succeeded, ref absent) → implement **failed** (agent didn't push)
  - `err != nil` (couldn't reach remote) → **succeed** + advisory warning (do not fail; Shuttle may lack the agent's git creds)

- [ ] **Step 3: Skip verify when cwd is not a git repo**

- [ ] **Step 4: Engine tests: (a) implement fails when list succeeds but ref absent; (b) implement succeeds + warns when `remote.List` errors; (c) implement succeeds when ref present**

- [ ] **Step 5: `go test ./internal/pipeline/ -count=1 -run 'Branch|Engine'` — PASS**

- [ ] **Step 6: Commit** (ask attribution)

---

### Task 4: `shuttle pr open`

**Files:**
- Create: `internal/cli/pr.go`
- Create: `internal/cli/pr_test.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- `pr open --branch NAME [--issue REF] [--cwd PATH]`
- Injectable `ghRunner` for tests
- Filter empty flag values from argv before parsing

- [ ] **Step 1: Failing tests — existing PR skip create; `gh pr list` returns `[]` → create; `gh pr list` error → fail (no create); empty `--issue`; branch `shuttle/issue-42`**

- [ ] **Step 2: Implement**

```go
// Detect: gh pr list --head B --state open [-R R] --json url
//   non-zero exit → return error (do not create)
//   [] → create
//   [{url:...}] → print url, exit 0
// With issue: gh issue view for title; gh pr create --head B -R R --title T --body "Closes #N"
// Without issue: gh pr create --head B --fill [-R R]
```

- [ ] **Step 3: `filterEmptyFlags(argv)` — drop `--issue ""` pairs before cobra parse**

- [ ] **Step 4: Register `pr` command under root**

- [ ] **Step 5: `go test ./internal/cli/ -count=1 -run PROpen` — PASS**

- [ ] **Step 6: Commit** (ask attribution)

---

### Task 5: Templates + scaffold + config defaults

**Files:**
- Modify: `internal/templates/pr-factory-headless.yaml`
- Modify: `internal/templates/pr-factory.yaml`
- Modify: `internal/templates/embed_test.go`
- Modify: `internal/repo/scaffold.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Update both implement prompts (MUST use branch, commit, push) per spec**

- [ ] **Step 2: Scaffold `OpenPR.Command`:**

```go
[]string{"{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"}
```

- [ ] **Step 3: `config.applyDefaults` — same `open_pr` command (replaces echo stub for unset open_pr)**

- [ ] **Step 4: Tests for scaffold YAML and config defaults**

- [ ] **Step 5: `go test ./internal/templates/ ./internal/config/ ./internal/repo/ -count=1` — PASS**

- [ ] **Step 6: Commit** (ask attribution)

---

### Task 6: Extended GitPreflight + retry branch + concurrent-issue warning

**Files:**
- Modify: `internal/pipeline/gitpreflight.go`
- Modify: `internal/pipeline/gitpreflight_test.go`
- Modify: `internal/pipeline/recovery.go`
- Modify: `internal/pipeline/recovery_test.go`
- Modify: `internal/pipeline/engine.go` (concurrent issue warning in `StartRun`)

- [ ] **Step 1: `GitPreflight(cwd, branch string) []string` — local branch (go-git) + open PR (`gh pr list --head`; warn on gh error, do not block)**

- [ ] **Step 2: Do NOT add go-git remote-branch preflight (stale local refs; see spec rev 3)**

- [ ] **Step 3: `RetryRun` uses `ResolveBranch(parent)` for preflight branch arg**

- [ ] **Step 4: `recovery_test`: spec retry child gets parent branch name in preflight / render**

- [ ] **Step 5: `StartRun`: warn when active run exists for same `issue_ref`**

- [ ] **Step 6: `go test ./internal/pipeline/ -count=1 -run 'Preflight|Retry|StartRun'` — PASS**

- [ ] **Step 7: Commit** (ask attribution)

---

### Task 7: Integration test + README + ROADMAP

**Files:**
- Modify: `internal/cli/integration_test.go`
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

- [ ] **Step 1: Integration test — `open_pr` stage with fake `gh` on `PATH` + `SHUTTLE_BIN` wrapper; assert idempotent skip when `gh pr list` returns existing PR**

- [ ] **Step 2: Integration or recovery test — retry spec run preserves branch via `parent_run_id`**

- [ ] **Step 3: README — Branch and PR lifecycle, migration snippet, resume vs retry preflight, `SHUTTLE_BIN`, `--force` updates existing PR**

- [ ] **Step 4: Update resume/retry section — link to new section, remove deferred note**

- [ ] **Step 5: Move Smarter PR lifecycle Next → Shipped in ROADMAP** (after dogfood note in commit message)

- [ ] **Step 6: `go test ./... -count=1` — PASS**

- [ ] **Step 7: Commit** (ask attribution)

---

## Execution order

1 → 2 → 3 → 4 → 5 → 6 → 7

Task 2 depends on Task 1. Task 6 depends on Task 1. Task 5 depends on Task 4 (subcommand exists). Task 7 last.

---

## Review fixes addressed

| Review finding | Resolution |
|----------------|------------|
| `gh pr view --head` invalid (rev 3) | `gh pr list --head --state open --json url`; empty `[]` vs non-zero exit |
| Spec-run retry new branch per UUID (rev 2) | `ResolveBranch` walks `parent_run_id` |
| `shuttle` on PATH (rev 2) | `{{.ShuttleBin}}` + `SHUTTLE_BIN` |
| `--fill` + `Closes #N` conflict (rev 2) | Issue path: explicit `--title` + `--body`; no `--fill` |
| go-git remote preflight stale (rev 3) | Dropped from preflight; `VerifyRemoteBranch` uses `remote.List` |
| `gh` error conflated with no PR (rev 3) | `pr open`: non-zero `gh pr list` → fail stage; preflight: warn only |
| Injectable runner misses subprocess path (rev 3) | Integration test: fake `gh` on `PATH` through shell worker |
| Prompt-only branch naming (rev 2) | `VerifyRemoteBranch` after implement |
| Compliance false-positive on cred isolation (rev 4) | `VerifyRemoteBranch` tri-state: absent→fail, unreachable→warn+proceed |
| Existing repos not migrated (rev 2) | README migration snippet; config defaults aligned |
| Gated pr-factory no git instructions (rev 2) | Same MUST branch/commit/push prompt |
| `BranchName` return type inconsistency (rev 2) | All helpers return `(string, error)` |
| Empty `--issue` argv (rev 2) | `filterEmptyFlags` in `pr open` |
| `--force` side effect (rev 3) | Documented: updates existing PR on same branch |
| `ParseRef` duplication (rev 3) | Reuse `ParseIssueURL` extraction logic |
| Concurrent same-issue runs (rev 2) | `StartRun` warning |
| Resume preflight asymmetry (rev 2) | Documented in README |
| Fork PRs (rev 2) | Non-goal in spec |
| ROADMAP shipped too early (rev 2) | Tied to merge + dogfood in Task 7 |
