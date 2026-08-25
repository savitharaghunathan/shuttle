# Final Fix Report

## Fixes Applied

### 1. Wire `pipeline.Render` into `Engine.Tick`
- Built `RenderContext` (RunID, StageID, Cwd, RunDir, Artifacts, Inputs, Env) before dispatching each stage.
- `Artifacts` and `Inputs` are populated by scanning `runDir/artifacts/` and `runDir/inputs/` respectively.
- `Env` is populated from `os.Environ()`.
- `stageDef.Prompt` and each `stageDef.Command` arg are rendered through `pipeline.Render()`.
- Missing artifact key (template `missingkey=error`) → stage fails immediately with clear message, remaining stages skipped, run marked failed.
- Added `TestRenderPromptWithArtifact`: verifies `{{.Artifacts.spec}}` resolves to real abs path in rendered prompt.
- Added `TestRenderMissingArtifactFailsStage`: verifies missing artifact key fails the stage and run.

### 2. Fix `defer tCancel()` inside Tick dispatch loop
- Replaced `defer tCancel()` with explicit `tCancel()` call after `w.Execute()` returns.
- `tCancel` is declared outside the `if` block; `nil`-checked before calling.
- Prevents context leak across loop iterations.

### 3. Defensive `os.MkdirAll` for approvals dir
- Added `os.MkdirAll(approvalDir, 0o755)` before `os.WriteFile` in `Engine.Approve()`.

## Test Results
- `go test ./internal/pipeline/ -v`: 22/22 PASS
- `go test ./...`: all packages PASS
