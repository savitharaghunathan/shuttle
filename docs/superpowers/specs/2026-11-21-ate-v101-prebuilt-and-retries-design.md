# Ate v1.0.1 Prebuilt Install + Resume Retries — Design

**Status:** Approved for implementation (2026-11-21); amended after design review. Builds on shipped remote workers (`docs/superpowers/specs/2026-11-17-ate-remote-workers-design.md`).

## Problem

Local kind verify is slow because helpers build ate components from source, and shuttle pins Agent Substrate **v1.0.0**. Upstream **v1.0.1** (2026-11-20) publishes GHCR prebuilts and returns a clear `503 actor is still resuming, retry` instead of an empty 504. Shuttle’s task client and resume path do not retry transient multiplex conditions, so remote stages fail under normal pool pressure.

## Goal

1. **A — Pin v1.0.1 and prefer prebuilt images** for ate-system **and** the gVisor worker image used by the kind WorkerPool.
2. **B — Retry transient resume/parking failures** in the Shuttle client so `--remote` matches upstream’s intended multiplex behavior.

Transient retries still honor the stage deadline: after exhaustion (or ctx cancel), the stage fails as today. This softens only immediate fail-on-first-transient, not the parent design’s “resume/task failure fails the stage.”

Non-goals: shared/dev cluster, sync suspend mode, dropping explicit `ResumeActor`, regenerating `ateapi` stubs unless a compile break forces it, changing the actor task protocol, blanket-retry of gRPC `Unavailable`.

## Intended-use note

Shuttle’s runtime shape already matches ate’s harness pattern (sticky actor, explicit resume → workload HTTP with `ate-target-actor` → suspend). Explicit Resume plus the routing header is intentional; HTTP 503 retries mainly cover races where the data plane is not ready yet. This change does not alter that contract.

## A — Version pin + prebuilt install

### Defaults

| Knob | New default |
|---|---|
| `ATE_REF` | `v1.0.1` |
| `ATE_IMAGE_REPO` | `ghcr.io/agent-substrate/substrate` (overridable) |
| `ATE_IMAGE_TAG` | same as `ATE_REF` unless overridden |

### `install-ate.sh`

Pass to `ate-setup deploy ate-system`:

```text
--image-repo ${ATE_IMAGE_REPO}
--image-tag ${ATE_IMAGE_TAG}
--sandbox-classes=gvisor
…existing credential-provider / ingress SPIFFE flags…
```

Still require a checkout of tag `ATE_REF` for manifests + `kubectl-ate` (upstream’s install model). Do **not** change shuttle actor image builds (`build-images.sh`); only ate control-plane / worker images use GHCR.

### WorkerPool (`apply-workerpool.sh`) — A′

Stop `ko build` of `ateworker-gvisor` from source for the default path. Pin `WorkerPool.spec.workerImage` to the release image under `${ATE_IMAGE_REPO}` / `${ATE_IMAGE_TAG}`, **by digest**.

During implementation, confirm the published image name against the v1.0.1 release (expected: `ateworker-gvisor`; older docs may say `ateom-gvisor`). Resolve digest via `crane` / `skopeo` / equivalent MIT/Apache tool already acceptable in-repo, or document a one-liner using the container CLI. Fail clearly if the image cannot be resolved.

Optional escape hatch: `ATE_WORKER_IMAGE` env overrides the resolved ref (advanced / offline).

### Docs

Update `deploy/ate/kind/README.md` (and `deploy/ate/README.md` if it pins v1.0.0):

- Document `ATE_REF` / `ATE_IMAGE_REPO` / `ATE_IMAGE_TAG` / `ATE_WORKER_IMAGE`.
- First bring-up still costs GHCR pulls; rebuilds should avoid source builds for ate-system and the worker image.
- **Existing clusters do not auto-upgrade:** change `ATE_REF` then `make ate-kind-down` + `make ate-kind-up` (or an equivalent reinstall). Note that `ensure-substrate.sh` must successfully fetch/checkout the new tag on a shallow cache.

### Success bar for A

- Kind helpers default to Substrate **v1.0.1**.
- ate-system install passes `--image-repo` / `--image-tag`.
- Default WorkerPool path does **not** `ko build` the worker from source.

## B — Resume-aware retries

### HTTP task client (`internal/ate/task.go`)

`RunTask` retries when **both**:

- Status is **503**, and
- Response body contains `still resuming` (case-insensitive).

Do **not** treat arbitrary 503 bodies that only say `retry` as resume-parking.

Behavior:

- Honor caller `context` deadline; stop when ctx is done. Retries share the stage timeout with the eventual agent task (no separate resume budget in this change).
- Exponential backoff with jitter (start ~200ms, max ~2s).
- Cap attempts (e.g. 30) so a stuck resume cannot spin forever when ctx has no deadline.
- **Rebuild the HTTP request each attempt** (new body reader); do not reuse a drained `bytes.Reader`.
- On a retryable 503: write a one-line notice to the stage log (`ate: actor still resuming, retry N…`). Do **not** append raw 503 bodies as task output.
- On final success or non-retryable failure: write the response body to the log as today.
- Do **not** retry other 4xx/5xx (including 502/504 without the resume signal).

### gRPC resume (`internal/ate` helper used by `AteAgent`)

Retry `ResumeActor` when gRPC code is:

- `RESOURCE_EXHAUSTED` (WorkerPool full), or
- `ABORTED` (another actor op in flight; upstream documents retry).

Same ctx / backoff / attempt caps as HTTP. Other codes (including `Unavailable`, `FailedPrecondition`) fail immediately — no message-based `Unavailable` filter in this change.

`EnsureActor` / `SuspendActor` are unchanged (no new retry loops).

### Tests

- `task_test.go`: 503-with-`still resuming` then 200 succeeds; 503 without that phrase fails; 502 still fails; attempt count bounded; request body replayed on retry.
- Worker / control helper: `RESOURCE_EXHAUSTED` then success; `ABORTED` then success; non-retryable resume error still fails.
- Extend `FakeControl` with sequenced resume errors (e.g. `ResumeErrs []error`) so “fail then succeed” is expressible.

## Out of scope

- Regenerating vendored `internal/ate/ateapi` (revisit only if v1.0.1 stubs are required to compile against a newer API).
- Synchronous suspend durability mode.
- Replacing explicit Resume with router-only auto-resume.
- CI kind smoke as default `make verify`.
- Separate resume-only timeout budget.
- Retrying gRPC `Unavailable`.

## Success criteria

- Kind helpers default to Substrate **v1.0.1**, install ate-system from GHCR, and pin WorkerPool worker image from GHCR by digest (no default `ko build` of the worker).
- Unit tests cover HTTP 503 resume retry (strict body match), log hygiene, body replay, and gRPC `RESOURCE_EXHAUSTED` / `ABORTED` resume retry.
- `go test ./internal/ate/... ./internal/worker/...` passes.
- Existing remote-worker semantics unchanged aside from transient retries under the stage deadline.
- Docs state that existing kind clusters need tear-down / re-up to pick up the new pin.
