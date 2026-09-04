# Task 1 Report: Artifact Content Injection

## Status

Completed Task 1 scope:

- Added `RenderContext.ArtifactBodies`.
- Added `internal/pipeline/artifacts.go` with `upstreamStageIDs` and `loadArtifactBodies`.
- Wired `Engine.Tick` to load artifact bodies before prompt and command rendering.
- Added focused tests for artifact body loading, render access, and engine-time injection.

No later-task scope was implemented: no `finalizeStageOutputs`, template edits, ship guard, or `mergeProcessEnv`.

## TDD Evidence

RED:

```text
go test ./internal/pipeline/ -count=1 -run 'Artifact|RenderIncludes' -v
```

Failed as expected before implementation with missing `loadArtifactBodies`, missing `maxArtifactBodyBytes`, and missing `RenderContext.ArtifactBodies`.

GREEN:

```text
go test ./internal/pipeline/ -count=1 -run 'Artifact|RenderIncludes' -v
```

Passed all focused artifact/render tests.

Additional verification:

```text
go test ./internal/pipeline -count=1
```

Passed outside the sandbox. The sandboxed run failed in existing go-git temp-repo tests due `operation not permitted` under `/private/var`, then passed when rerun with unrestricted permissions.

## Implementation Notes

- `loadArtifactBodies` always attempts to load non-empty `artifacts/spec` as `bodies["spec"]`.
- It loads non-empty `artifacts/<dep>/summary` for every transitive `depends_on` ancestor, keyed by stage ID.
- It returns `artifact <key> exceeds 128 KiB; shorten spec or prior stage output` when an injected whole artifact exceeds the 128 KiB cap.
- Empty files are omitted, preserving template `missingkey=error` behavior.
- `review` loads the last 32 KiB of `logs/test.log` into `bodies["test"]`.
- Engine render now fails the stage and skips remaining stages if artifact body loading fails.

## Tests Added

- `TestLoadArtifactBodies_PlanGetsSpec`
- `TestLoadArtifactBodies_DesignGetsPlanSummary`
- `TestLoadArtifactBodies_ReviewGetsImplementAndTestLog`
- `TestLoadArtifactBodies_EmptyFileOmitted`
- `TestLoadArtifactBodies_OversizedBodyFails`
- `TestRenderIncludesInjectedSpec`
- `TestArtifactBodiesInjectedBeforeStageRender`

## Concerns

None known for Task 1 scope.
