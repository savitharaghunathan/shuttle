# Task 6b Report: CLI Integration Tests

## Status

Implemented Task 6b integration coverage in `internal/cli/integration_test.go`.

## Tests Added

- `TestE2EDrySDLC`
- `TestE2EDrySDLCHeadless`
- `TestE2EHeadlessSwapsSDLCTemplate`
- `TestE2ESDLCStrictEmbeddedFailsWithoutArtifact`
- `TestPRFactoryHeadlessRegression`
- `TestPRFactoryEmptyStdoutRegression`
- `TestE2ESDLCShipOpensPR`
- `TestE2ESDLCIssueSmoke`

## Verification

Passed:

```sh
go test ./internal/cli/ -tags=integration -count=1 -v -run 'SDLC|PRFactory|HeadlessSwaps|IssueSmoke|StrictEmbedded'
```

## Notes

- SDLC embedded tests use stub workers that write declared summary artifacts and non-empty test logs so downstream review prompts can render.
- PR and issue tests use fake `gh`, isolated `SHUTTLE_HOME`, and `SHUTTLE_BIN` wrapper helpers.
