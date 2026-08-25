# shuttle

Shuttle is a local AI software factory control plane. It runs small DAG pipelines, records each run in SQLite, writes run artifacts under `.shuttle/`, and pauses at manual gates before actions such as opening a PR or shipping.

The MVP is intentionally local-first: one process, one SQLite database, one writer, and explicit human approval before sensitive stages.

## Install

Build the CLI from the repository:

```bash
go build -o bin/shuttle ./cmd/shuttle
```

Then run it from a project directory:

```bash
./bin/shuttle workers
./bin/shuttle run -t pr-factory --spec path/to/spec.md
```

You can also run without building:

```bash
go run ./cmd/shuttle workers
```

## Configuration

Shuttle reads `shuttle.yaml` from the current directory. If it is absent, design defaults are used.

```yaml
default_worker: claude
work_root: .shuttle
concurrency: 2
default_timeout: 30m
test:
  command: ["go", "test", "./..."]
open_pr:
  command: ["echo", "open_pr skipped: configure open_pr.command"]
ship:
  command: ["echo", "ship skipped: configure ship.command"]
workers:
  shell:
    shell: /bin/bash
  cursor:
    bin: cursor-agent
    args: ["--print", "{{.Prompt}}"]
```

`default_worker` is used for stages that do not name a worker. Built-in worker names include `intake`, `shell`, `exec`, `claude`, `codex`, `opencode`, and `cursor`.

Configure `open_pr.command` before using a pipeline that reaches `open_pr`. The default is a safe echo:

```yaml
open_pr:
  command: ["gh", "pr", "create", "--fill"]
```

## Pipelines

Embedded templates:

- `pr-factory`: intake a spec, implement, test, pause at `approve_pr`, then run `open_pr`.
- `sdlc`: plan, design, implement, test, review, pause at `approve_ship`, then run `ship`.

Local pipeline overlays can live in `pipelines/`. For example, this repo includes `pipelines/pr-factory-dry.yaml`, which uses shell commands for a dry PR factory run:

```bash
shuttle run -t pr-factory-dry --spec spec.md
shuttle run -t pipelines/pr-factory-dry.yaml --spec spec.md
```

## Gates

Manual stages block the run and print approval hints:

```bash
shuttle approve <run-id> approve_pr --message "approved"
shuttle reject <run-id> approve_pr --message "reason"
```

Approving a gate records the approval under `.shuttle/runs/<run-id>/approvals/` and resumes the run. Rejecting a gate fails the run and skips downstream pending stages.

## Run State And Logs

All run state is stored in `.shuttle/shuttle.db`; artifacts are written under `.shuttle/runs/<run-id>/`.

```bash
shuttle status <run-id>
shuttle logs <run-id>
shuttle logs <run-id> <stage-id>
shuttle cancel <run-id>
```

`shuttle serve` starts an unauthenticated local HTTP API on `127.0.0.1:8080`. Treat it as local-only.

## Trust Boundary

Only run trusted pipeline YAML and trusted `shuttle.yaml` files. Workers execute with the invoking user's privileges:

- `shell` and `exec` run configured commands in the project directory.
- Agent presets invoke local CLIs such as `claude`, `codex`, `opencode`, or `cursor-agent`.
- `open_pr.command` and `ship.command` are ordinary commands and can perform side effects.

Review commands before running a pipeline, especially when accepting YAML from another repository or branch.

## Concurrency And Retry Model

This MVP assumes a single writer to `.shuttle/shuttle.db`. Do not run multiple Shuttle processes against the same work root at the same time.

There is no run resume command and no automatic retry command in the MVP. Stage definitions may carry retry metadata, but failed terminal runs are not resumed in place. Start a new run after changing code, config, or a pipeline.
