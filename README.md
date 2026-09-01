# shuttle

Shuttle is a local AI software factory control plane. It runs DAG pipelines with agent runtimes, records each run in SQLite, and opens PRs from GitHub issues.

## Quick start (issue → PR)

```bash
go build -o bin/shuttle ./cmd/shuttle

# One-time per repo
shuttle import repo https://github.com/org/repo

# Headless run from an issue
shuttle run --issue https://github.com/org/repo/issues/42 \
            --agent cursor \
            --model claude-4-sonnet \
            --headless
```

Imported repos live under `~/.shuttle/repos/<owner>/<name>` by default, or at any path you register with `--path` / `.`. Shuttle scaffolds `shuttle.yaml` on import (test command, `open_pr`, agent presets). Edit that file once per repo if needed.

## Install

Build the CLI from the repository:

```bash
go build -o bin/shuttle ./cmd/shuttle
# or
make build
```

You can also run without building:

```bash
go run ./cmd/shuttle workers
```

### Make targets

```bash
make help               # list targets
make build              # bin/shuttle
make test-unit          # unit tests (fast; no integration tag)
make test-integration   # CLI pipeline integration tests
make test               # unit + integration
make verify             # build + test
```

## Import repos

Register a GitHub repo once. Shuttle writes a starter `shuttle.yaml` if missing.

```bash
# Clone into ~/.shuttle/repos/<owner>/<name> (default)
shuttle import repo https://github.com/org/repo
shuttle import repo git@github.com:org/repo.git   # SSH also supported
shuttle import repo org/repo                      # shorthand, uses HTTPS

# Register an existing checkout (no clone)
shuttle import repo --path /path/to/checkout org/repo
shuttle import repo --path /path/to/checkout      # owner/name from git origin
cd /path/to/checkout && shuttle import repo .

shuttle repos list
```

Global state lives in `SHUTTLE_HOME` (default `~/.shuttle`):

```
~/.shuttle/
  registry.db              # imported repo index (path may be outside this tree)
  repos/<owner>/<name>/    # default clone location when --path / . is not used
```

## Run modes

### Issue-driven (headless)

```bash
shuttle run --issue https://github.com/org/repo/issues/1 \
            --agent cursor \
            --model claude-4-sonnet \
            --headless
```

- Fetches issue body via `gh issue view`
- Runs `pr-factory-headless`: intake → implement (agent) → test → open_pr
- No manual approval gate

### Spec file (local cwd)

```bash
cd /path/to/project
shuttle run -t pr-factory --spec specs/feature.md
```

Uses the current directory as the project checkout. Blocks at `approve_pr` unless you pass `--headless`.

### Dry run (no agents)

```bash
shuttle run -t pr-factory-dry --spec spec.md
```

## Configuration

Shuttle reads `shuttle.yaml` from the target repo. On `import repo`, Shuttle generates one if missing. You can also maintain it manually:

```yaml
default_worker: cursor
work_root: .shuttle
test:
  command: ["go", "test", "./..."]
open_pr:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
workers:
  cursor:
    bin: cursor-agent
    args: ["--trust", "--yolo", "{{if .Model}}--model{{end}}", "{{.Model}}", "--print", "{{.Prompt}}"]
  # Any CLI with a bin becomes a usable --agent:
  # goose:
  #   bin: goose
  #   args: ["run", "{{.Prompt}}"]
```

`--agent` and `--model` on `shuttle run` override the default worker and are passed into preset arg templates (`{{.Prompt}}`, `{{.Model}}`). If `--model` is unset, empty template args are dropped so the CLI is not passed blank flags.

Built-in agent presets: `claude`, `codex`, `opencode`, `cursor`. Add any other agent under `workers:` in `shuttle.yaml` and use `--agent <name>` — no code change required. List registered workers with `shuttle workers`. Also: `intake`, `shell`, `exec`, `manual`.

## Branch and PR lifecycle

Shuttle now keeps branch/PR behavior stable across retries and re-runs.

| Run type | Branch |
|----------|--------|
| `run --issue` | `shuttle/issue-<N>` |
| `run --spec` (new run) | `shuttle/run-<run-id-prefix>` |
| `retry` of a spec run | Reuses the original root run branch via `parent_run_id` |

- `implement` prompts require using `{{.Branch}}` exactly.
- After `implement`, Shuttle verifies `refs/heads/{{.Branch}}` exists on the remote.
- If remote verification is inconclusive (auth/network), Shuttle warns and continues.
- If remote verification succeeds and the branch is missing, `implement` fails compliance.
- Default `open_pr` runs `shuttle pr open`, which is idempotent:
  - existing open PR on the branch: prints `pr: <url>` and exits 0
  - no open PR: creates one
  - `gh pr list` failure: stage fails instead of silently creating

### `SHUTTLE_BIN` and wrappers

`{{.ShuttleBin}}` resolves to:

1. `SHUTTLE_BIN` if set
2. current executable path
3. fallback `shuttle` on `PATH`

Set `SHUTTLE_BIN` when using a wrapper script or non-standard install location.

### Migration for existing repos

`shuttle import repo` scaffolds new configs with the idempotent `open_pr` command, but existing repos keep their current `shuttle.yaml`. Update old configs manually:

```yaml
open_pr:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
```

You can still override `open_pr.command` with any custom workflow.

## Pipelines

Embedded templates:

- `pr-factory`: intake → implement (agent) → test → **approve_pr** → open_pr
- `pr-factory-headless`: same without the manual gate
- `sdlc`: plan → design → implement → test → review → approve_ship → ship

Local pipeline overlays can live in `pipelines/`.

## Gates

Manual stages block the run (unless `--headless`):

```bash
shuttle approve <run-id> approve_pr --message "approved"
shuttle reject <run-id> approve_pr --message "reason"
```

## Run state and logs

Per-repo run state: `<repo>/.shuttle/shuttle.db`

During `shuttle run`, progress streams to the terminal: stage start/complete lines plus live worker output (agent logs stream as they run). Stage logs are also saved under `.shuttle/runs/<run-id>/logs/`.

```bash
shuttle status <run-id>
shuttle logs <run-id>
shuttle logs <run-id> <stage-id>
shuttle cancel <run-id>
```

`shuttle serve` starts an unauthenticated local HTTP API on `127.0.0.1:8080`. Treat it as local-only.

## Trust boundary

Workers execute with the invoking user's privileges. Agent presets invoke local CLIs (`claude`, `cursor-agent`, etc.). Review pipeline YAML and `shuttle.yaml` before running.

## Concurrency

Single writer per `.shuttle/shuttle.db`. Do not run multiple Shuttle processes against the same work root concurrently.

## Listing runs

List persisted runs for the current repo:

```bash
shuttle runs list
shuttle runs list --agent cursor --status failed,running
shuttle runs list --since 7d --limit 50
shuttle runs list --model unset            # runs started without --model
shuttle runs list --json
```

Note: DURATION is wall-clock (updated_at - created_at) and therefore includes time spent blocked at approval gates; use stage logs for agent-active time. Short run IDs (first 8 chars) work anywhere a run ID is accepted.

## Resume and retry

```bash
shuttle resume <run-id>                 # continue a failed/cancelled run
shuttle retry <run-id> --agent claude   # new run, same spec, different agent
shuttle retry <run-id> --force          # retry a succeeded run (sharp edge)
```

- Resume is for terminal `failed`/`cancelled` only; use `approve`/`reject` for `blocked`.
- **Do not edit pipeline YAML for in-flight runs** — template drift blocks resume.
- Retry uses a **frozen spec** (`inputs/spec.md`); it does not re-fetch the issue — use `run --issue` for that.
- Retry does **not** clean git; inspect `git status` / open PRs first.
- Retry runs extended git preflight warnings (advisory): dirty tree, non-default branch, local branch already exists, open PR already exists.
- Resume skips retry preflight checks and continues the same run state.
- Retrying a succeeded run with `--force` can update the existing PR on that branch (it does not open a second PR).
- See `Branch and PR lifecycle` for branch naming, idempotent PR-open behavior, and migration.
- API resume/retry POSTs **block** for the full run; prefer the CLI for long recovery runs, or set a 30+ min client timeout.
- Stale logs/artifacts for reset stages are archived under `<run-dir>/.recovery/` (not deleted; not auto-pruned).
