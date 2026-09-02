# Full SDLC Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `sdlc` a first-class factory path with artifact **content injection** (128 KiB cap), strict finalize on embedded `sdlc`/`sdlc-headless` only, test-log injection for review, pr-factory-safe bridge mode, `readonly:` stages, ship guard, env dedupe, `sdlc-headless`, integration tests, and docs.

**Architecture:** `loadArtifactBodies` → `RenderContext.ArtifactBodies` before render (incl. test log tail for review). `finalizeStageOutputs(..., strict)` — strict allowlist `sdlc` + `sdlc-headless` only. `mergeProcessEnv` in shell/exec. Read-only stages via `readonly: true` + `readonly_worker`. SDLC ship fails on `IsNonProductionShipCommand`.

**Tech Stack:** Go, text/template, cobra, existing `shuttle pr open` / branch compliance

**Spec:** [`docs/superpowers/specs/2026-09-01-sdlc-pipeline-design.md`](../specs/2026-09-01-sdlc-pipeline-design.md) (rev 6)

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged: Shuttle does not checkout, push, or reset git
- Prefer MIT / Apache-2.0 OSS libraries; go-git for git reads; `gh` subprocess for PR APIs
- ask user for attribution before each commit

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/pipeline/render.go` | `ArtifactBodies map[string]string` on `RenderContext` |
| `internal/pipeline/artifacts.go` | `loadArtifactBodies`, `upstreamStageIDs` |
| `internal/pipeline/artifacts_test.go` | Body loading unit tests |
| `internal/pipeline/types.go` | `ReadOnly bool` on `Stage` |
| `internal/pipeline/outputs.go` | `ensureOutputDirs`, `finalizeStageOutputs`, `isStrictOutputTemplate` |
| `internal/pipeline/outputs_test.go` | Strict vs bridge unit tests |
| `internal/pipeline/engine.go` | Bodies before render; finalize; branch compliance ordering; `shuttleStageEnv`; ship guard; `applyDefaults` |
| `internal/pipeline/engine_test.go` | Finalization, injection, regressions, ship guard, branch ordering |
| `internal/worker/env.go` | `mergeProcessEnv` — dedupe with override |
| `internal/worker/env_test.go` | SHUTTLE_* override semantics |
| `internal/worker/shell.go`, `exec.go` | Use `mergeProcessEnv` |
| `internal/pipeline/recovery.go` | SDLC retry stderr hint |
| `internal/pipeline/recovery_test.go` | Resume artifacts, persisted workers, retry artifact discard |
| `internal/config/config.go` | `ReadOnlyWorker`, `IsNonProductionShipCommand`, `EffectiveReadOnlyWorker()` |
| `internal/config/config_test.go` | Echo stub detection (LoadDefaults + scaffold) |
| `internal/templates/sdlc.yaml` | `readonly: true`, injected `ArtifactBodies`, stdout preamble |
| `internal/templates/sdlc-headless.yaml` | Without `approve_ship` |
| `internal/templates/embed.go` | Embed `sdlc-headless` |
| `internal/templates/embed_test.go` | Readonly, empty worker, bodies in prompts, `IDs()` |
| `internal/repo/scaffold.go` | Production `ship.command` |
| `internal/cli/root.go` | SDLC validation; headless swap |
| `internal/cli/root_test.go` | SDLC requires spec/issue |
| `pipelines/sdlc-dry.yaml` | File-writing shell stubs (bridge mode) |
| `pipelines/sdlc-dry-headless.yaml` | Headless dry variant |
| `internal/cli/integration_test.go` | SDLC e2e including strict embedded failure |
| `README.md` | pr-factory vs sdlc, costs, retry/resume, migration |
| `docs/ROADMAP.md` | Full SDLC → Shipped (Task 7, after dogfood) |

---

### Task 0: Stage `readonly` + `applyDefaults` worker rules

**Files:**
- Modify: `internal/pipeline/types.go`
- Modify: `internal/config/config.go`
- Modify: `internal/pipeline/engine.go` (`applyDefaults`)
- Modify: `internal/pipeline/engine_test.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**

```go
ReadOnlyWorker string `yaml:"readonly_worker,omitempty"`
func (c *Config) EffectiveReadOnlyWorker() string // "claude" when empty

ReadOnly bool `yaml:"readonly,omitempty"` // on pipeline.Stage
```

`applyDefaults` when `worker` empty:
- `ReadOnly == true` → `EffectiveReadOnlyWorker()`, never `--agent`
- else → `--agent` if set, else `DefaultWorker`

- [ ] **Step 1: `TestAgentOverrideSkipsReadOnlyStages`** — plan=claude, implement=cursor when `Agent: cursor`.

- [ ] **Step 2: `TestExplicitWorkerNotOverridden`** — explicit `worker: codex` on plan stays codex.

- [ ] **Step 3: Implement field + config + `applyDefaults`**

- [ ] **Step 4: Run tests — PASS**

Run: `go test ./internal/pipeline/ ./internal/config/ -count=1 -run 'AgentOverride|ReadOnly|ExplicitWorker' -v`

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 1: Artifact content injection

**Files:**
- Modify: `internal/pipeline/render.go`
- Create: `internal/pipeline/artifacts.go`
- Create: `internal/pipeline/artifacts_test.go`
- Modify: `internal/pipeline/engine.go`

**Interfaces:**

```go
func upstreamStageIDs(pl *Pipeline, stageID string) []string
func loadArtifactBodies(runDir, stageID string, pl *Pipeline) (map[string]string, error)
```

- [ ] **Step 1: Add `ArtifactBodies map[string]string` to `RenderContext`**

- [ ] **Step 2: Unit tests in `artifacts_test.go`**

```go
func TestLoadArtifactBodies_PlanGetsSpec(t *testing.T)
func TestLoadArtifactBodies_DesignGetsPlanSummary(t *testing.T)
func TestLoadArtifactBodies_ReviewGetsImplementAndTestLog(t *testing.T)
func TestLoadArtifactBodies_EmptyFileOmitted(t *testing.T)
func TestLoadArtifactBodies_OversizedBodyFails(t *testing.T)
```

Setup temp run dir with `artifacts/spec`, `artifacts/plan/summary`, `logs/test.log`, etc.

- [ ] **Step 3: Implement `artifacts.go`**

- `const maxArtifactBodyBytes = 128 * 1024`
- Load `artifacts/spec` → `bodies["spec"]` when size > 0; error if > cap.
- Walk transitive `depends_on` ancestors; load `artifacts/<dep>/summary` → `bodies[dep]`.
- For `review` stage: tail `logs/test.log` (last 32 KiB) → `bodies["test"]`.

- [ ] **Step 4: Engine tick — before `Render(prompt)`**

```go
bodies, err := loadArtifactBodies(run.RunDir, sid, pl)
rctx.ArtifactBodies = bodies
```

- [ ] **Step 5: `TestRenderIncludesInjectedSpec`** — render SDLC plan prompt template with bodies map; assert spec text present.

- [ ] **Step 6: Run tests — PASS**

Run: `go test ./internal/pipeline/ -count=1 -run 'Artifact|RenderIncludes' -v`

- [ ] **Step 7: Commit** (ask attribution)

---

### Task 2: Engine — output dirs, finalize (strict vs bridge), worker env, branch ordering

**Files:**
- Create: `internal/pipeline/outputs.go`
- Create: `internal/pipeline/outputs_test.go`
- Create: `internal/worker/env.go`
- Create: `internal/worker/env_test.go`
- Modify: `internal/worker/shell.go`
- Modify: `internal/worker/exec.go`
- Modify: `internal/pipeline/engine.go`
- Modify: `internal/pipeline/engine_test.go`

**Interfaces:**

```go
func ensureOutputDirs(runDir, stageID string, outputs []string) error
func finalizeStageOutputs(runDir, stageID string, outputs []string, result *worker.Result, strict bool) error
func isStrictOutputTemplate(templateID string) bool
func shuttleStageEnv(runDir, stageID string) map[string]string
```

`isStrictOutputTemplate` (allowlist only):

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

- [ ] **Step 1: Unit tests in `outputs_test.go`**

```go
func TestFinalizeStageOutputs_StrictFailsWhenBothMissing(t *testing.T)
func TestFinalizeStageOutputs_BridgeSucceedsWhenBothMissing(t *testing.T)
func TestFinalizeStageOutputs_StdoutBridge(t *testing.T)
func TestIsStrictOutputTemplate(t *testing.T)
// sdlc=true, sdlc-headless=true, sdlc-dry=false, sdlc-foo=false, pr-factory=false
```

- [ ] **Step 1b: `env_test.go` — `TestMergeProcessEnvOverridesDuplicateKeys`**

Parent env has `SHUTTLE_RUN_DIR=/wrong`; override map has `/correct` → exec env uses `/correct`.

- [ ] **Step 2: Implement `outputs.go` + `env.go`** (per spec rev 6)

- [ ] **Step 3: Wire engine tick**

- `ensureOutputDirs` before `w.Execute`
- On worker success: `finalizeStageOutputs` with `strict := isStrictOutputTemplate(run.TemplateID)`
- On finalize error → stage failed, **return before** `VerifyRemoteBranch`
- On `implement` success + finalize OK → existing branch compliance

- [ ] **Step 4: `TestImplementFinalizeFailSkipsBranchCompliance`** — strict sdlc, empty stdout, git repo present → failed with artifact error, **not** branch-missing error.

- [ ] **Step 5: P0 regressions**

- `TestPRFactoryImplementEmptyStdoutStillSucceeds` (bridge)
- `TestSDLCMissingArtifactFails` (strict, template `sdlc`)

- [ ] **Step 6: Wire `req.Env` via `shuttleStageEnv` in engine**

- [ ] **Step 7: Update `shell.go` and `exec.go`** to use `mergeProcessEnv(req.Env)` instead of append-to-`os.Environ()`.

- [ ] **Step 8: Run `go test ./internal/pipeline/ ./internal/worker/ -count=1 -v`**

- [ ] **Step 9: Commit** (ask attribution)

---

### Task 3: SDLC templates

**Files:**
- Modify: `internal/templates/sdlc.yaml`
- Create: `internal/templates/sdlc-headless.yaml`
- Modify: `internal/templates/embed.go`
- Modify: `internal/templates/embed_test.go`

- [ ] **Step 1: Replace `sdlc.yaml`**

Example `plan` prompt:

```yaml
- id: plan
  readonly: true
  prompt: |
    Do NOT modify, create, or delete any files in {{.Cwd}}.
    Do NOT run git commands.
    Respond with your deliverable as markdown in your output; Shuttle records it as the stage artifact.

    ## Task spec
    {{.ArtifactBodies.spec}}
  outputs: [summary]
```

`design` uses `{{.ArtifactBodies.plan}}`; `implement` uses `{{.ArtifactBodies.design}}` + branch instructions; `review` uses `{{.ArtifactBodies.implement}}` and `{{.ArtifactBodies.test}}` (test log tail; advisory review).

- [ ] **Step 2: Create `sdlc-headless.yaml`**

- [ ] **Step 3: Update `embed.go`**

- [ ] **Step 4: `embed_test.go` assertions**

- `plan`/`design`/`review`: `ReadOnly: true`, **empty** `Worker` (not `claude`)
- Prompts contain `{{.ArtifactBodies.spec}}` / `.plan` / `.implement` / `.test` (review)
- `implement` contains `{{.Branch}}`
- `TestIDsIncludesSDLCHeadless`

- [ ] **Step 5: Run `go test ./internal/templates/ -count=1 -v`**

- [ ] **Step 6: Commit** (ask attribution)

---

### Task 4: Config ship helpers + scaffold

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/repo/scaffold.go`

- [ ] **Step 1: Add `IsNonProductionShipCommand(cmd []string) bool`**

```go
var nonProductionShipCommands = [][]string{
	{"echo", "ship skipped: configure ship.command"}, // LoadDefaults
	{"echo", "ship skipped"},                         // legacy scaffold
}

func IsNonProductionShipCommand(cmd []string) bool {
	for _, stub := range nonProductionShipCommands {
		if slices.Equal(cmd, stub) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Tests**

```go
func TestIsNonProductionShipCommand_LoadDefaultsEcho(t *testing.T)
func TestIsNonProductionShipCommand_ScaffoldEcho(t *testing.T)
func TestIsNonProductionShipCommand_PROpenFalse(t *testing.T)
func TestLoadDefaultsShipRemainsEcho(t *testing.T)
```

- [ ] **Step 3: Update scaffold `Ship` to pr open** (same as `OpenPR`)

- [ ] **Step 4: Run `go test ./internal/config/ ./internal/repo/ -count=1 -v`**

- [ ] **Step 5: Commit** (ask attribution)

---

### Task 5: CLI + SDLC ship guard

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/pipeline/engine.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/pipeline/engine_test.go`

- [ ] **Step 1: SDLC validation + headless swap** (per spec)

- [ ] **Step 1b: Narrow `--headless` flag help** — "skip manual gates for pr-factory/sdlc (selects headless template)"

- [ ] **Step 2: Ship guard before `ship` worker**

```go
if sid == "ship" && isStrictOutputTemplate(run.TemplateID) && config.IsNonProductionShipCommand(renderedCmd) {
	// fail stage immediately
}
```

- [ ] **Step 3: `TestShipGuardFailsOnScaffoldEcho`** — rendered cmd `echo ship skipped` → ship failed.

- [ ] **Step 4: `TestShipGuardFailsOnLoadDefaultsEcho`**

- [ ] **Step 5: `TestRunRequiresSpecForSDLC`**

- [ ] **Step 6: Run tests — PASS**

Run: `go test ./internal/cli/ ./internal/pipeline/ -count=1 -run 'TestRunRequires|ShipGuard' -v`

- [ ] **Step 7: Commit** (ask attribution)

---

### Task 6a: Dry pipelines + engine/recovery tests

**Files:**
- Create: `pipelines/sdlc-dry.yaml`
- Create: `pipelines/sdlc-dry-headless.yaml`
- Modify: `internal/pipeline/recovery.go`
- Modify: `internal/pipeline/recovery_test.go`

- [ ] **Step 1: `sdlc-dry.yaml`** — shell stubs write files via `$SHUTTLE_RUN_DIR/artifacts/<stage>/summary`; `test` writes a small `logs/test.log`; `ship: ["true"]`

- [ ] **Step 2: `sdlc-dry-headless.yaml`**

- [ ] **Step 3: `TestRetrySDLCDiscardsParentArtifacts`** — parent run has `artifacts/plan/summary`; child retry run dir must not contain it.

- [ ] **Step 4: SDLC retry stderr hint** in `RetryRun` when `parent.TemplateID` is `sdlc` or `sdlc-headless`:

```go
warnings = append(warnings, "retry starts a new run; plan/design artifacts are not copied from the parent run")
```

- [ ] **Step 5: `TestResumeFromImplementPreservesDesignArtifact`**

- [ ] **Step 6: `TestResumeUsesPersistedPlanWorker`**

- [ ] **Step 7: Run `go test ./internal/pipeline/ -count=1 -run 'Retry|Resume' -v`**

- [ ] **Step 8: Commit** (ask attribution)

---

### Task 6b: CLI integration tests

**Files:**
- Modify: `internal/cli/integration_test.go`

- [ ] **Step 1: `TestE2EDrySDLC`** / **`TestE2EDrySDLCHeadless`** — gated approve path + headless success; verify artifact files on disk

- [ ] **Step 2: `TestE2EHeadlessSwapsSDLCTemplate`** — embedded `sdlc` + `--headless` → succeeds with stub workers writing stdout summaries

- [ ] **Step 3: `TestE2ESDLCStrictEmbeddedFailsWithoutArtifact`** — **P0**

Embedded `sdlc-headless`, stub worker returns empty Summary and does not write files → run fails at `plan` (strict finalize).

- [ ] **Step 4: `TestPRFactoryHeadlessRegression`** + **`TestPRFactoryEmptyStdoutRegression`**

- [ ] **Step 5: `TestE2ESDLCShipOpensPR`** — scaffold-style `ship.command`, fake `gh`, `SHUTTLE_BIN` wrapper, stub workers

- [ ] **Step 6: `TestE2ESDLCIssueSmoke`** — mock `gh issue view`; stub workers; `run -t sdlc-headless --issue <url>`

- [ ] **Step 7: Run integration tests**

Run: `go test ./internal/cli/ -tags=integration -count=1 -v -run 'SDLC|PRFactory|HeadlessSwaps|IssueSmoke|StrictEmbedded'`

- [ ] **Step 8: Commit** (ask attribution)

---

### Task 7: README + ROADMAP

**Files:**
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

- [ ] **Step 1: README — pr-factory vs sdlc**

Include rev 6 items:
- Content injection (not path-only prompts); 128 KiB cap per body
- Review includes test log tail (advisory; does not block ship in headless)
- Headless: review does NOT block ship
- 4× cost; `--model` on all agent stages
- **Retry discards prior artifacts**; rebuilds from `plan`; stderr hint
- **Concurrent same-issue** higher risk for SDLC
- `--agent` → implement only; `readonly_worker`
- `run --issue --headless` → pr-factory-headless
- SDLC fails on known echo ship stubs; show exact error + migration snippet
- Issue prompt-injection (4×); trusted issues only
- Local `pipelines/sdlc-*` overlays are bridge mode (not strict)
- yolo sharp edge on readonly overlay
- **Dogfood checklist** (spec section) before ROADMAP shipped

- [ ] **Step 2: List `sdlc-headless` in embedded templates**

- [ ] **Step 3: ROADMAP — Full SDLC → Shipped** (only after dogfood checklist)

- [ ] **Step 4: `go test ./... -count=1`**

- [ ] **Step 5: Commit** (ask attribution)

---

## Execution order

**0 → 1 → 2 → 3 → 4 → 5 → 6a → 6b → 7**

Task 2 depends on Task 0 (readonly unrelated but parallel OK). Task 3 depends on Task 1 (`ArtifactBodies` in prompts). Task 5 depends on Task 4 (`IsNonProductionShipCommand`). Task 6a/6b depend on Tasks 0–5. Task 7 last.

---

## Spec coverage checklist (rev 6)

| Spec requirement | Task |
|------------------|------|
| `ArtifactBodies` content injection | 1, 3 |
| Test log tail for review | 1, 3 |
| 128 KiB artifact body cap | 1 |
| Strict finalize allowlist (`sdlc`, `sdlc-headless`) | 2 |
| `sdlc-dry*` / `sdlc-foo` bridge mode | 2, 6b |
| pr-factory empty stdout regression | 2, 6b |
| finalize fail → skip branch compliance | 2 |
| `mergeProcessEnv` override semantics | 2 |
| `readonly: true` + empty worker in YAML | 0, 3 |
| `IsNonProductionShipCommand` (both echos) | 4, 5 |
| Scaffold ship / LoadDefaults echo split | 4 |
| CLI + headless swap + flag help | 5 |
| Strict embedded e2e failure test | 6b |
| Ship → pr open integration | 6b |
| Issue smoke with stub workers | 6b |
| Retry discards artifacts + stderr hint | 6a, 7 |
| Resume artifacts + persisted workers | 6a |
| README + dogfood checklist | 7 |

---

## Review fixes addressed (rev 6)

| Review finding (round 4) | Resolution |
|--------------------------|------------|
| Review blind to test results | **Test log tail** → `ArtifactBodies.test` for review (Tasks 1, 3) |
| `req.Env` append duplicates keys | **`mergeProcessEnv`** in shell/exec (Task 2) |
| `HasPrefix("sdlc")` too broad | **Allowlist** `sdlc` + `sdlc-headless` only (Task 2) |
| No artifact size limits | **128 KiB cap** per body (Task 1) |
| Retry artifact discard not tested | **`TestRetrySDLCDiscardsParentArtifacts`** (Task 6a) |
| Empty file behavior unclear | **`TestLoadArtifactBodies_EmptyFileOmitted`** (Task 1) |
| Task 6 overloaded | Split **6a** (recovery/dry) + **6b** (integration) |
| `--headless` flag misleading | Narrow flag help (Task 5) |
| Retry cost surprise | **stderr hint** on SDLC retry (Task 6a) |
| Dogfood vague | **Checklist** in spec + Task 7 |
