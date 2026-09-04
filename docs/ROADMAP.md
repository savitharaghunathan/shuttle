# Shuttle product roadmap

Shuttle is a **local AI software factory**: register repos, run DAG pipelines with agent runtimes, record state in SQLite, and ship from GitHub issues to PRs.

This document tracks **product capabilities**, not implementation tasks. Task breakdowns live in `docs/superpowers/plans/`; design specs in `docs/superpowers/specs/`.

---

## Shipped

### Pipeline control plane
Local DAG scheduler with stage retries, timeouts, cancel, and human approval gates. Runs are persisted per repo; CLI and localhost HTTP API share the same engine.

### Issue → PR factory
Headless workflow: import a repo, point at a GitHub issue, agent implements, tests run, PR opens. No manual gate in headless mode.

### Repo onboarding
Register a project once via clone (HTTPS/SSH) **or** point at an existing checkout (`--path`, `.`). Auto-scaffold `shuttle.yaml` (test command, PR command, agent presets). Global registry under `SHUTTLE_HOME`.

### Agent runtimes
Built-in CLI adapters for Claude, Codex, Cursor, and OpenCode. Any agent defined in `shuttle.yaml` `workers:` is usable via `--agent`. Run-level `--agent` and `--model`; live progress streaming during runs.

### Developer ergonomics
Dry-run pipelines, per-stage logs, `approve`/`reject` gates, and embedded templates (`pr-factory`, `pr-factory-headless`, `sdlc`).

### Runs view + recovery
List and compare runs (agent, status, time, issue, stage) via `shuttle runs list` and `GET /v1/runs`. Resume failed/cancelled runs from the earliest failed stage; retry any terminal run against its frozen spec with optional agent/model overrides. Run metadata persists across sessions.

### Smarter PR lifecycle
Deterministic feature branches per run (`shuttle/issue-N` for issues, `shuttle/run-<id>` for specs; spec retries inherit via `parent_run_id`). `implement` prompts pin the branch; post-implement compliance checks the remote ref. Idempotent `shuttle pr open` reuses an existing PR instead of failing on retry. Extended retry preflight warns on dirty tree, leftover branch, and open PR.

---

## Next

### Zero-touch repo discovery
**Goal:** `run --issue` can resolve and register a repo the first time, without a separate import step.

Explicit import stays the default; auto-import is opt-in behavior for faster iteration.

### Full SDLC pipeline
**Goal:** Ship the embedded `sdlc` template as a first-class product path: plan → design → implement → test → review → approve → ship.

Implementation complete on `feat/sdlc-pipeline`; mark Shipped after dogfood checklist.

Capabilities ready on the feature branch:

- `sdlc` and `sdlc-headless` embedded templates with plan, design, implement, test, review, and ship stages.
- Content injection for spec, upstream stage summaries, and review test-log tail, with a 128 KiB cap per injected body.
- Strict output enforcement and non-production ship-command guard for embedded SDLC templates only.
- `--headless -t sdlc` selects `sdlc-headless`; bare `run --issue --headless` remains `pr-factory-headless`.
- Read-only planning/design/review stages use `readonly_worker`; `--agent` applies to implement only and `--model` applies to all agent stages.
- SDLC retry rebuilds from `plan` using the frozen spec and warns that prior artifacts are discarded.

Dogfood checklist before moving to Shipped: run gated SDLC in a real repo with scaffolded `ship.command`, inspect plan/design/implement artifacts, approve `approve_ship`, confirm PR open is idempotent, and optionally run `sdlc-headless` on a throwaway spec.

---

## Future

### Local web dashboard
Browser UI on `shuttle serve`: list runs, drill into status/logs, resume/retry — built on the runs list API.

### Remote workers
Run agent stages on another machine or pool while keeping the control plane local.

### Team / shared factory
Multi-user registry, auth, and shared run history — beyond single-user local trust boundary.

### Pipeline marketplace
Share and overlay pipeline templates across repos (`pipelines/` today is local-only).

---

## Out of scope (for now)

- Multi-tenant auth / SSO
- Vendor SDKs in core (agents stay CLI-invoked)
- Concurrent writers to the same `.shuttle/shuttle.db`

---

## How to use this doc

| Change | Update here |
|--------|-------------|
| Feature shipped on `main` | Move from **Now** / **Next** → **Shipped** |
| New product idea | Add under **Next** or **Future** as a capability, not a task |
| Ready to build | Write spec in `docs/superpowers/specs/`, plan in `docs/superpowers/plans/` |
