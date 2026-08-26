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

---

## Now

### Run recovery
**Goal:** Resume or retry a failed run from the last good stage instead of starting over.

Failed/cancelled runs are terminal today. Stage-level retries exist; run-level recovery does not.

---

## Next

### Smarter PR lifecycle
**Goal:** Handle real-world git state — existing branch, open PR for the issue, push updates instead of failing at `open_pr`.

### Zero-touch repo discovery
**Goal:** `run --issue` can resolve and register a repo the first time, without a separate import step.

Explicit import stays the default; auto-import is opt-in behavior for faster iteration.

### Full SDLC pipeline
**Goal:** Ship the embedded `sdlc` template as a first-class product path: plan → design → implement → test → review → approve → ship.

MVP proved `pr-factory`; SDLC is the longer-horizon factory mode for larger work.

---

## Future

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
