# API / CLI Run Create Parity — Design

**Status:** Draft for review (2026-10-16, rev 4)

## Amendments log

- **rev 4:** Review fixes — served-repo check runs inside the API's `LookupRepo` (before any `gh` call) and compares with `os.SameFile`, so case/symlink differences don't matter; registry opened lazily on first lookup; production registry path gets a test; API tests pin `SHUTTLE_HOME`; smoke script fails cleanly on a non-201; `issue_url` is no longer metadata-only (documented as an API break); every fetch failure, including a nonexistent issue, stays 502; CLI fetch errors gain a `fetch issue:` prefix.
- **rev 3:** Review fixes — API rejects `issue_url` whose imported repo is not the repo `shuttle serve` runs in (prevents runs landing in the wrong DB / work root / `shuttle.yaml`); `smoke-test-app.sh` sends `issue_url` only; typed errors from `runinput` (CLI maps with `errors.Is`/`errors.As`, no string rewriting); issue fetch failure → 502; `headless` on a template with no headless variant is an error on both surfaces; note pre-existing `req.CWD` hole as a non-goal; `outputs_mode` appears on every response built by `writeRun`.
- **rev 2:** Review fixes — issue path ignores `req.CWD`; API `spec_path` must be absolute; drop unused `specPath` from `ResolveInput`; neutral `runinput` errors with CLI wrap; per-request registry open; list reuses template cache for `outputs_mode`; headless no-op on already-headless ids; primary assert `inputs/spec.md`; `OutputsMode` required (not optional); API never passes empty `template_id` into `ResolveTemplate`; unknown template → 400.
- **rev 1:** Initial design. Shared `internal/runinput`, `POST /v1/runs` headless + issue fetch parity, `outputs_mode` on get/list/detail JSON.

## Problem

CLI create-run policy was fixed and unit-tested (`resolveTemplateID`, issue → `SpecContent` via `resolveRunInput`). HTTP `POST /v1/runs` still:

| Gap | Impact |
|-----|--------|
| No headless swap | Clients must know `sdlc-headless` / `pr-factory-headless` by name; CLI `--headless` is not mirrored |
| No spec/issue requirement | Empty SDLC / pr-factory runs can start and fail late |
| `issue_url` only sets `IssueRef` | Parse errors swallowed; issue body never fetched → no `SpecContent` → plan gets empty/missing injection |
| Spec + issue both allowed | CLI rejects; API does not |
| Issue for another repo accepted | Run executes in repo X's checkout but uses the server repo's `shuttle.yaml` (workers, `test.command`, `ship.command`), DB, and work root |
| No `outputs_mode` on API JSON | CLI `status` shows strict/bridge; HTTP list/get do not |

The repo's own `examples/scripts/smoke-test-app.sh` works around the missing issue fetch by running `gh issue view` locally and posting **both** `spec_path` and `issue_url`. A future dashboard or any HTTP client will hit these gaps immediately.

## Goal

1. **One create-run policy** shared by CLI and API (template resolution + input resolution).
2. **`POST /v1/runs` matches CLI rules** for embedded product templates (pr-factory / sdlc and headless variants), including issue body fetch into `SpecContent`.
3. **Runs created over HTTP belong to the served repo** — an issue for a different imported repo is rejected rather than silently mis-filed.
4. **`outputs_mode`** (`strict` \| `bridge` \| omitted) on run JSON — same meaning as CLI `status`.

Success criteria:

1. Unit tests for shared helpers (template matrix moves to `runinput`; `ResolveInput` covered with injected deps).
2. API tests: missing spec/issue → 400; both → 400; bad `issue_url` → 400; relative `spec_path` → 400; issue for a repo imported elsewhere → 400; issue fetch failure → 502; `headless` + `sdlc` → persisted `template_id` is `sdlc-headless`; `headless` + `sdlc-headless` → no-op (201); `headless` + `demo` → 400; issue URL → **`inputs/spec.md` non-empty**.
3. Existing API recovery tests still pass (demo templates without forced inputs).
4. CLI continues to pass unit + integration (`make verify`), with flag-oriented error messages unchanged.
5. `make smoke-test-app` works against a served test app with the updated script.
6. README notes API `headless`, absolute `spec_path`, served-repo rule, and `outputs_mode`.

## Non-goals

- OpenAPI document / codegen
- Async create/resume (still blocking `RunUntilBlockOrDone`)
- New endpoints (`/logs`, `/artifacts`)
- Dashboard UI
- Per-request engine/config for other repos (the real fix for multi-repo serve; v1 rejects instead)
- Closing the pre-existing `req.CWD` hole on the spec-only path (a client can point a run at another checkout while it uses the server's DB/config). Not endorsed; unchanged in v1.
- Engine-level template allowlists beyond existing `IsStrictOutputTemplate`
- Changing CLI empty-`-t` defaulting behavior (stays CLI-only)
- Making API `template_id` optional
- Long-lived registry on the API server (v1 opens/closes per request)
- Schema / SQLite migrations
- Error code enum in JSON (keep `{ "error": "<message>" }`)

## Architecture

```
CLI `shuttle run`          API POST /v1/runs
        │                         │
        │                  LookupRepo = registry + served-repo check
        └───────────┬─────────────┘
                    ▼
           internal/runinput
             ResolveTemplate(...)   // typed errors
             ResolveInput(...)      // typed errors, no specPath arg
                    │
                    ▼
           pipeline.StartRun(SpecContent, IssueRef, ...)
```

| Layer | Change |
|-------|--------|
| `internal/runinput` | New package: `ResolveTemplate`, `ResolveInput`, exported error types (+ tests moved/adapted from CLI) |
| `internal/cli/root.go` | Call `runinput`; map typed errors to `--spec` / `--issue` / `--headless` wording |
| `internal/api/server.go` | `headless` on create body; call `runinput`; absolute `spec_path`; served-repo check; status mapping; wire `SpecContent`; add `outputs_mode` |
| `internal/pipeline` | Exported `OutputsMode(templateID, pl *Pipeline) string` |
| `examples/scripts/smoke-test-app.sh` | Post `issue_url` only; drop local `gh issue view` pre-fetch |
| `README.md` | Document API rules and `outputs_mode` |

No changes to store schema, resume/retry handlers, or `shuttle pr`.

## Shared package: `internal/runinput`

### Errors

`runinput` returns typed errors with neutral messages (no `--flag` names). Callers classify with `errors.Is` / `errors.As`, never by matching text.

```go
var ErrSpecAndIssue = errors.New("use either issue_url or spec_path, not both")

// InputRequiredError: a product template got neither spec nor issue.
type InputRequiredError struct{ Product string } // "pr-factory" | "sdlc"
func (e *InputRequiredError) Error() string {
	return "spec_path or issue_url is required for " + e.Product
}

// HeadlessUnsupportedError: headless requested for a template with no headless variant.
type HeadlessUnsupportedError struct{ TemplateID string }
func (e *HeadlessUnsupportedError) Error() string {
	return "headless is not supported for template " + e.TemplateID + " (no headless variant)"
}

// RepoNotImportedError: issue_url names a repo missing from the home registry.
type RepoNotImportedError struct{ Owner, Name string }
func (e *RepoNotImportedError) Error() string {
	return fmt.Sprintf("repo %s/%s not imported; run: shuttle import repo https://github.com/%s/%s", e.Owner, e.Name, e.Owner, e.Name)
}

// FetchError: fetching the issue body failed (gh missing, auth, network). Not a caller error.
type FetchError struct{ Err error }
func (e *FetchError) Error() string { return "fetch issue: " + e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }
```

Malformed issue URLs return the `issue.ParseIssueURL` error unchanged (caller error).

### `ResolveTemplate`

```go
func ResolveTemplate(templateID string, headless, hasIssue, hasSpec bool) (string, error)
```

1. `hasIssue && hasSpec` → `ErrSpecAndIssue`
2. Empty `templateID` → `pr-factory-headless` if `headless || hasIssue`, else `pr-factory` (**CLI-only path**; see API policy)
3. If `headless`:
   - `pr-factory` → `pr-factory-headless`; `sdlc` → `sdlc-headless`
   - already headless → no-op. "Already headless" means the template's base name, with any `.yaml`/`.yml` extension stripped, ends in `-headless` (so `sdlc-headless`, `pipelines/sdlc-dry-headless.yaml` both qualify)
   - anything else → `*HeadlessUnsupportedError` (e.g. `demo`, `sdlc-dry`, `pipelines/sdlc-dry.yaml`). A silent no-op would leave the client believing gates were skipped while the run blocks at a manual stage.
4. If `!hasIssue && !hasSpec` and id is `pr-factory` / `pr-factory-headless` / `sdlc` / `sdlc-headless` → `*InputRequiredError{Product}`
5. Other ids (overlays, paths, `demo`) → returned as-is without forcing inputs

**API policy:** reject empty `template_id` **before** calling `ResolveTemplate` (`template_id is required`). The API never passes `""`.

**CLI mapping** (preserves existing messages and tests):

| Error | CLI message |
|-------|-------------|
| `ErrSpecAndIssue` | `use either --issue or --spec, not both` |
| `*InputRequiredError{p}` | `--spec is required for <p> (or use --issue)` |
| `*HeadlessUnsupportedError{id}` | `--headless is not supported for template <id> (no headless variant)` |
| others | unchanged |

Rule 3's error is a CLI behavior change (`run -t sdlc-dry --headless` used to silently run gated). Nothing in the repo relies on the old behavior.

### `ResolveInput`

```go
type Input struct {
	CWD         string
	SpecContent string
	IssueRef    string
}

type Options struct {
	FallbackCWD string // used when issueURL is empty (API cwd / CLI Getwd)
	LookupRepo  func(owner, name string) (localPath string, err error)
	FetchSpec   func(ctx context.Context, owner, name string, number int) (string, error) // default: issue.FetchSpec
}

func ResolveInput(ctx context.Context, issueURL string, opts Options) (Input, error)
```

Rules:

1. If `issueURL == ""`: return `{CWD: FallbackCWD or Getwd(), SpecContent: "", IssueRef: ""}`. Spec file path is not handled here — callers pass `SpecPath` to `StartRun`.
2. If `issueURL != ""`:
   - parse URL (fail → parse error);
   - `LookupRepo` required (nil → error);
   - `LookupRepo` returning `registry.ErrNotFound` → `*RepoNotImportedError`;
   - `FetchSpec` (default `issue.FetchSpec`) failure → `*FetchError`;
   - `IssueRef` = `owner/name#N`;
   - `CWD` = imported path. `FallbackCWD` is ignored on the issue path.

### Registry lifecycle

`runinput` does not open `SHUTTLE_HOME`. CLI and API each pass a `LookupRepo` that opens the home registry on its first call (so a malformed URL never touches `SHUTTLE_HOME`) and close it when the command/request finishes. No long-lived registry on `Server` in v1.

CLI fetch failures now read `fetch issue: <gh error>` (previously the bare `gh` error). Accepted; no test depends on the old text.

## API: `POST /v1/runs`

### Request

```go
type createRunRequest struct {
	TemplateID string `json:"template_id"`
	Headless   bool   `json:"headless,omitempty"`
	SpecPath   string `json:"spec_path,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Model      string `json:"model,omitempty"`
	IssueURL   string `json:"issue_url,omitempty"`
}
```

### `spec_path` (HTTP)

`artifact.CopySpec` reads the path relative to the server process cwd, which an HTTP client cannot see. If `spec_path` is set and not `filepath.IsAbs`, return **400** `spec_path must be an absolute path`. CLI keeps accepting relative paths.

### Served-repo rule

The API engine is bound to the repo `shuttle serve` started in: its `shuttle.yaml`, `.shuttle/shuttle.db`, and work root. The CLI, by contrast, reopens the whole app inside the imported repo for issue runs.

So when `issue_url` is set, the API wraps `LookupRepo`: after the registry returns the imported path, compare it with the server's `Options.CWD` **before** `ResolveInput` fetches the issue. A mismatch never calls `gh`, so it is always a 400 and never masked by a 502.

Compare with `os.SameFile` on both `os.Stat` results (handles symlinks, `/var` vs `/private/var`, and macOS case-insensitive paths). If either `Stat` fails, fall back to comparing `filepath.Clean(filepath.Abs(...))`. On mismatch → **400**:

```
issue repo owner/name is imported at <path>; this server serves <server cwd> (run shuttle serve from that repo)
```

If `Options.CWD` is empty (tests or embedding), fall back to `os.Getwd()` for the comparison. Serving from a subdirectory of the imported repo is a mismatch (the engine's config and DB live in the served directory).

### Handler flow

1. Decode body; require non-empty `template_id`.
2. If `spec_path` set and not absolute → 400.
3. `resolved, err := runinput.ResolveTemplate(req.TemplateID, req.Headless, req.IssueURL != "", req.SpecPath != "")` → 400.
4. Load pipeline with **resolved** id → 400 if unknown / load failure.
5. `FallbackCWD` for the spec-only path: `req.CWD` → server `Options.CWD` → `Getwd()`.
6. If `issue_url` set: `LookupRepo` = served-repo check wrapped around `Options.LookupRepo`, or around a lazily opened registry (closed when the request ends).
7. `in, err := runinput.ResolveInput(ctx, req.IssueURL, opts)` → status per table below (served-repo mismatch surfaces here, before the fetch).
8. `StartRun` with `TemplateID: resolved`, `CWD: in.CWD`, `SpecPath: req.SpecPath`, `SpecContent: in.SpecContent`, `IssueRef: in.IssueRef`, agent/model.
9. `RunUntilBlockOrDone` (unchanged, still blocking).
10. Write run JSON (201) including `outputs_mode`.

`api.Options` gains optional injectors (nil in production):

```go
type Options struct {
	CWD         string
	PipelineIDs []string
	LookupRepo  func(owner, name string) (string, error)
	FetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}
```

### Status codes

| Case | Status |
|------|--------|
| Decode / empty `template_id` / relative `spec_path` | 400 |
| `ResolveTemplate` errors (`ErrSpecAndIssue`, `InputRequiredError`, `HeadlessUnsupportedError`) | 400 |
| Malformed `issue_url`, `RepoNotImportedError`, served-repo mismatch | 400 |
| `FetchError` (gh missing, auth, network, **or a nonexistent issue**) | **502** |
| Unknown or unloadable template / StartRun validation | 400 |
| Template loader missing / RunUntilBlockOrDone failure | 500 (unchanged) |
| Success | 201 |

Malformed `issue_url` must be 400, not a silent empty `issue_ref`.

Telling a missing issue apart from other `gh` failures would mean matching `gh` output text; v1 does not. A nonexistent issue is a 502 whose message carries `gh`'s error.

**API break:** `issue_url` is no longer metadata-only. Today `{"template_id":"demo","issue_url":...}` just records `issue_ref`; after this change any `issue_url` requires the repo to be imported at the served directory and a successful fetch, for every template.

## API: `outputs_mode`

Add optional `"outputs_mode"` to list JSON and to every response built by `writeRun` — create, get, resume, cancel, approve, reject. Additive; clients that ignore unknown fields are unaffected.

| Value | When |
|-------|------|
| `"strict"` | Template declares outputs **and** `pipeline.IsStrictOutputTemplate(id)` |
| `"bridge"` | Template declares outputs **and** not strict |
| omitted | Template declares no `outputs:`, or the template no longer loads |

Implement as `pipeline.OutputsMode(templateID string, pl *Pipeline) string`. CLI `outputsMode` becomes a thin wrapper that loads the template then calls it.

**List performance:** `GET /v1/runs` already caches one template load per distinct `template_id` for `current_stage`. Compute `outputs_mode` from that same cached `*Pipeline`.

## Smoke script

`examples/scripts/smoke-test-app.sh` currently fetches the issue body with `gh issue view` into `$ISSUE_SPEC` and posts `spec_path`, `cwd`, and `issue_url` together. After this change that body is a 400 (`ErrSpecAndIssue`).

Update the create step to post `template_id`, `agent`, and `issue_url` only, and remove the local `gh issue view` pre-fetch. The existing `run_dir` work-root assertion stays.

The script must check the create status: on a non-201 it prints the `error` field, the `shuttle import repo .` hint (the likely cause is the test app imported by URL, i.e. cloned under `~/.shuttle/repos/`), and exits non-zero instead of crashing in `json_field id`.

## Testing

- **Unit (`runinput`):** template matrix with `errors.Is`/`errors.As` assertions, including headless unsupported for `demo` / `sdlc-dry` / `pipelines/sdlc-dry.yaml` and no-op for `pipelines/sdlc-dry-headless.yaml`; `ResolveInput` with fake `LookupRepo` + `FetchSpec`, covering not-imported, fetch failure (`*FetchError`), and issue path ignoring `FallbackCWD`.
- **API:** missing spec/issue for `sdlc` → 400; both → 400; relative `spec_path` → 400; `headless` + `sdlc` → `sdlc-headless`; `headless` + `sdlc-headless` → 201 same id; `headless` + `demo` → 400; issue with injected `LookupRepo` returning the server dir + `FetchSpec` → `inputs/spec.md` non-empty and request `cwd` ignored; issue imported at another path → 400 **and `FetchSpec` not called**; `FetchSpec` error → 502; malformed URL → 400; get/list include `outputs_mode`.
- **API, production registry path:** with `SHUTTLE_HOME` set to a temp dir and only `FetchSpec` injected — repo saved in the registry at the served dir → 201; repo not in the registry → 400 `not imported`. All API test envs pin `SHUTTLE_HOME` to a temp dir.
- **Regression:** existing `demo` createRun bodies without spec still succeed.
- **CLI:** existing tests keep passing (flag-oriented messages via typed-error mapping); add `--headless` with a non-swappable template → error.
- **Manual:** `make serve-test-app` + `make smoke-test-app ISSUE_URL=...` with the updated script.

## Documentation

README "serve" / API section:

- `POST /v1/runs` accepts `headless` (same swap as CLI; already-headless ids are a no-op; templates with no headless variant are rejected).
- Product templates `pr-factory` / `sdlc` (and headless variants) require exactly one of `spec_path` or `issue_url`.
- HTTP `spec_path` must be absolute.
- `issue_url` fetches the issue body into the run spec. The issue's repo must be the one `shuttle serve` was started in; the request `cwd` is ignored. Any fetch failure, including a nonexistent issue, is a 502.
- `issue_url` is no longer metadata-only, for any template (API break).
- Run JSON may include `outputs_mode`: `strict` or `bridge`.
- CLI: `--headless` on a template with no headless variant is now an error.

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged (localhost API unauthenticated)
- Prefer MIT / Apache-2.0 OSS
- Commits: `git commit -s` (Signed-off-by) with trailer `Assisted-by: Cursor <cursoragent@cursor.com>`

## Out of scope follow-ups

- OpenAPI capture after this lands
- Non-blocking API for long runs
- `GET /v1/runs/{id}/artifacts` and logs endpoints (dashboard prerequisites)
- Per-repo engine on `shuttle serve` (multi-repo serving) and a long-lived registry
- Closing the `req.CWD` hole on the spec-only path
