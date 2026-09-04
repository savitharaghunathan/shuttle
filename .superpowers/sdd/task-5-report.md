# Task 5 Report: CLI + SDLC Ship Guard

## Status

DONE

## Changes

- SDLC runs now require `--spec` or `--issue` for both `sdlc` and `sdlc-headless`.
- `--headless -t sdlc` now selects `sdlc-headless`.
- `--headless` help text now says: `skip manual gates for pr-factory/sdlc (selects headless template)`.
- Strict-output SDLC templates now fail the `ship` stage before execution when the rendered command is a known non-production ship echo.

## Tests

- Added `TestRunRequiresSpecForSDLC`.
- Added `TestShipGuardFailsOnScaffoldEcho`.
- Added `TestShipGuardFailsOnLoadDefaultsEcho`.

Verification:

```sh
go test ./internal/cli/ ./internal/pipeline/ -count=1 -run 'TestRunRequires|ShipGuard' -v
```

Result: pass.

## Concerns

None.
