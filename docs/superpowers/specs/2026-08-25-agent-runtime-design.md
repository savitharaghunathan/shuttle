# Agent Runtime & Issue-Driven Runs — Design

**Status:** Approved direction (2026-08-25)

## Problem

The MVP proved the scheduler, gates, and shell workers work. It is not the product. The real workflow is:

```bash
shuttle import repo https://github.com/org/repo
shuttle run --issue https://github.com/org/repo/issues/42 \
           --agent cursor \
           --model claude-4-sonnet \
           --headless
```

One issue link in. Agent runtime does the work. PR opened. No manual gates in headless mode.

## Architecture

```
~/.shuttle/                          # SHUTTLE_HOME (global)
  registry.db                        # imported repos
  repos/<owner>/<name>/              # git clones (agent cwd)

Per repo (generated on import):
  shuttle.yaml                       # test + open_pr + worker presets
  .shuttle/shuttle.db                # runs for this repo (unchanged)

CLI/API
  import repo  → clone + detect + write shuttle.yaml + register
  run --issue  → resolve repo → fetch issue → spec → pr-factory-headless
```

### Layers

| Layer | Responsibility |
|---|---|
| `internal/home` | Resolve `SHUTTLE_HOME` (~/.shuttle) |
| `internal/registry` | SQLite: imported repos (url, path, owner, name) |
| `internal/repo` | Parse URLs, clone, detect project type, scaffold config |
| `internal/issue` | Parse issue URL, fetch via `gh`, write spec.md |
| `internal/worker` | Agent presets; `Request` carries Agent + Model |
| `internal/pipeline` | `pr-factory-headless` template; `--headless` skips gates |
| `internal/cli` | `import`, `repos list`, extended `run` flags |

### Run flow (headless)

1. Parse `--issue` → owner/repo/#number
2. Lookup repo in registry (error if not imported)
3. `gh issue view` → write run inputs/spec.md
4. `StartRun` with cwd = repo checkout, agent/model on params
5. Pipeline: intake → implement (agent) → test → open_pr
6. Return run id + PR URL from open_pr log

### Agent runtime contract

Stages without explicit `worker:` use run-level `--agent` (fallback: shuttle.yaml `default_worker`).

Worker preset args support templates:

```yaml
workers:
  cursor:
    bin: cursor-agent
    args: ["--model", "{{.Model}}", "--print", "{{.Prompt}}"]
```

`Request` fields: `Agent`, `Model`, `Prompt`, `WorkDir`, `RunDir`, …

Built-in agents: `claude`, `codex`, `opencode`, `cursor`. Custom names via `workers:` map.

### Repo import contract

```bash
shuttle import repo <github-url>
```

- Clone to `{SHUTTLE_HOME}/repos/{owner}/{name}`
- Detect: `go.mod` → go test; `package.json` → npm test; `pyproject.toml` → pytest; else `true`
- Write `shuttle.yaml` if missing (never overwrite existing)
- Register in `registry.db`

### Headless vs gated

- Default `pr-factory`: keeps `approve_pr` manual gate (safe)
- `pr-factory-headless` or `--headless`: no manual stage
- `--headless` on run selects headless pipeline variant

## Non-goals (this phase)

- Multi-tenant auth / SSO
- Run resume / retry command
- Cloud workers
- Auto-import on unknown repo (explicit import required)

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- No vendor SDKs in core — agents invoked as CLIs
- Single SQLite writer per DB file
- Trust boundary unchanged: workers run as invoking user
