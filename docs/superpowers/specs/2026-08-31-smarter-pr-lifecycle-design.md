# Smarter PR Lifecycle — Design

**Status:** Approved direction (2026-08-31, rev 4)

## Amendments log

- **rev 4:** `VerifyRemoteBranch` distinguishes "branch definitively absent" (fail `implement`) from "could not reach remote" — auth/network `remote.List` errors are **warn-and-proceed**, because the Shuttle process may not share the agent's git credentials and a successful push must not be misreported as a compliance failure.
- **rev 3:** PR detection uses `gh pr list --head` (not invalid `gh pr view --head`). Explicit `gh` error vs empty-result handling. Preflight remote-branch check dropped (stale local refs); open-PR warning via `gh pr list` only. `VerifyRemoteBranch` uses go-git `remote.List` (network fetch). `--force` on succeeded issue run documented (updates existing PR). `ParseRef` shares owner/repo/number parsing with `ParseIssueURL`.
- **rev 2:** Spec-run retry inherits branch via `parent_run_id` chain (not new run UUID). `RenderContext.ShuttleBin` from `os.Executable()` / `SHUTTLE_BIN`. Post-implement branch compliance check (remote ref). Gated `pr-factory` prompt aligned with headless git instructions. `config.LoadDefaults` aligned with scaffold. Migration docs for existing `shuttle.yaml`. `BranchName`/`ResolveBranch` return errors. Empty `--issue` argv filtering. `gh pr create` body strategy without `--fill`+`Closes` conflict. Concurrent same-issue warning. Resume vs retry preflight documented. Fork PRs remain non-goal.
- **rev 1:** Initial design.

## Problem

`pr-factory[-headless]` instructs the agent to work on an unnamed "feature branch" and runs `gh pr create --fill` at `open_pr`. In real repos this breaks on the second attempt:

| Situation | Today |
|-----------|--------|
| Retry after `open_pr` failed | `gh pr create` errors: PR already exists for branch |
| Retry after `test` failed | Agent may push to the same branch; `open_pr` may still fail |
| Resume from `open_pr` | Same failure if PR was created between attempts |
| No issue ref (`--spec` runs) | No branch naming convention; every run is ad hoc |
| Spec-run retry | N/A today — would get a new ad hoc branch per retry |

Run recovery (rev 3) deliberately deferred branch/PR detection and idempotent `open_pr` to this feature. Retry git preflight today warns only on dirty tree and non-default branch — not on existing feature branches or open PRs.

## Goal

Make issue-driven and retried runs **reuse** the same feature branch and **succeed** at `open_pr` when a PR already exists, without Shuttle mutating the user's git repository.

Success criteria:

1. Deterministic branch name per logical run line (from `issue_ref`, run id, or `parent_run_id` chain).
2. `implement` prompt tells the agent that branch name explicitly (gated and headless).
3. Post-implement validation that the expected branch exists on the remote (agent compliance gate).
4. Default `open_pr` command is idempotent: create PR if missing, no-op (exit 0) if one already exists for the branch.
5. Retry preflight adds **advisory** warnings when the feature branch or an open PR already exists.
6. `open_pr` resolves the Shuttle binary without requiring `shuttle` on `PATH`.

## Non-goals

- Shuttle checking out, creating, deleting, or resetting branches (trust boundary unchanged).
- Shuttle pushing commits or force-pushing.
- Closing, reopening, or editing existing PR bodies after creation (except appending `Closes #N` at create time when missing).
- Merge conflict resolution.
- Fork-based PR head (`owner:branch`) — same-repo branches only in v1; fork workflows use a custom `open_pr.command`.
- GitLab / non-GitHub forges (issue URLs remain GitHub-only).
- Replacing `gh` for PR operations (already required for `issue.FetchSpec`).
- Auto-import on `run --issue` (separate ROADMAP item: Zero-touch repo discovery).
- Serializing concurrent runs on the same issue (warn only).

## Architecture

```
StartRun (issue_ref, run id, parent_run_id)
       │
       ▼
ResolveBranch(run) ──► RenderContext.{Branch, IssueRef, ShuttleBin}
       │                         │
       │                         ├──► implement prompt
       │                         ├──► open_pr command ({{.ShuttleBin}} pr open …)
       │                         └──► post-implement VerifyRemoteBranch (go-git)
       │
RetryRun ──► GitPreflight(cwd, branch) ──► warnings[] (branch exists, PR open)
```

### Layers

| Layer | Change |
|-------|--------|
| `internal/issue` | `ParseRef`, `BranchName`, `ResolveBranch(run)` with `parent_run_id` inheritance |
| `internal/pipeline` | Extend `RenderContext`; `ShuttleBin()` helper; populate fields in engine; `VerifyRemoteBranch`; extend `GitPreflight`; concurrent-issue warning in `StartRun` |
| `internal/cli/pr.go` | `shuttle pr open` — idempotent PR create via `gh` |
| `internal/repo/scaffold.go` | Default `open_pr.command` uses `{{.ShuttleBin}}` |
| `internal/config/config.go` | `LoadDefaults` `open_pr` aligned with scaffold template vars |
| `internal/templates` | `pr-factory`, `pr-factory-headless` implement prompts reference `{{.Branch}}` + git instructions |
| `README` | Branch convention, migration, idempotent open_pr, retry/resume PR behavior |

No SQLite schema changes. Branch is derived at render time from `runs.issue_ref`, `runs.id`, and `runs.parent_run_id`.

## Branch naming convention

| Run type | Resolution | Branch |
|----------|------------|--------|
| Issue-driven | `issue_ref` set | `shuttle/issue-<N>` |
| Spec / local (new) | no `issue_ref`, no `parent_run_id` | `shuttle/run-<short>` where `<short>` is first 8 chars of run UUID |
| Spec retry | `parent_run_id` set, no `issue_ref` | **same branch as parent** (walk `parent_run_id` chain to root run id) |

### `ResolveBranch`

```go
// ResolveBranch returns the feature branch for a run.
// Issue-driven: shuttle/issue-N from issue_ref.
// Spec retry: recurse parent_run_id until root, then shuttle/run-<rootShort>.
// Malformed issue_ref: error (surfaces at stage render).
func ResolveBranch(issueRef, runID, parentRunID string, parentBranch func(parentID string) (string, error)) (string, error)
```

`parentBranch` is injected by the engine (loads parent run, calls `ResolveBranch` recursively). This keeps spec-run retries on the same branch as the failed parent without a DB migration.

**`ParseRef`:** parses stored `issue_ref` (`org/repo#N`). Reuse the owner/repo/number extraction logic from `ParseIssueURL` (same validation rules; different input format). Do not duplicate parsing in two incompatible ways.

Rules:

- Issue number is unique within a repo; branch names are scoped to the registered checkout (`cwd`).
- Characters: lowercase `shuttle/`, digits, hyphen.
- `ParseRef` errors on malformed `org/repo#N`.

## Render context

Extend `pipeline.RenderContext`:

```go
type RenderContext struct {
    // existing fields...
    IssueRef   string // e.g. org/repo#42; empty for spec runs
    Branch     string // always set for runs after this change
    ShuttleBin string // os.Executable(), overridable via SHUTTLE_BIN env
}
```

**`ShuttleBin` resolution (engine):**

1. If `SHUTTLE_BIN` env is set and executable → use it.
2. Else `os.Executable()` (absolute path to current `shuttle` binary).
3. On failure, fall back to `"shuttle"` and log a one-line warning to progress stderr.

Engine sets `IssueRef`, `Branch`, and `ShuttleBin` from the `store.Run` record on every stage tick. Malformed `issue_ref` fails the stage at render time with a clear error.

### Template updates

**`pr-factory-headless` implement prompt:**

```yaml
prompt: |
  Implement the requested change from the intake spec artifact.
  You MUST use branch {{.Branch}} exactly.
  Create it from the default branch if needed, commit your changes, and push to origin.
  If the branch already exists locally or on the remote, continue on it and push updates.
  Summarize what you changed.
```

**`pr-factory` implement prompt** (same git requirements; human gate unchanged):

```yaml
prompt: |
  Implement the requested change from the intake artifacts.
  You MUST use branch {{.Branch}} exactly.
  Create it from the default branch if needed, commit your changes, and push to origin.
  If the branch already exists locally or on the remote, continue on it and push updates.
  Summarize the result.
```

## Branch compliance (post-implement)

After the `implement` stage worker returns success, before marking the stage succeeded, the engine calls `VerifyRemoteBranch(cwd, branch) (found bool, err error)` (go-git):

1. Open repo at `cwd`.
2. Resolve `origin` remote (or first remote if `origin` missing).
3. Call `remote.List(&git.ListOptions{})` to fetch current remote refs (network; same trust boundary as agent push).
4. If `remote.List` **succeeds**: return whether `refs/heads/<branch>` is present (`found`, `nil`).
5. If `remote.List` **errors** (auth, network, no remote): return `(false, err)` — an *inconclusive* result, not a definitive absence.

**Engine handling of the result:**

| Result | Action |
|--------|--------|
| `found == true` | mark `implement` succeeded (normal) |
| `found == false, err == nil` (list succeeded, ref absent) | mark `implement` **failed** — the agent did not push the required branch |
| `err != nil` (could not reach remote) | mark `implement` **succeeded** + append advisory progress warning `could not verify remote branch <branch>: <err>` |

Definitive-absence failure summary:

```
expected branch shuttle/issue-42 not found on remote origin; agent must push to this branch
```

Rationale: the Shuttle process may not share the agent's git credentials. A successful agent push followed by a Shuttle-side auth/network failure must **not** be misreported as a compliance failure (that would make `implement` un-passable in credential-isolated setups). Only a *confirmed* empty remote fails the stage — mirroring the `gh pr list` "non-zero exit vs empty array" distinction in `shuttle pr open`.

This is a read-only check. Shuttle does not push. Skipped entirely when `cwd` is not a git repo (spec-only / dry runs without git).

**Retry behavior:** failed compliance allows retry/resume to re-run `implement` with the same branch in the prompt.

## `shuttle pr open`

New subcommand invoked by the default `open_pr` shell stage.

```bash
shuttle pr open --branch <name> [--issue org/repo#N] [--cwd <path>]
```

**Argv hygiene:** strip flag pairs where the value is empty after template render (`--issue ""` must not be passed to cobra or `gh`).

**Behavior:**

1. `--cwd` defaults to process cwd (the repo checkout). Must be a git repo (go-git `PlainOpen`); error otherwise.
2. `--branch` required. Slash-containing names (`shuttle/issue-42`) are passed quoted to `gh` as-is (same-repo head).
3. If `--issue` is non-empty, parse with `issue.ParseRef` → `-R owner/repo` and issue number `N`.
4. If `--issue` empty, infer `-R owner/repo` from `origin` via go-git when possible.
5. **Detect existing PR** (use `gh pr list`, not `gh pr view --head` — `--head` is a `list` flag):

   ```bash
   gh pr list --head <branch> --state open [-R owner/repo] --json url
   ```

   - Exit code **non-zero** → fail the stage (auth, network, repo not found). Do **not** fall through to create.
   - Exit code **0** with non-empty JSON array → print `pr: <url>` from first element to stdout and **exit 0**.
   - Exit code **0** with empty array `[]` → no open PR; proceed to create.

6. **Create PR:**
   - **With issue:** fetch title via `gh issue view N -R owner/repo --json title`; then `gh pr create --head <branch> -R owner/repo --title "<title>" --body "Closes #N"`. Do **not** use `--fill` (incompatible with explicit `--body`).
   - **Without issue:** `gh pr create --head <branch> [-R owner/repo] --fill`.
7. On `gh pr create` failure, exit non-zero with stderr message (normal stage failure).

**Trust boundary:** read-only git inspection. Warn to stderr if remote branch ref missing; still attempt create so `gh` surfaces the error in stage logs.

**Override:** Users may keep `gh pr create --fill` in `shuttle.yaml`.

### Default `open_pr.command` (scaffold + `config.LoadDefaults`)

```yaml
open_pr:
  command:
    - "{{.ShuttleBin}}"
    - pr
    - open
    - --branch
    - "{{.Branch}}"
    - --issue
    - "{{.IssueRef}}"
```

`shuttle pr open` ignores `--issue` when the rendered value is empty (spec runs).

### Migration (existing repos)

README documents that repos imported before this feature should update `shuttle.yaml`:

```yaml
open_pr:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
```

Scaffold change applies only to newly generated configs. No auto-migration of on-disk `shuttle.yaml`.

## Git preflight (retry only)

Extend `GitPreflight(cwd, branch string) []string`:

| Check | Library | Warning |
|-------|---------|---------|
| Dirty working tree | go-git | `uncommitted changes in <cwd>` (unchanged) |
| Non-default current branch | go-git | `on non-default branch <name>` (unchanged; `main`/`master` only — known limitation) |
| Local branch ref exists | go-git | `branch <branch> already exists locally` |
| Open PR for branch | `gh pr list --head` | `open PR already exists for branch <branch>: <url>` |

**Dropped:** go-git "remote branch exists" preflight. Local `refs/remotes/origin/*` are stale without a recent fetch and would produce false negatives after an agent push. Post-implement `VerifyRemoteBranch` uses `remote.List` (network) for the authoritative check; preflight relies on local branch + `gh pr list` only.

**`gh` preflight errors:** if `gh pr list` exits non-zero, append warning `could not check for open PR: <err>` and proceed (advisory). Unlike `pr open` (stage failure), preflight must not block retry on transient `gh` failures.

All warnings remain **advisory** (warn-and-proceed). `RetryRun` passes `ResolveBranch(parent)`.

**Resume** does not re-run extended preflight — operator already chose to continue the same run. README documents this asymmetry.

## Concurrent runs (same issue)

On `StartRun`, when `issue_ref` is non-empty: if another run in the same DB has the same `issue_ref` and status in `{pending, running, blocked}`, append a **warning** to CLI stderr / API create response:

```
another run for org/repo#42 is already active (<run-id>); concurrent work may conflict on branch shuttle/issue-42
```

Does not block start (single-writer SQLite already prevents parallel writers; this warns about sequential overlap).

## Interaction with run recovery

| Scenario | Expected behavior |
|----------|-------------------|
| Retry after `open_pr` failed with PR open | Same branch; `open_pr` finds PR, exits 0 |
| Retry after `implement` failed | Same branch (issue or parent chain); agent continues partial work |
| Spec-run retry (`retry --agent`) | **Same** `shuttle/run-<rootShort>` via `parent_run_id` chain |
| Retry with `--force` on succeeded issue run | Same `shuttle/issue-N`; agent pushes new commits; **existing open PR is updated** (not a new PR). Operator must expect the succeeded run's PR to change. |
| Retry with `--force` on succeeded spec run | New run id but **inherits** parent branch via `parent_run_id`; same PR-update behavior if a PR exists |
| Resume from failed `open_pr` | Same run id → stable branch; `open_pr` idempotent |

## Error cases

| Case | Result |
|------|--------|
| `shuttle pr open` without `--branch` | CLI error |
| `--cwd` not a git repo | error |
| Malformed `--issue` or `issue_ref` in DB | error at render or CLI |
| `gh` not on PATH | error with hint to install gh |
| `implement` compliance: branch not on remote | stage failed (compliance message) |
| `gh pr create` fails (no commits on branch, etc.) | stage failure; logs capture stderr |
| Custom `open_pr.command` without `{{.Branch}}` | unchanged behavior (user responsibility) |
| `SHUTTLE_BIN` / `os.Executable()` unavailable | fallback `shuttle` on PATH + warning |

## Testing

- `ParseRef`, `BranchName`, `ResolveBranch` unit tests (issue ref, spec run, retry inheritance, malformed ref).
- `VerifyRemoteBranch` with temp bare remote (go-git).
- `RenderContext` includes `Branch`, `IssueRef`, `ShuttleBin` in engine test.
- `shuttle pr open` unit tests with injectable `ghRunner`: existing PR skip create; `gh pr list` returns `[]` → create; `gh pr list` non-zero exit → stage error (no create); empty `--issue` argv filtering; branch names with `/`.
- `GitPreflight` extended checks with temp repos + fake `gh` on `PATH` (subprocess path, not injectable runner).
- **Integration:** shell worker invokes `SHUTTLE_BIN` wrapper script that delegates to real binary or fake `gh` on `PATH` — exercises the actual `open_pr` stage argv path.
- Template embed tests for `{{.Branch}}` in both factory templates.
- Scaffold + `config.LoadDefaults` tests for `{{.ShuttleBin}}` open_pr command.
- **Integration:** `recovery_test` or `integration_test` — retry inherits spec branch; mock `pr open` idempotent path.
- Engine test: implement fails when remote branch missing.

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged: Shuttle does not mutate user git state
- Prefer MIT / Apache-2.0 OSS libraries; use go-git for git reads; `gh` subprocess for GitHub PR/issue APIs (consistent with `internal/issue/fetch.go`)
- Do not add `Co-authored-by: Cursor`; ask user for attribution before commit

## Documentation (README)

Add a **Branch and PR lifecycle** section covering:

- Branch naming table and `parent_run_id` inheritance
- `{{.ShuttleBin}}` / `SHUTTLE_BIN` / install on PATH
- Post-implement remote branch compliance
- `open_pr` idempotency and `shuttle pr open`
- **Migration** for existing `shuttle.yaml`
- Retry warnings vs resume (no extended preflight on resume)
- Concurrent same-issue warning
- Custom `open_pr.command` override
- Link from resume/retry section (replace deferred note)

Move **Smarter PR lifecycle** from ROADMAP **Next** → **Shipped** when the feature is merged and dogfooded (not merely when tests pass).
