# Task 5 Report: Workers

## Status

DONE

## Summary

Implemented `internal/worker` with:

- `Worker`, `Request`, `Result`, `Status`, and `Registry`
- `shell` worker using direct `exec.CommandContext` argv execution
- `exec` worker for stage-supplied full argv execution
- `intake` worker calling `artifact.CopySpec` and reporting `Outputs["spec"] = "spec"`
- `fake` worker backed by `map[stageID]Result` for engine tests

## Files Changed

- `internal/worker/worker.go`
- `internal/worker/shell.go`
- `internal/worker/exec.go`
- `internal/worker/intake.go`
- `internal/worker/fake.go`
- `internal/worker/worker_test.go`
- `.superpowers/sdd/task-5-report.md`

## TDD Evidence

### RED

Added failing tests first in `internal/worker/worker_test.go`, then ran:

```bash
go test ./internal/worker
```

Observed RED result before production code:

```text
github.com/savitharaghunathan/shuttle/internal/worker: no non-test Go files in /Users/savee/local_dev/shuttle/.worktrees/shuttle-mvp/internal/worker
FAIL	github.com/savitharaghunathan/shuttle/internal/worker [build failed]
FAIL
```

### GREEN

Implemented production code, formatted it, then ran:

```bash
gofmt -w internal/worker && go test ./internal/worker
```

Observed GREEN result:

```text
ok  	github.com/savitharaghunathan/shuttle/internal/worker	0.294s
```

## Final Verification

Full suite:

```bash
go test ./...
```

Result:

```text
?   	github.com/savitharaghunathan/shuttle/cmd/shuttle	[no test files]
ok  	github.com/savitharaghunathan/shuttle/internal/artifact	0.174s
ok  	github.com/savitharaghunathan/shuttle/internal/config	(cached)
ok  	github.com/savitharaghunathan/shuttle/internal/pipeline	0.286s
ok  	github.com/savitharaghunathan/shuttle/internal/store	(cached)
ok  	github.com/savitharaghunathan/shuttle/internal/worker	(cached)
```

IDE lint check for `internal/worker` reported no linter errors.

## Commit

- Included in Task 5 commit with signoff and `Assisted-by: Cursor <cursoragent@cursor.com>` trailer.

## Self-Review

- Confirmed shell and exec use direct argv, not `bash -c` wrapping.
- Confirmed non-zero command exits return a failed result without surfacing an execution error.
- Confirmed timeout cancellation returns a failed result and context error.
- Confirmed intake delegates copying to `artifact.CopySpec` and reports the relative artifact name `spec`.
- Confirmed registry registers only `shell`, `exec`, and `intake`; vendor presets are deferred to Task 7.
- Confirmed fake worker returns configured results by stage ID.

## Concerns

- `shell` falls back to configured preset argv only when the request has no command; future engine wiring should decide when that fallback is appropriate.
- `fake` reports missing stage IDs as execution errors, which should make engine test setup mistakes visible.
