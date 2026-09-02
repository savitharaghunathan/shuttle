# Full SDLC Pipeline — Design

**Status:** Approved direction (2026-09-01, rev 6)

## Amendments log

- **rev 6:**  **Strict mode allowlist** — only embedded `sdlc` / `sdlc-headless` (not `strings.HasPrefix`); local `pipelines/sdlc-*` overlays stay bridge mode. **`mergeProcessEnv`** — shell/exec dedupe env with override semantics for `SHUTTLE_*` (fix append bug). **Test log injection** — review receives `{{.ArtifactBodies.test}}` (tail of `logs/test.log`, capped). **Artifact size cap** — 128 KiB per injected body; fail stage with clear error. **Retry stderr hint** for SDLC templates. **`--headless` flag help** narrowed. **Dogfood checklist** before ROADMAP shipped. Expanded tests (empty file, oversized body, retry artifact discard, custom `sdlc-foo` not strict).
- **rev 5:**  **Artifact content injection** — engine loads upstream file contents into `RenderContext.ArtifactBodies` before prompt render (stdout-only presets cannot read paths). **Read-only preamble** aligned with stdout bridge (no “write to path” instructions). **`IsDefaultShipEcho`** matches LoadDefaults **and** legacy scaffold echo argv. **`req.Env` wiring** in engine only (shell worker already merges `Request.Env`; override semantics). **Strict finalize e2e** on embedded `sdlc*` (not only `sdlc-dry`). **finalize failure skips branch compliance** on `implement`. **Retry discards** prior run artifacts; **concurrent same-issue** worse for SDLC. **Review prompt** drops unfulfillable “inspect cwd” — uses injected `implement` body. **Resume persisted workers** test. **Dogfood gate** before ROADMAP shipped.
- **rev 4:** Template-scoped strict finalize; `readonly: true`; ship guard; bridge mode for pr-factory; expanded tests.
- **rev 3:** Stdout fallback bridge, `--agent` on implement only, `RunDir` paths.
- **rev 2:** On-disk artifacts, read-only prompts, scaffold vs LoadDefaults split.
- **rev 1:** Initial design.

## Problem

The embedded `sdlc` template exists and validates, but it is not a proven product path like `pr-factory`:

| Gap | Impact |
|-----|--------|
| Prompts don't inject spec or prior stage content | Stdout-only agents (`claude -p`) can't read `{{.RunDir}}/artifacts/...` paths |
| `outputs: [summary]` declared but not enforced | Downstream stages can't rely on prior deliverables |
| `implement` lacks branch-aware git instructions | Branch compliance may fail |
| `ship` echo stub on existing configs | No production end state for SDLC until migration |
| No `sdlc-headless` | `--headless` only affects `pr-factory` |
| No dry-run or integration test | Regressions undetected |
| CLI doesn't require `--spec` or `--issue` for `-t sdlc` | Empty runs possible |

Run recovery and smarter PR lifecycle already work at the engine level and apply to SDLC without schema changes.

## Goal

Ship the embedded `sdlc` template as a **first-class product path**:

**plan → design → implement → test → review → approve_ship → ship (open PR)**

Success criteria:

1. Spec or issue body is **injected into `plan`**; each stage's upstream artifacts are **injected into prompts**; each agent stage produces a persisted `summary` artifact.
2. `implement` uses deterministic branch naming and post-implement remote compliance (same as `pr-factory`).
3. Newly scaffolded repos get `ship.command` that opens a PR idempotently via `shuttle pr open`.
4. `sdlc-headless` skips `approve_ship`; `--headless -t sdlc` selects it (tested).
5. Integration tests prove gated/headless paths **and** strict finalize on embedded `sdlc` / `sdlc-headless` only.
6. **`pr-factory` / `pr-factory-headless` behavior unchanged** (regression-tested).
7. README documents pr-factory vs sdlc, costs, headless risk, ship migration, retry/resume semantics.

## Non-goals

- Auto-merge (`gh pr merge`) — user overrides `ship.command` if desired.
- Plan or design approval gates in v1 — only `approve_ship` in gated mode.
- Engine gate on review verdict in v1 — review is advisory; human gate in gated mode.
- Semantic validation of artifact content (presence/size caps only in v1).
- Per-stage `--model` overrides (future `readonly_model` config); v1: single `--model` for all agent stages.
- Auto-import on `run --issue` (separate ROADMAP item).
- Engine-level "skip all manual stages" — explicit templates only.
- Behavior keyed on pipeline metadata flags (v1 uses embedded template ID allowlist for strict/ship guard).
- SQLite schema changes.
- Fixing `intake` worker `Outputs` semantics (do not add YAML `outputs:` to `intake`).

## Gating strategy

SDLC v1 gates only at **`approve_ship`**.

**Trade-off:** Wrong plan/design can waste agent time through implement. Operators should use gated mode and **inspect artifacts** under `.shuttle/runs/<id>/artifacts/` before approving.

**Headless risk (README quick-start):** `sdlc-headless` runs `review` then `ship` with no human gate. Review output does **not** block PR open. Use headless only for trusted/automated paths.

**Runtime / cost:**

- Four agent stages × default 30m timeout can exceed 2h wall time and **4× agent invocation cost**.
- **`--model` applies to every agent stage** (plan, design, implement, review).
- **`retry` starts a new run** with frozen `inputs/spec.md` but **does not copy** prior plan/design/implement artifacts — the chain rebuilds from `plan` (4 agent calls again). CLI prints a one-line stderr hint when retrying an SDLC template run.
- **Concurrent runs** on the same `issue_ref` (existing warning) are especially risky for SDLC: multiple 4-stage runs may conflict on `shuttle/issue-N`.
- Bare `run --issue --headless` still selects **`pr-factory-headless`**, not SDLC — SDLC requires `-t sdlc`.

## Architecture

```
StartRun (--spec | --issue)
       │
       ▼
WriteSpec → inputs/spec.md + artifacts/spec
       │
       ▼
plan → design → implement → test → review → [approve_ship] → ship
  │       │         │                              │            │
  │       │         │ finalizeStageOutputs (strict: embedded sdlc only) │
  │       │         │ → on failure: skip branch compliance    │
  │       │         └ post-implement VerifyRemoteBranch        │
  └ loadArtifactBodies → RenderContext.ArtifactBodies ─────────┘
```

### Layers

| Layer | Change |
|-------|--------|
| `internal/pipeline/render.go` | `ArtifactBodies map[string]string` on `RenderContext` |
| `internal/pipeline/artifacts.go` | `loadArtifactBodies(runDir, stageID, pl)` |
| `internal/pipeline/types.go` | `readonly: true` on `Stage` |
| `internal/pipeline/outputs.go` | `ensureOutputDirs`, `finalizeStageOutputs` |
| `internal/pipeline/engine.go` | Load bodies before render; wire `req.Env`; ship guard; readonly `applyDefaults` |
| `internal/worker/env.go` | `mergeProcessEnv(base, overrides)` — dedupe with override |
| `internal/worker/shell.go`, `exec.go` | Use `mergeProcessEnv` instead of append |
| `internal/config/config.go` | `readonly_worker`; `IsNonProductionShipCommand` |
| `internal/templates/sdlc*.yaml` | Injected bodies in prompts; stdout-oriented read-only preamble |
| `internal/repo/scaffold.go` | Production `ship.command` |
| `internal/cli/root.go` | SDLC validation; headless swap |
| `pipelines/sdlc-dry*.yaml` | Shell stubs (bridge mode; not strict) |
| `README.md` | pr-factory vs sdlc, costs, migration |

No changes to `shuttle pr open`, branch resolution, or run recovery logic.

## Agent stages, `--agent`, and `--model`

Built-in presets are **stdout-oriented** (`claude -p`, `cursor-agent --print`). Repo-mutating presets (`cursor` with `--yolo`) must not run read-only stages unless the operator explicitly overrides `worker:` in a local pipeline overlay (document as sharp edge).

### Template stage assignment

| Stage | YAML | Worker resolution | `--agent`? | `--model`? |
|-------|------|-------------------|------------|------------|
| `plan` | `readonly: true` | `readonly_worker` → `claude` | No | Yes |
| `design` | `readonly: true` | same | No | Yes |
| `implement` | *(empty)* | `default_worker` or `--agent` | **Yes** | Yes |
| `review` | `readonly: true` | same | No | Yes |
| `test` | `worker: shell` | shell | No | No |
| `ship` | `worker: shell` | shell | No | No |

### `readonly: true`

`applyDefaults` when `worker` is empty:

1. **`readonly: true`** → `cfg.ReadOnlyWorker` if set, else **`claude`**. **Never** replaced by `--agent`.
2. **Otherwise** → `--agent` if set on `StartRun`, else `cfg.DefaultWorker`.

Explicit `worker:` in YAML is never overridden by `--agent`. Embedded templates use `readonly: true` with **empty** `worker:` (no hardcoded `claude` in YAML).

Worker names are resolved **once at `StartRun`** and persisted on `store.StageRun.Worker`; resume uses persisted workers (no re-resolution).

### Config

```yaml
default_worker: cursor
readonly_worker: claude   # optional; default claude when unset
```

## Artifact content injection

Stdout-only agents cannot reliably open paths in prompts. Before rendering each stage prompt/command, the engine loads upstream artifact **contents** into `RenderContext.ArtifactBodies`.

### `loadArtifactBodies`

```go
const maxArtifactBodyBytes = 128 * 1024 // 128 KiB per injected body

func loadArtifactBodies(runDir, stageID string, pl *Pipeline) (map[string]string, error)
```

Rules:

1. If `artifacts/spec` exists and size > 0 → `bodies["spec"]` (fail if size > `maxArtifactBodyBytes`).
2. For each stage ID in the **transitive `depends_on` closure** of `stageID`, if `artifacts/<dep>/summary` exists and size > 0 → `bodies[dep]` (same size cap).
3. For `plan` (no depends_on), still load `spec` (rule 1).
4. **Review stage only:** if `logs/test.log` exists, load **tail** (last 32 KiB) into `bodies["test"]` so review can assess test output without a repo walk. Cap total `test` body at `maxArtifactBodyBytes`.
5. Files that exist but are **empty** (size 0) are **omitted** (not loaded). Downstream render on `{{.ArtifactBodies.<key>}}` fails via `missingkey=error`.
6. If a loaded body exceeds `maxArtifactBodyBytes` → return error: `artifact <key> exceeds 128 KiB; shorten spec or prior stage output`.

Keys match stage IDs (`spec`, `plan`, `design`, `implement`, `test`) — not file paths.

### Prompt contract

**Read-only preamble** (`plan`, `design`, `review`):

```yaml
Do NOT modify, create, or delete any files in {{.Cwd}}.
Do NOT run git commands.
Respond with your deliverable as markdown in your output; Shuttle records it as the stage artifact.
```

**Implement** may mutate `{{.Cwd}}` and must push `{{.Branch}}`.

| Stage | Injected input | Persisted output |
|-------|----------------|------------------|
| `plan` | `{{.ArtifactBodies.spec}}` | `artifacts/plan/summary` |
| `design` | `{{.ArtifactBodies.plan}}` | `artifacts/design/summary` |
| `implement` | `{{.ArtifactBodies.design}}` + `{{.Branch}}` | `artifacts/implement/summary` |
| `review` | `{{.ArtifactBodies.implement}}`, `{{.ArtifactBodies.test}}` | `artifacts/review/summary` |

Review is **summary-based** in v1: implement summary plus **test log tail** (not full repo inspection). Review output is **advisory** — it does not block `ship` in headless mode; gated mode relies on human `approve_ship` after reading artifacts and logs.

Paths like `{{.RunDir}}/artifacts/...` may appear in prompts only as **documentation** of where artifacts are stored; **injection carries the content**.

**Artifact validation (v1):** non-zero size required to load; **128 KiB max per injected body**; operators must read artifacts before `approve_ship`.

## Engine: declared output files

`Result.Summary` is execution log (stdout/stderr). The engine **stdout bridge** writes it to `artifacts/<stageID>/summary` when the agent did not write the file directly.

### Before dispatch

`ensureOutputDirs(runDir, stageID, outputs)` creates `artifacts/<stageID>/` (`0755`).

### After successful worker execution

`finalizeStageOutputs(runDir, stageID, outputs, result, strict)`:

| Order | Condition | Action |
|-------|-----------|--------|
| 1 | File exists, size > 0 | OK |
| 2 | File missing/empty **and** `TrimSpace(result.Summary) != ""` | Write `Summary` to file |
| 3 | Otherwise | Strict → **fail**; bridge → **succeed** without writing |

**Strict mode:** `isStrictOutputTemplate(templateID)` returns true **only** for embedded template IDs:

```go
func isStrictOutputTemplate(id string) bool {
	switch id {
	case "sdlc", "sdlc-headless":
		return true
	default:
		return false
	}
}
```

Local pipelines (`pipelines/sdlc-experiment.yaml`, `id: sdlc-foo`, etc.) use **bridge mode** unless the operator copies embedded templates verbatim. This avoids surprising strict/ship-guard behavior on experimental overlays.

Dry pipelines `sdlc-dry` / `sdlc-dry-headless` are also bridge mode (not in allowlist).

**Ordering on `implement`:** run `finalizeStageOutputs` first; **only if it succeeds**, run `VerifyRemoteBranch`. Do not emit branch-compliance errors when finalize already failed.

Then `materializeOutputs` only if `result.Outputs` non-empty (unchanged; `intake` path).

### Worker environment (engine + worker fix)

The shell/exec workers today **append** `Request.Env` to `os.Environ()`, which can leave duplicate keys (parent `SHUTTLE_*` shadowing engine values). This feature adds shared merge logic:

```go
// internal/worker/env.go
func mergeProcessEnv(overrides map[string]string) []string
```

1. Start from `os.Environ()` parsed into a map.
2. Apply `overrides` (`SHUTTLE_RUN_DIR`, `SHUTTLE_STAGE_ID`) — **replace** existing keys.
3. Flatten back to `KEY=value` slice for `cmd.Env`.

Engine sets overrides on every stage:

```go
req.Env = shuttleStageEnv(run.RunDir, sid) // SHUTTLE_RUN_DIR, SHUTTLE_STAGE_ID only
```

Update `shell.go` and `exec.go` to call `mergeProcessEnv(req.Env)` instead of append. Dry-run shell stubs may use `SHUTTLE_RUN_DIR` for file writes.

## Ship stage

### Defaults (intentional split)

| Source | `ship.command` when unset |
|--------|---------------------------|
| `config.LoadDefaults` / missing yaml | Echo stub |
| `repo.WriteShuttleYAMLIfAbsent` | `shuttle pr open` via template vars |

### SDLC ship guard (fail loud)

When `isStrictOutputTemplate(run.TemplateID)` (embedded `sdlc`, `sdlc-headless`), **before executing** `ship`:

1. Render `ship` command.
2. If `config.IsNonProductionShipCommand(renderedCmd)` → **fail** `ship` immediately.

`IsNonProductionShipCommand` returns true for **all known echo stubs**:

```go
// LoadDefaults echo:
[]string{"echo", "ship skipped: configure ship.command"}
// Legacy scaffold echo (internal/repo/scaffold.go today):
[]string{"echo", "ship skipped"}
```

Argv equality after render (not stdout substring matching). Custom `echo` ship commands that are not these exact stubs are treated as intentional operator config and are **not** blocked.

For non-SDLC templates, echo ship remains a safe no-op.

## CLI contract

```bash
shuttle run -t sdlc --spec specs/feature.md
shuttle run -t sdlc --issue https://github.com/org/repo/issues/42 --headless
shuttle run -t sdlc-headless --spec specs/feature.md
```

1. `run -t sdlc` without `--spec` or `--issue` → error.
2. `--headless` with `-t sdlc` → `sdlc-headless`.
3. `run --issue --headless` without `-t` → `pr-factory-headless`.
4. Issue body → `WriteSpec` at `StartRun`.
5. `--headless` flag help text: **"skip manual gates for pr-factory/sdlc (selects headless template)"** — not a generic skip-all-manual engine mode.

## Dry-run pipelines

`pipelines/sdlc-dry.yaml` and `pipelines/sdlc-dry-headless.yaml` — shell stubs write artifact files via `SHUTTLE_RUN_DIR` or rendered `{{.RunDir}}`. Template IDs `sdlc-dry*` are **not** strict (bridge mode) so stubs can use stdout-only paths in tests that don't need strict enforcement.

**Strict finalize** must still be covered by integration tests on **embedded** `sdlc` / `sdlc-headless` (see Testing).

## Interaction with existing features

| Feature | SDLC behavior |
|---------|---------------|
| Branch naming | Unchanged |
| Post-implement compliance | `implement` only, after successful finalize |
| **Retry** | New run; frozen spec; **prior artifacts not copied**; chain from `plan`; stderr hint on SDLC retry |
| **Resume** | Upstream artifacts preserved; persisted stage workers unchanged |
| `--agent` | **`implement` only** |
| `--model` | **All agent stages** |
| Concurrent same-issue warning | Existing warning; higher blast radius for SDLC |

## Error cases

| Case | Result |
|------|--------|
| SDLC: missing upstream body at render | Stage failed at render (`missingkey=error`) |
| SDLC: artifact file exists but size 0 | Key omitted → render fails if prompt references it |
| SDLC: artifact body > 128 KiB | Stage failed at body load with size error |
| SDLC: no file, empty stdout after worker | Stage failed (strict finalize) |
| pr-factory implement: empty stdout, worker success | Stage **succeeds** (bridge) |
| Local pipeline `id: sdlc-foo` | Bridge mode (not strict); ship guard off |
| SDLC `ship` with non-production echo command | Stage **failed** (ship guard) |
| `implement` finalize fails | Stage failed; **no** branch compliance check |
| Issue prompt injection | Same trust boundary; **4×** agent exposure; trusted issues only |

## Testing

- **P0:** pr-factory-headless e2e after output finalization wiring.
- **P0:** pr-factory implement empty stdout still succeeds (bridge).
- **P0:** embedded `sdlc-headless` with stub omitting artifact → **plan fails** (strict e2e).
- Engine: `loadArtifactBodies` keys; render includes injected spec/plan content.
- Engine: `loadArtifactBodies` injects test log tail for `review` stage.
- Engine: empty artifact file omitted → render fails on reference.
- Engine: oversized body (>128 KiB) fails with clear error.
- Engine: `finalizeStageOutputs` strict vs bridge; `isStrictOutputTemplate("sdlc-foo")` false.
- Engine: implement finalize fail skips branch check.
- Engine: resume preserves `design/summary` and persisted `plan` worker.
- Worker: `mergeProcessEnv` overrides duplicate `SHUTTLE_*` keys.
- Config: `IsNonProductionShipCommand` true for LoadDefaults **and** scaffold echo.
- Recovery: retry of SDLC run does not copy parent `artifacts/plan/summary` to child run dir.
- Integration: `sdlc-dry` gated + headless (artifact files on disk).
- Integration: `sdlc-headless` ship → `shuttle pr open` (fake `gh`, scaffold ship.command).
- Integration: `sdlc-headless --issue` with stub workers + mock `gh issue view`.
- Embed: readonly stages have empty `worker:`; review prompt references `ArtifactBodies.test`.

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged
- Prefer MIT / Apache-2.0 OSS; go-git + `gh` subprocess
- Do not add `Co-authored-by: Cursor`; ask user for attribution before commit

## Documentation (README)

- pr-factory vs sdlc; artifact paths; injection model (content in prompt, not path-only).
- Headless SDLC: review does not block ship (advisory review + test log in prompt).
- 4× cost; `--model` on all agent stages; retry discards prior artifacts (stderr hint).
- Concurrent same-issue risk for SDLC.
- `--agent` → implement only; `readonly_worker`.
- `ship.command` scaffold vs migration; SDLC fails on known echo stubs (show exact error + fix).
- Issue prompt-injection (4× exposure); trusted issues/repos only.
- Artifact size limit (128 KiB per injected body).
- Sharp edge: `worker: cursor` on readonly stages enables yolo.
- Local `pipelines/sdlc-*` overlays are not strict unless ID is exactly `sdlc` / `sdlc-headless`.

### Dogfood checklist (before ROADMAP shipped)

1. Import or use a real repo with scaffolded `ship.command`.
2. `shuttle run -t sdlc --spec <small-spec.md>` (gated).
3. Inspect `.shuttle/runs/<id>/artifacts/{plan,design,implement}/summary`.
4. `shuttle approve <id> approve_ship`.
5. Confirm `ship` opens PR (or idempotent skip if PR exists).
6. Optionally run `sdlc-headless` on a throwaway spec in a test repo.

Move **Full SDLC pipeline** → **Shipped** when merged **and dogfood checklist complete** (not tests alone).
