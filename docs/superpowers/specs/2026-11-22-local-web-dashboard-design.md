# Local Web Dashboard — Design

**Status:** Approved for planning (2026-11-22, rev 4 — dogfood deferred; dashboard re-ranked first). Prerequisite: shipped API/CLI run parity and runs list/recovery APIs. SDLC dogfood is deferred and does not block this work.

## Problem

`shuttle serve` exposes a full localhost JSON API for runs (list, get, create, resume, retry, cancel, approve, reject) but there is no browser UI. Operators must use CLI or raw HTTP. Stage logs are only referenced as `log_path` on run JSON — a browser cannot read those files.

## Goal

Ship a **local web dashboard** on `shuttle serve`:

- List runs and drill into status / stage summaries / logs
- Act: resume, retry, cancel, approve, reject
- Create a new run (same policy as CLI / `POST /v1/runs`)
- Stay on the existing trust boundary (unauthenticated `127.0.0.1`)

## Decisions

| Topic | Choice |
|---|---|
| UI stack | Vite + React + TypeScript in `web/` |
| Delivery | Production build embedded (`go:embed`) and served by the same process as the API |
| Node | **Required** for `make build` and `make verify` (fail clearly if missing) |
| API | Existing `/v1/*` JSON; UI does not reimplement create-run policy |
| Create UX | **Issue URL primary**; absolute `spec_path` as advanced/secondary field (no upload/paste-body API in v1) |
| Mutations | Create / resume / retry / approve / reject keep synchronous `RunUntilBlockOrDone`; UI uses long timeouts + loading + anti double-submit. **Cancel** is a fast local cancel — normal timeouts OK |
| Live updates | Poll **active run detail** ~2s while non-terminal; list slower or on focus/manual; stop when terminal; refetch after actions |
| Streaming | No SSE/WebSocket in v1 |
| Logs | New HTTP endpoint returns log text (see below) |
| Detail JSON | Add `created_at`, `updated_at`, and `current_stage` to `GET /v1/runs/{id}` (required parity with list) |
| Artifacts | Stage `summary` only in v1 — no artifact file browser |
| Auth | None (localhost-only, unchanged) |
| Licenses | MIT / Apache-2.0 frontend deps only |
| Frontend unit tests | Vitest for API client + action enablement helpers **required** in verify |

## Architecture

```
Browser (React SPA)
  ├─ poll GET /v1/runs (slow/on focus), GET /v1/runs/{id} (active ~2s)
  ├─ GET  /v1/runs/{id}/stages/{stage_id}/log?tail=N
  ├─ POST /v1/runs (+ resume/retry/approve/reject)  [long-lived HTTP]
  ├─ POST /v1/runs/{id}/cancel  [fast]
  └─ GET  /v1/pipelines
         │
shuttle serve (chi)
  ├─ /v1/*  → existing api.Server (+ log handler + detail timestamp fields)
  └─ /*     → embedded SPA (assets by path; non-file routes → index.html)
```

Dev: Vite proxies `/v1` to `shuttle serve` (dev-only CORS/proxy config as needed). Prod: single binary, `http://127.0.0.1:8080/`.

## API changes

### `GET /v1/runs/{id}/stages/{stage_id}/log`

- Resolve run id (prefix OK, same as other routes).
- Locate the stage’s `log_path` from store; if unset → 404; if file missing → 404.
- Resolve the path (absolute or relative) and allow read **only** if the cleaned path is inside that run’s `run_dir` (reject escape).
- Optional `tail` (lines); `0` or omitted = full file subject to a **hard max byte cap** (plan picks the number; return truncated body with a header such as `X-Shuttle-Log-Truncated: true` when capped).
- Response: `text/plain; charset=utf-8`.

### Detail parity on `GET /v1/runs/{id}`

Add to the existing run JSON:

- `created_at`, `updated_at` (RFC3339, same as list)
- `current_stage` (same helper as list)

No other new run-policy endpoints in v1.

## Synchronous mutations (sharp edge)

`POST /v1/runs`, resume, retry, and approve/reject all run the engine until block or done **before** responding. The UI must:

- Use a long client timeout (minutes, not seconds)
- Show an in-progress state and disable the triggering control (no double-submit)
- Treat network timeout as indeterminate — refetch run status rather than assuming failure

`POST …/cancel` only signals cancel and updates store status — use a normal short timeout.

Async job IDs are out of scope for v1.

## UI surfaces

1. **Runs list** (`/`) — id, template, status, agent/model, issue, `created_at`/`updated_at`; CTA **New run**.
2. **Run detail** (`/runs/:id`) — header metadata (incl. timestamps); stage timeline (status + summary); per-stage log viewer; action bar:
   - Resume / Retry / Cancel by run status
   - Approve / Reject: target the stage with `waiting_approval` (if exactly one, auto-select; if none/multiple, disable or require explicit stage choice — mirror CLI inference rules)
3. **New run** (`/runs/new`) — template from `GET /v1/pipelines`; **Issue URL** primary; **absolute spec path** behind advanced/secondary disclosure (helper text: path is on the machine running `shuttle serve`); headless; agent; model; remote. Exactly one of issue or spec, matching API. Submit → long `POST /v1/runs` → navigate to detail. Show API `{error}` inline.

Empty and error states are explicit; the client never invents CLI-only rules.

### SPA routing

- Hashed/static assets served by exact path from the embed FS.
- Unknown non-`/v1` paths serve `index.html` so deep links (`/runs/:id`, `/runs/new`) work on refresh.
- `/v1/*` is registered before the SPA fallback and is never shadowed.

## Build / embed

- Source: `web/` (Vite app). Generated assets land in a Go embed package (e.g. `internal/ui`); `web/dist` is build output, gitignored unless the plan says otherwise.
- **`go:embed` cannot embed an empty directory.** The plan must either (a) generate assets before any `go build` that imports the embed package, or (b) keep a committed minimal placeholder in the embed tree so a clean checkout still compiles until `make build` refreshes assets. Prefer (a) with Make ordering; use (b) only if needed for `go test` of non-UI packages without Node in some workflows — but **`make build` / `make verify` still require Node** and a real UI build.
- **`make build` and `make verify` require Node/npm.** If missing or `npm ci` / `npm run build` fails → fail the Make target with an actionable message. No API-only silent binary.
- **CI** (`.github/workflows/ci.yml` is Go-only today) must gain `actions/setup-node` (version from `web/` pin), run the UI build, then Go lint/test/`verify` so embed imports resolve. Wire this in the implementation plan — do not leave CI on pre-UI assumptions.
- Exact embed package path and Make wiring locked in the implementation plan.

## Testing

- Go: log endpoint (happy path, missing stage, missing file, escape rejection, tail, byte cap / truncated header).
- Go: detail JSON includes `created_at` / `updated_at` / `current_stage`.
- Go: `/` and `/runs/x` serve SPA shell; `/v1/runs` still JSON.
- Web: Vitest for API client helpers + action enablement (waiting gate inference) — part of verify.
- Manual: serve → create (issue URL) → poll detail → log view → gate/action against a test repo.

## Out of scope

- Auth / multi-user
- Multi-repo switcher / registry browser
- SSE or WebSocket log/status streaming
- Spec body upload / paste API
- Async create/resume job API
- Retry `force` flag in the UI (API still supports it; CLI remains the escape hatch)
- Editing `shuttle.yaml` from the UI
- Artifact file browser
- Changing create-run policy (already shared)

## Docs / ROADMAP

- README: dashboard URL when serving; localhost-only warning retained; note Node required to build; note create form issue-URL primary and absolute `spec_path` semantics; note long-running POST.
- After merge: move **Local web dashboard** → **Shipped** in `docs/ROADMAP.md`.

## Success criteria

1. `shuttle serve` serves the SPA and `/v1` API from one process; deep links work.
2. From the UI: list, detail, logs, create (issue-first), resume/retry/cancel/approve/reject work against the real API.
3. Active detail polling updates non-terminal runs and stops when terminal.
4. `make build` / `make verify` fail without Node; pass with Node + Go checks including Vitest.
5. ROADMAP Local web dashboard → Shipped.
