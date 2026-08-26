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
```

You can also run without building:

```bash
go run ./cmd/shuttle workers
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
  command: ["gh", "pr", "create", "--fill"]
workers:
  cursor:
    bin: cursor-agent
    args: ["--trust", "--yolo", "--print", "{{.Prompt}}"]
```

`--agent` and `--model` on `shuttle run` override the default worker and are passed into preset arg templates.

Built-in agent runtimes: `claude`, `codex`, `opencode`, `cursor`. Also: `intake`, `shell`, `exec`, `manual`.

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
