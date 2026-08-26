# Runs View + Recovery — Design

**Status:** Approved direction (2026-08-26, rev 3)

## Amendments log

- **rev 3:**  Ratified resume-point **attempt reset to 0** (the brainstorm said "attempt unchanged"; changed deliberately to restore the stage retry budget after run-level recovery). Narrowed retry git preflight to dirty-tree + current-branch checks (branch/PR-collision detection deferred to Smarter PR lifecycle). Added `.recovery/` retention note, spec-staleness note for issue-driven retry, explicit stale-worker validation scope, and an explicit "resume-from-blocked is not allowed" line.
- **rev 2:** Added delivery phasing, template-drift contract, archive-not-delete cleanup, stage-ordering rule, `wall_duration` naming, model-comparison limit.

## Delivery phasing

Ship in two plans to reduce delivery risk (each independently testable and mergeable):

- **Plan 1 — Runs view + metadata:** migration, `runOpts` persistence fix, `ResolveRunID`, `runs list` (CLI + `GET /v1/runs`), extended get/create JSON. No recovery yet.
- **Plan 2 — Recovery:** `resume`, `retry`, resume/retry API, artifact cleanup, template validation.

Both plans share this spec. Non-goals and constraints apply to both.

## Problem

Shuttle persists runs per repo, but you cannot **see**, **compare**, or **continue** them well:

| Gap | Today |
|-----|--------|
| Run metadata | `runs` stores only `id`, `template_id`, `status`, `cwd`, `run_dir`, timestamps — no agent, model, or issue |
| Agent/model on resume | `runOpts` in the engine is memory-only; a new CLI/API session loses `--agent` / `--model` |
| List runs | No `ListRuns` in store; no `shuttle runs list`; API has `GET /v1/runs/{id}` only |
| Recovery | Terminal runs (`failed`, `cancelled`) cannot be continued; only `blocked` runs resume via `approve` |
| Run ID UX | Only full UUIDs work; no prefix resolution |

## Goal

1. **Runs list** — CLI table (+ JSON) and `GET /v1/runs` to compare runs by agent, status, time, issue, template, and stage
2. **Run recovery** — `shuttle resume` (same run ID) and `shuttle retry` (new run, same spec)
3. **Persist metadata** — agent, model, issue_ref, parent_run_id in SQLite; restore agent/model on fresh sessions
4. **Run ID prefix** — resolve unique prefixes (min 8 chars) across all run-targeting commands

## Non-goals

- Web dashboard (Future in `docs/ROADMAP.md`)
- Global cross-repo run index (runs stay per-repo in `<repo>/.shuttle/shuttle.db`)
- Concurrent multi-process writers to the same DB
- Re-running succeeded stages in-place
- `--stage` flag on resume (always picks the resume point algorithm below)
- `effective_model` / per-run config snapshot (model baked into presets is not captured; see Model-comparison limit)
- Automatic git cleanup on retry (branch delete, stash, reset — see Retry git state)
- Async job IDs / webhooks for long-running API calls (localhost v1 stays synchronous)
- Git/PR collision handling on retry (deferred to **Smarter PR lifecycle** in ROADMAP)
- Pipeline versioning / template snapshots at run start (v1 validates against current template; see Template stability)

## Architecture

```
CLI (runs list, resume, retry) ──┐
API (GET /v1/runs, POST resume/retry) ──┼──► Engine (StartRun, ResumeRun, RetryRun, EnsurePipeline)
                                         └──► Store (runs + metadata, ListRuns, ResolveRunID, stage_runs)
```

### Layers

| Layer | Change |
|-------|--------|
| `internal/store` | Additive migration on `runs`; index on `created_at`; `ListRuns(opts)` with filters; `ResolveRunID(prefix)`; transactional `ResetStages` batch for resume |
| `internal/pipeline` | Persist/restore `runOpts`; template validation + drift error contract; stale-worker validation; log/artifact **archive** to `.recovery/` on resume; git preflight (go-git) on retry; `ResumeRun`, `RetryRun`; stage logic uses **template order** |
| `internal/cli` | `runs list` (memoized template loads), `resume`, `retry`; `resolveRunID` helper wired into all run-targeting commands |
| `internal/api` | `GET /v1/runs`; extend create/get JSON (`wall_duration`, metadata); `POST` resume/retry (retry `warnings[]`); same `ResolveRunID` via store |
| `README` | Document runs list, resume/retry, duration semantics (`wall_duration`), **CLI as the supported path for long recovery runs** (API POSTs block), **do not edit pipeline YAML for in-flight runs** (drift blocks resume), retry git state + `--force`, retry uses a **frozen spec** (not re-fetched from the issue), model-comparison limit |

### Stage ordering rule (critical)

`GetStageRuns` returns rows ordered by insert ID, **not** pipeline order. All resume, retry, and current-stage logic **must** iterate `pl.Stages` from `TemplateLoader` and join status from a `stage_id → StageRun` map. Never infer pipeline order from DB row order.

### Template stability (v1)

v1 does **not** snapshot pipeline definitions at run start. Resume, retry, and list logic load the **current** template for `runs.template_id`.

**Validation** (before `ResumeRun`, and when deriving `current_stage` for list/API):

1. Load template via `TemplateLoader(template_id)`.
2. Every `stage_runs.stage_id` for the run must exist in `pl.Stages`.
3. Every `pl.Stages[].ID` must have a matching `stage_runs` row for the run.

If (1) fails (template missing): see Current stage fallback below; `ResumeRun` / `RetryRun` error with `template … not found`.

If (2) or (3) fails: `ResumeRun` / `RetryRun` return an error. List/API set `current_stage` to `"unknown"`.

**Error message contract (drift):** the error names the template id and the specific mismatch, and points to recovery:

```
template "pr-factory" no longer matches run a1b2c3d4:
  missing stages in template: [open_pr]     # in DB, not in current template
  extra stages in template:   [security]    # in current template, not in DB
restore the template definition, or start fresh with: shuttle retry a1b2c3d4
```

**Stale worker validation (resume/retry):** the persisted resolved `agent` (and each per-stage `worker`) must still resolve in the current `worker.Registry`. **Scope:** validate the `worker` field of every `stage_runs` row for the run that is not a built-in (`manual`, `shell`, `intake`) — i.e. agent-backed stages. Built-ins are always registered. Before `ResumeRun` continues, and before `RetryRun` starts, each such worker name must exist in the registry. If not:

```
worker "cursor" is no longer configured for run a1b2c3d4
add it back to shuttle.yaml workers:, or retry with a different agent:
  shuttle retry a1b2c3d4 --agent claude
```

`resume` cannot override the agent (it continues the same run), so a missing worker on resume is a hard error with the retry hint. `retry --agent X` re-resolves against the current registry and is the escape hatch.

Embedded templates (`pr-factory`, etc.) and built-in workers are treated as stable. Custom `pipelines/*.yaml` and `workers:` edits between run start and recovery are user responsibility; validation prevents silent corruption or opaque mid-run failures.

## Data model

Additive columns on `runs` (nullable for old rows):

| Column | Purpose |
|--------|---------|
| `agent` | **Resolved worker name** at run start (after `applyDefaults`), e.g. `cursor`, `claude` — always set for new runs |
| `model` | Run-level `--model` (empty if not passed) |
| `issue_ref` | e.g. `org/repo#42` (empty for `--spec` runs) |
| `parent_run_id` | Lineage for `retry`; references `runs.id` (app-validated; no SQLite FK — ALTER cannot add self-FK) |

**Effective agent at start:** In `StartRun`, call `applyDefaults` first, then persist the worker name that implement stages will use (config `DefaultWorker` when `--agent` omitted). This keeps experiment comparison stable even if `shuttle.yaml` changes later.

**Model-comparison limit (documented, not fixed in v1):** `model` captures only the run-level `--model` value. It does **not** capture a model baked into a worker preset's args in `shuttle.yaml` (e.g. `workers.cursor` args without `{{.Model}}`). Two runs can show the same `model` (or both `(default)`) yet execute different models if the preset changed between runs. v1 accepts this; README states it explicitly. A future `effective_model` (resolved from preset render) or a per-run config snapshot is out of scope here (listed under Non-goals).

**Engine fix:** persist agent/model in `StartRun`; in `EnsurePipeline`, load into `runOpts` from DB and pass `run.Agent` to `applyDefaults` so approve/resume/retry work from a fresh process.

**Issue ref:** derived at run start from `--issue` URL as `org/repo#N`.

**parent_run_id validation:** `CreateRun` rejects unknown parent IDs. Value may be full UUID or resolved via `ResolveRunID`. Because there is no SQLite FK (see below) and v1 never deletes runs, orphaned `parent_run_id` values are only possible via manual DB edits; the list view tolerates a dangling parent (shows the prefix, no join required).

**Index:** `CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at DESC)` for `--since` / default sort (table scans acceptable at v1 limits).

### Run ID prefix resolution

`ResolveRunID(prefix)` — **single implementation in store**; all CLI commands and API handlers call this (no ad-hoc prefix matching):

- **Normalize input first:** `strings.TrimSpace` + `strings.ToLower`. `google/uuid` emits lowercase hyphenated IDs; normalization lets users paste mixed-case copies. Hyphens are significant and preserved (no hyphen stripping — an 8-char prefix stays within the first UUID group).
- If `prefix` matches a full UUID exactly → that run
- If `len(prefix) < 8` → error ("prefix too short")
- Else match runs where `id LIKE prefix||'%'` (lowercase UUID hex + hyphens)
  - 0 matches → not found
  - 1 match → that run
  - 2+ matches → error listing ambiguous matches (`ambiguous run ID prefix: …`)

Apply to **all** run-targeting surfaces: `status`, `logs`, `approve`, `reject`, `cancel`, `resume`, `retry`, `--parent` filter value, and API `GET/POST /v1/runs/{id}/*`.

### Duration (wall-clock)

Computed at list time as `updated_at - created_at` (or `now - created_at` if still running).

**Includes** time spent `blocked` at approval gates (human wait). This is intentional wall-clock duration for experiment comparison, not agent-active time. README: use stage logs for agent execution time; a future enhancement may add `active_duration`.

**JSON field name:** serialized as `wall_duration` (seconds, integer) to prevent misreading it as agent-active time. The CLI **DURATION** column renders the same value human-friendly (`4m12s`). Reserving `active_duration` for the future avoids a later breaking rename.

### Current stage (for list / JSON)

Derived at query time using **template order** (`pl.Stages`) after template validation (see Template stability):

1. First stage with status `running`
2. Else first stage with status `waiting_approval`
3. Else last stage (in pipeline order) with status `failed`, `cancelled`, or `rejected`
4. Else if run status is `cancelled`: first `skipped` stage in pipeline order after the longest initial `succeeded` prefix (covers cancel-during-blocked, where the gate becomes `skipped`)
5. Else last stage with status `succeeded`
6. Else `—`

**Fallback:** if template cannot be loaded or fails validation → `current_stage: "unknown"` (CLI column shows `unknown`; JSON string `"unknown"`). Run row is still listed; no error for the whole list.

**Performance (v1 cost model):** `current_stage` derivation loads and validates a template **per listed run**. At default `--limit 20` this is ≤20 template loads per `runs list` / `GET /v1/runs` call. This is O(n) in templates loaded and is acceptable at v1 list sizes.

To keep it bounded:

- **Memoize** `TemplateLoader` results **per list call** by `template_id` (a `map[string]*Pipeline` local to the request). Repeated templates across rows load once. Embedded templates (the common case) are effectively free.
- Enforce the `--limit` cap (default 20) as the hard bound on template loads.
- Do **not** add a denormalized `current_stage` column in v1 (avoids write-path complexity and drift); revisit only if profiling shows list latency problems on large custom-pipeline repos.

`--json` and the table share the same derivation path (compute once).

### Display defaults

| Field | Empty in DB (legacy rows) | Shown in list |
|-------|---------------------------|---------------|
| `agent` | pre-migration run | `(unknown)` |
| `model` | not set | `(default)` |

New runs always have `agent` populated.

## CLI contract

### Runs list

```bash
shuttle runs list
shuttle runs list --agent cursor --status failed,running
shuttle runs list --issue org/repo#42 --template pr-factory-headless
shuttle runs list --since 7d --limit 50
shuttle runs list --parent a1b2c3d4
shuttle runs list --json
```

**Columns (all shown in default table):**

| RUN | PARENT | STATUS | AGENT | MODEL | TEMPLATE | ISSUE | STARTED | DURATION | STAGE |
|-----|--------|--------|-------|-------|----------|-------|---------|----------|-------|

- **RUN** — first 8 chars of UUID
- **PARENT** — first 8 chars of `parent_run_id` for retried runs; blank otherwise (makes retry lineage visible without `--json`)
- **ISSUE** — blank for `--spec` runs

**Filters:**

| Flag | Behavior |
|------|----------|
| `--agent` | exact match on persisted resolved `agent` |
| `--model` | exact match on stored value. To match runs with **no** model set (empty string in DB), use `--model unset` (shell-friendly sentinel); `--model ""` also works but is awkward from bash |
| `--template` | exact match on `template_id` |
| `--issue` | exact match on `issue_ref` |
| `--status` | comma-separated (`failed,running`) |
| `--since` | relative (`7d`, `24h`) or date (`2026-08-01`) |
| `--parent` | exact match on `parent_run_id`; value resolved via `ResolveRunID` (full UUID or unique prefix) |
| `--limit` | default 20 |
| `--json` | JSON array instead of table |

**Scope:** current repo's `.shuttle/shuttle.db` (same cwd semantics as `status` today). Default sort: newest first.

### Resume

```bash
shuttle resume <run-id>
```

**When allowed:** run status is `failed` or `cancelled` only (not `succeeded`, `running`, or `blocked`).

> **Not for `blocked` runs.** A `blocked` run is waiting at a manual gate — drive it with `shuttle approve` / `reject` (which continue cross-session via `EnsurePipeline`). `resume` is only for runs that have reached terminal `failed`/`cancelled`. To abandon a blocked run and resume later, `cancel` it first.

**Precondition:** template loads and passes validation (Template stability), **and** the persisted resolved `agent` / stage workers still resolve in the current registry (stale-worker rule). Otherwise error before any mutation.

**Resume point** — earliest stage in **pipeline order** matching the first applicable rule:

| Priority | Stage status | Typical case |
|----------|--------------|--------------|
| 1 | `failed` | worker exhausted retries |
| 2 | `cancelled` | cancel mid-flight |
| 3 | `rejected` | manual gate rejected |
| 4 | `skipped` | only when run status is `cancelled` and no stage matched 1–3 (cancel-during-blocked) |

If no resume point → error ("no stage to resume").

**Reset behavior:**

1. Resume-point stage → `ready`; **reset `attempt` to 0** — restores the stage-level retry budget after run-level recovery (e.g. agent exhausted `retries` before run failed)
2. Clear `summary` and `log_path` on resume-point stage
3. **Artifact scope (resume point + all downstream stages in pipeline order):** target `<run-dir>/artifacts/<stage-id>/` (and legacy single-file `<run-dir>/artifacts/<stage-id>`). Do **not** touch upstream artifacts, `inputs/spec.md`, or upstream approval files.
4. All downstream stages → `pending`; clear `summary`, `log_path`; reset `attempt` to 0; same artifact scope as (3)
   - Includes downstream stages currently `skipped`
5. Upstream `succeeded` stages unchanged
6. Run status → `running`; invoke `RunUntilBlockOrDone`

Preserves run dir, upstream artifacts, inputs, and approval history.

**Cleanup ordering, atomicity, and log retention:**

Resume mutates both SQLite and the filesystem. To keep them consistent and preserve the audit trail:

1. **Archive, don't delete, logs.** Instead of removing files at `log_path`, move stale logs/artifacts for reset stages into `<run-dir>/.recovery/<UTC-timestamp>/<stage-id>/`. This keeps the audit trail (review item: cleanup was previously lossy) while guaranteeing the resumed stage starts with a clean `artifacts/<stage-id>/` so agents never read stale outputs.
2. **Filesystem first, then DB, in one transaction.** Perform the archive moves first. Only after all moves succeed, apply the stage-status updates inside a **single store transaction** (new `ResetStages(tx)`-style batch), then flip run status to `running`. If any archive move fails, abort before touching the DB → run stays terminal, safe to re-invoke `resume`.
3. **Idempotent / best-effort within a step.** A missing source path (stage produced no artifacts/logs) is not an error — skip it. Moves use rename within the same run dir (same filesystem, atomic); fall back to copy+remove only if rename fails cross-device (should not happen inside one run dir).
4. **Partial-failure contract.** DB updates are all-or-nothing (transaction). Filesystem archive completes before the transaction, so the worst case on crash is orphaned files under `.recovery/` (harmless, ignored by the engine) with the DB unchanged — resume can be retried safely.

`.recovery/` is internal bookkeeping: excluded from `Artifacts` template resolution and never surfaced as stage output.

**Retention (v1):** `.recovery/` archives are **not** pruned automatically — repeated resumes on a flaky stage accumulate timestamped snapshots under `<run-dir>/.recovery/`. This is bounded in practice by run count and acceptable at v1 scale (single-user, per-repo DB). Operators can delete `.recovery/` manually; the engine ignores it. Automatic retention (keep last N per stage) is a future enhancement, not v1.

**Manual gates after resume:** a resumed `rejected` or `skipped` manual gate returns to `ready` with cleared summary; on next `Tick` it transitions to `waiting_approval` again (existing manual-gate behavior).

### Retry

```bash
shuttle retry <run-id> [--agent X] [--model Y] [--force]
```

**When allowed:**

| Parent status | Allowed |
|---------------|---------|
| `failed`, `cancelled` | yes |
| `succeeded` | only with `--force` |
| `running`, `blocked`, `pending` | no |

Parent must have `inputs/spec.md`.

**Behavior:**

1. **Git preflight** on parent `cwd` (go-git): warn on dirty working tree and/or non-default current branch (see Retry git state). Warnings surface on CLI stderr / API `warnings[]`; retry proceeds regardless.
2. Validate parent worker(s) resolve, or a `--agent` override is supplied (stale-worker rule).
3. Create fresh run dir (empty artifacts — no stale agent context)
4. Copy spec from parent run (`inputs/spec.md` → new run)
5. Inherit template, cwd, agent, model, issue_ref from parent
6. Optional `--agent` / `--model` overrides (re-resolve effective `agent` after override via `applyDefaults`)
7. Set `parent_run_id` → original run
8. Start and run full pipeline from scratch

#### Spec staleness (issue-driven runs)

`retry` copies the **frozen** `inputs/spec.md` captured at the parent run's start. For issue-driven runs (`issue_ref` set), the upstream GitHub issue may have changed since — new comments, edited body, closed/reopened. v1 deliberately retries against the frozen snapshot (reproducible experiments; "same input, different agent"). It does **not** re-fetch the issue.

To retry against the current issue, start a fresh run with `shuttle run --issue <url>` instead of `retry`. README documents this distinction so operators don't assume `retry` re-reads the issue.

#### Retry git state (AI factory) — the primary risk

Retry uses a **frozen spec snapshot** and the **same `cwd`** as the parent, and runs the **full pipeline from scratch** (fresh run dir, empty artifacts). Shuttle deliberately does **not** manipulate the user's git repository: it does not stash, reset, checkout, delete branches, or close PRs. This is the highest-risk area because in `pr-factory[-headless]` the `implement` stage instructs the agent to commit on a feature branch and push.

**Expected repository state going into a retry:**

| Parent outcome | Likely git state at parent's `cwd` | What the retried agent sees |
|----------------|-----------------------------------|-----------------------------|
| `implement` failed | uncommitted/partial changes, possibly a half-created feature branch, no push | a dirty tree and/or existing branch from the prior attempt |
| `test` failed | committed + pushed branch, failing tests | existing branch/commits; a new push may update the same branch |
| `open_pr` failed | branch pushed, PR open or partially created | branch exists remotely; `open_pr` may fail "PR already exists" |
| `cancelled` mid-flight | indeterminate (depends on stage reached) | whatever the interrupted stage left behind |

**What Shuttle guarantees:** a clean **run workspace** (`inputs/spec.md` copied, empty `artifacts/`). It does **not** guarantee a clean **git working tree** — that is the user's repository, outside Shuttle's trust boundary.

**Preflight (implemented in v1 — scoped narrowly):** before starting the retried run, `RetryRun` inspects the parent `cwd` git state (via go-git, Apache-2.0 — no shelling out) and records warnings. To keep detection reliable and tests deterministic, v1 checks exactly two conditions:

1. **Dirty working tree** — uncommitted or untracked changes (`go-git` `Worktree.Status`). Warning: `uncommitted changes in <cwd>`.
2. **Non-default current branch** — `HEAD` is not on the repo's default branch (`main`/`master`), signalling a leftover feature branch checkout. Warning: `on non-default branch <name>`.

Both are **warn-and-proceed** (CLI stderr; API response `warnings[]`); retry never mutates git. Retry's purpose is often exactly "same messy state, different agent."

**Explicitly deferred (not in v1 preflight):** enumerating all local/remote feature branches, matching a branch to the issue ref, and detecting an already-open PR. These require a branch-naming convention Shuttle does not yet define (`pr-factory-headless.yaml` `implement` prompt says only "a feature branch") and overlap with **Smarter PR lifecycle** in ROADMAP. The `open_pr` "PR already exists" failure remains a run-time stage failure, surfaced through normal logs, not a preflight warning.

**No auto-cleanup in v1.** Automatic git cleanup (stash/reset/branch delete) is a **Non-goal** — it risks destroying user work and overlaps with **Smarter PR lifecycle** in ROADMAP (push-updates-to-existing-branch, reuse-open-PR). Until that ships:

- `retry` without `--force` stays limited to `failed`/`cancelled` (the common agent-swap path).
- `--force` (succeeded parent) is the sharp-edged case: branch exists, PR open, tree may be dirty → `open_pr` will likely fail. README documents inspecting `git status` / open PRs first.

**README must document:** retry does not clean git; inspect `git status` and open PRs before retrying; prefer starting each experiment from a clean tree when comparing agents; failure modes at `open_pr` and how **Smarter PR lifecycle** will address them.

### Blocked runs

Unchanged — `shuttle approve` / `reject` (cross-session via `EnsurePipeline`). Use `resume` only after the run reaches a terminal `failed` or `cancelled` state.

## API contract

### List runs

```
GET /v1/runs?limit=20&status=failed,running&agent=cursor&model=&template=pr-factory-headless&issue=org/repo#42&since=7d&parent=<run-id-or-prefix>
```

Response: JSON array with `id`, `status`, `agent`, `model`, `template_id`, `issue_ref`, `created_at`, `updated_at`, `wall_duration` (integer seconds; see Duration), `current_stage`, `parent_run_id`.

Register `GET /v1/runs` **before** `GET /v1/runs/{id}` in the router to avoid path conflicts.

### Create run (extend existing)

`POST /v1/runs` body adds optional fields:

```json
{
  "template_id": "pr-factory-headless",
  "spec_path": "...",
  "cwd": "...",
  "agent": "cursor",
  "model": "claude-4-sonnet",
  "issue_url": "https://github.com/org/repo/issues/42"
}
```

`issue_url` resolves to `issue_ref` the same way as CLI `--issue`. Persisted metadata matches CLI-created runs (including resolved `agent`).

### Get run (extend existing)

`GET /v1/runs/{id}` — add `agent`, `model`, `issue_ref`, `parent_run_id` to existing response. Accepts run ID prefix.

### Recovery

```
POST /v1/runs/{id}/resume     # no body
POST /v1/runs/{id}/retry      # optional JSON: { "agent": "...", "model": "...", "force": true }
```

Both accept run ID prefix. Both invoke `RunUntilBlockOrDone` synchronously (same as create/approve today).

`POST /v1/runs/{id}/retry` response includes any git preflight warnings:

```json
{ "id": "<new-run-id>", "parent_run_id": "<old-run-id>", "status": "...", "warnings": ["uncommitted changes in <cwd>", "on non-default branch feature/x"] }
```

`warnings` is omitted/empty when the tree is clean.

**Known limitation — synchronous long calls (v1):** agent stages may run for many minutes and localhost v1 has **no async job model**; resume/retry/create block until the run reaches a terminal or blocked state. Mitigations, documented in README:

- Set HTTP client read timeout to **at least 30 minutes**, or drive long runs via the CLI and use the API only for `list`/`status`.
- Cancel an in-flight API-driven run with `shuttle cancel <id>` from another terminal.

This is an accepted v1 tradeoff (localhost, single-user trust boundary). An async job model (job IDs + polling/webhooks) is deferred to the web dashboard effort and listed under Non-goals.

### Errors

| Case | HTTP |
|------|------|
| Run not found | 404 |
| Ambiguous run ID prefix | 400 |
| Invalid state (e.g. resume a running run) | 409 |
| Retry from `succeeded` without `force` | 409 |
| Template missing or no longer matches run | 409 |
| Worker no longer configured (resume; retry without `--agent`) | 409 |
| Missing spec on retry | 400 |
| Unknown `parent_run_id` on create/retry | 400 |

## Testing

| Layer | Tests |
|-------|-------|
| Unit | `ListRuns` filters (agent, model incl. empty-matches-unset, template, issue, status, since, parent with prefix); `ResolveRunID` exact, prefix, too-short, ambiguous, mixed-case normalized; central use from all command paths (table-driven list of callers) |
| Unit | `ResumeRun` stage reset including `skipped`→`pending`; attempt reset on resume point; clear summary on resume point; **archive** (not delete) of resume-point + downstream logs/artifacts to `.recovery/<ts>/`; upstream artifacts untouched |
| Unit | Resume atomicity: archive-move failure aborts before DB mutation (run stays terminal); DB reset applied in one transaction; re-invoking resume after a simulated crash is safe/idempotent |
| Unit | Template validation: extra DB stage, missing DB stage → resume error with drift contract (names template + missing/extra stages); list `unknown` |
| Unit | Stale-worker validation: removed worker → resume hard error with retry hint; `retry --agent X` re-resolves and succeeds |
| Unit | `runOpts` restore in `EnsurePipeline`; effective `agent` persisted when `--agent` omitted |
| Unit | Current-stage derivation: running, blocked, failed, rejected, cancel-during-blocked, **cancel-mid-implement (running→cancelled, downstream skipped) — list STAGE and resume point must agree**, succeeded, template missing → `unknown`; template loads memoized once per repeated `template_id` in a list call |
| Unit | JSON serialization: `wall_duration` field name + integer seconds; PARENT column populated for retried runs, blank otherwise |
| Unit | Retry git preflight (go-git): dirty tree → warning; existing branch → warning; clean tree → no warning; retry still proceeds in all cases |
| Integration | Fail implement → `resume` → succeeds (with attempt reset, no stale implement artifacts; prior logs present under `.recovery/`) |
| Integration | Reject gate → run failed → `resume` → gate waits again (no stale rejection summary) → approve → succeed |
| Integration | Cancel while blocked → `resume` → gate waits again |
| Integration | Resume after exhausted retries (attempt was > retries, resume succeeds after attempt reset) |
| Integration | Retry from failed with agent override; `parent_run_id` set; retry from succeeded without `--force` fails |
| Integration | `retry --force` from succeeded creates new run (document smoke only if git state fragile) |
| Integration | `runs list` shows resolved agent (not `(default)`) for runs started without `--agent`; PARENT prefix shown for retried run |
| Integration | API `POST /v1/runs` with agent/model/issue_url persisted and visible in list; retry response surfaces `warnings[]` on dirty tree |
| Integration | Prefix ID works for `status`, `resume`, `runs list --parent`, API get |
| Regression | Keep `TestFreshEngineApproveAndContinue` passing; extend for persisted agent/model |

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Prefer MIT / Apache-2.0 OSS libraries

