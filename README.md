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

### Issue-driven PR factory (headless)

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

### Full SDLC (plan → PR)

Use SDLC for larger work that benefits from separate planning, design, implementation, test, and review stages:

```bash
shuttle run -t sdlc --spec specs/feature.md
shuttle run -t sdlc --issue https://github.com/org/repo/issues/42
shuttle run -t sdlc --spec specs/feature.md --headless
```

`sdlc` runs `plan` → `design` → `implement` → `test` → `review` → `approve_ship` → `ship`. `sdlc-headless` skips `approve_ship`; `--headless -t sdlc` selects `sdlc-headless`. Bare `run --issue --headless` still selects `pr-factory-headless`, not SDLC.

`pr-factory` is the fast issue-to-PR path: intake, implement, test, and open a PR. `sdlc` is the fuller factory path: it injects the spec or issue body into `plan`, injects upstream artifact content into each later prompt, and persists stage outputs under `.shuttle/runs/<run-id>/artifacts/<stage-id>/summary`. Prompts receive artifact **content**, not just artifact paths, so stdout-only agents can participate. Each injected artifact body is capped at 128 KiB.

Headless SDLC is higher risk than headless `pr-factory`: `review` receives the implement summary plus the tail of `logs/test.log`, but review is advisory and does **not** block `ship`. Use headless SDLC only for trusted automated paths; gated SDLC lets you inspect artifacts before `approve_ship`.

SDLC has four agent stages (`plan`, `design`, `implement`, `review`), so expect roughly 4× agent invocation cost and longer wall time. `--model` applies to every agent stage. `--agent` applies only to `implement`; read-only stages use `readonly_worker` (default `claude`) unless a local pipeline explicitly sets a worker.

Retrying an SDLC run starts a new run from the frozen `inputs/spec.md` and discards prior plan/design/implement artifacts, rebuilding the chain from `plan`; the CLI prints a stderr hint for SDLC retries. Concurrent runs on the same issue are especially risky for SDLC because multiple four-stage runs may compete on the same `shuttle/issue-N` branch.

Issues remain untrusted input. SDLC exposes issue text to four agent stages, so use `--issue -t sdlc` only with trusted issues and repos.

### Dry run (no agents)

```bash
shuttle run -t pr-factory-dry --spec spec.md
```

## Configuration

Shuttle reads `shuttle.yaml` from the target repo. On `import repo`, Shuttle generates one if missing. You can also maintain it manually:

```yaml
default_worker: cursor
readonly_worker: claude
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

`--agent` and `--model` on `shuttle run` override the default worker for standard agent stages and are passed into preset arg templates (`{{.Prompt}}`, `{{.Model}}`). SDLC read-only stages (`plan`, `design`, `review`) use `readonly_worker` and ignore `--agent`; `implement` uses `--agent`. If `--model` is unset, empty template args are dropped so the CLI is not passed blank flags.

Built-in agent presets: `claude`, `codex`, `opencode`, `cursor`. Add any other agent under `workers:` in `shuttle.yaml` and use `--agent <name>` — no code change required. List registered workers with `shuttle workers`. Also: `intake`, `shell`, `exec`, `manual`.

Sharp edge: repo-mutating presets such as `cursor` with `--yolo` should not be used for SDLC read-only stages. The embedded templates mark those stages `readonly: true`; if a local pipeline overlay sets `worker: cursor` on them, that overlay is choosing to give the agent write access.

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

`shuttle import repo` scaffolds new configs with idempotent PR commands, but existing repos keep their current `shuttle.yaml`. SDLC `ship` fails fast on known placeholder echo commands:

```text
non-production ship command is not allowed for sdlc
```

Update old configs manually:

```yaml
open_pr:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
ship:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
```

You can still override `open_pr.command` with any custom workflow.

## Pipelines

Embedded templates:

- `pr-factory`: intake → implement (agent) → test → **approve_pr** → open_pr
- `pr-factory-headless`: same without the manual gate
- `sdlc`: plan → design → implement → test → review → approve_ship → ship
- `sdlc-headless`: same without the manual gate

Local pipeline overlays can live in `pipelines/`. Local `pipelines/sdlc-*` overlays are bridge mode unless their template ID is exactly embedded `sdlc` or `sdlc-headless`; experimental overlays such as `sdlc-dry` or `sdlc-foo` do not get strict SDLC output or ship-guard behavior automatically.

## Gates

Manual stages block the run (unless `--headless`):

```bash
shuttle approve <run-id> approve_pr --message "approved"
shuttle reject <run-id> approve_pr --message "reason"
```

The stage id is optional when exactly one stage is waiting, so gated SDLC needs only `shuttle approve <run-id>`. Run ids accept an 8-character-or-longer prefix everywhere.

Before approving `approve_ship`, read what the agents produced:

```bash
shuttle artifacts <run-id>          # list deliverables with sizes and paths
shuttle artifacts <run-id> plan     # print one stage's deliverable
```

## Run state and logs

Per-repo run state: `<repo>/.shuttle/shuttle.db`

During `shuttle run`, progress streams to the terminal: stage start/complete lines plus live worker output (agent logs stream as they run). Stage logs are also saved under `.shuttle/runs/<run-id>/logs/`.

```bash
shuttle status <run-id>                    # stages plus branch, run dir, outputs mode
shuttle status <run-id> --json
shuttle logs <run-id>
shuttle logs <run-id> <stage-id>
shuttle logs <run-id> --stage implement --tail 50
shuttle artifacts <run-id>
shuttle cancel <run-id>
```

`status` reports `outputs: strict` for the embedded `sdlc` templates and `outputs: bridge` for overlays that declare stage outputs without strict enforcement, so a local `pipelines/sdlc-*.yaml` run is distinguishable from a real one.

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
