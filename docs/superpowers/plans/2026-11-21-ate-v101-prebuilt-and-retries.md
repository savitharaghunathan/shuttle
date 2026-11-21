# Ate v1.0.1 Prebuilt Install + Resume Retries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pin kind helpers to Agent Substrate v1.0.1 with GHCR prebuilts (ate-system + WorkerPool worker image), and retry transient resume/parking failures in the Shuttle ate client.

**Architecture:** Shared retry helpers in `internal/ate` (backoff + HTTP 503/`still resuming` + gRPC `RESOURCE_EXHAUSTED`/`ABORTED`). `TaskClient.RunTask` and `AteAgent` resume path call them. Kind scripts pin `ATE_REF=v1.0.1`, pass `--image-repo`/`--image-tag` to `ate-setup`, and resolve a digest-pinned GHCR worker image instead of `ko build`.

**Tech Stack:** Go 1.26, gRPC status codes, `net/http`/`httptest`, bash kind helpers, GHCR (`ghcr.io/agent-substrate/substrate`).

**Spec:** [`docs/superpowers/specs/2026-11-21-ate-v101-prebuilt-and-retries-design.md`](../specs/2026-11-21-ate-v101-prebuilt-and-retries-design.md)

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Prefer MIT / Apache-2.0 OSS libraries; do not add GPL/proprietary deps
- Prefer go-git over git CLI (unchanged by this plan)
- Do not regenerate `internal/ate/ateapi` unless compile breaks force it
- Do not retry gRPC `Unavailable` or arbitrary HTTP 503 without `still resuming`
- Commits: ask the user for attribution trailers before committing; do **not** add `Co-authored-by: Cursor` / `Assisted-by: Cursor`
- Kind smoke is optional and not part of default `make verify`

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/ate/retry.go` | Shared backoff, `IsResumeParkingHTTP`, `IsRetryableResume`, `Retry` loop helper |
| `internal/ate/retry_test.go` | Matcher + backoff unit tests |
| `internal/ate/task.go` | `RunTask` retry loop, request rebuild, log hygiene |
| `internal/ate/task_test.go` | 503 resume / non-resume / 502 / attempt bound / body replay |
| `internal/ate/fake.go` | `ResumeErrs []error` sequenced resume errors |
| `internal/ate/control.go` | Optional thin `ResumeActorWithRetry` or keep retry in worker |
| `internal/worker/ate.go` | Call resume-with-retry |
| `internal/worker/ate_test.go` | RESOURCE_EXHAUSTED / ABORTED then success |
| `deploy/ate/kind/lib.sh` | `ATE_REF=v1.0.1`, image repo/tag/worker overrides |
| `deploy/ate/kind/install-ate.sh` | Pass `--image-repo` / `--image-tag` |
| `deploy/ate/kind/apply-workerpool.sh` | Resolve GHCR worker digest; no default `ko build` |
| `deploy/ate/kind/README.md` | Document pins, overrides, re-up for existing clusters |
| `deploy/ate/README.md` | Align version / prebuilt notes if present |

---

### Task 1: Retry helpers + matchers

**Files:**
- Create: `internal/ate/retry.go`
- Create: `internal/ate/retry_test.go`

**Interfaces:**
- Produces:
  - `func IsResumeParkingHTTP(statusCode int, body []byte) bool`
  - `func IsRetryableResume(err error) bool` — true for gRPC `ResourceExhausted` or `Aborted`
  - `type Backoff struct{ Initial, Max time.Duration; Factor float64 }` with `func (b Backoff) Wait(ctx context.Context, attempt int) error`
  - `const DefaultResumeAttempts = 30`
  - `func DefaultResumeBackoff() Backoff` — Initial 200ms, Max 2s, Factor 2

- [ ] **Step 1: Write the failing tests**

```go
package ate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsResumeParkingHTTP(t *testing.T) {
	if !ate.IsResumeParkingHTTP(503, []byte("actor is still resuming, retry")) {
		t.Fatal("expected true for still resuming")
	}
	if ate.IsResumeParkingHTTP(503, []byte("credential provider slow, retry")) {
		t.Fatal("must not match bare retry")
	}
	if ate.IsResumeParkingHTTP(502, []byte("still resuming")) {
		t.Fatal("must require 503")
	}
	if ate.IsResumeParkingHTTP(503, []byte("STILL RESUMING please wait")) {
		t.Fatal("case-insensitive: expected true") // fix expectation: should be true
	}
}

func TestIsResumeParkingHTTPCaseInsensitive(t *testing.T) {
	if !ate.IsResumeParkingHTTP(503, []byte("Actor Is Still Resuming")) {
		t.Fatal("expected case-insensitive match")
	}
}

func TestIsRetryableResume(t *testing.T) {
	if !ate.IsRetryableResume(status.Error(codes.ResourceExhausted, "no room")) {
		t.Fatal("RESOURCE_EXHAUSTED")
	}
	if !ate.IsRetryableResume(status.Error(codes.Aborted, "in flight")) {
		t.Fatal("ABORTED")
	}
	if ate.IsRetryableResume(status.Error(codes.Unavailable, "empty")) {
		t.Fatal("must not retry Unavailable")
	}
	if ate.IsRetryableResume(errors.New("plain")) {
		t.Fatal("must not retry plain errors")
	}
}

func TestBackoffWaitRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := ate.Backoff{Initial: time.Second, Max: time.Second, Factor: 2}
	if err := b.Wait(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
```

Fix `TestIsResumeParkingHTTP` so the case-insensitive case is its own test (as above) — do not leave the contradictory comment in the first test.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ate/ -run 'TestIsResumeParkingHTTP|TestIsRetryableResume|TestBackoffWaitRespectsCancel' -count=1`

Expected: FAIL (undefined symbols / package compile error)

- [ ] **Step 3: Implement `retry.go`**

```go
package ate

import (
	"context"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DefaultResumeAttempts = 30

type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64
}

func DefaultResumeBackoff() Backoff {
	return Backoff{Initial: 200 * time.Millisecond, Max: 2 * time.Second, Factor: 2}
}

func IsResumeParkingHTTP(statusCode int, body []byte) bool {
	if statusCode != 503 {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "still resuming")
}

func IsRetryableResume(err error) bool {
	switch status.Code(err) {
	case codes.ResourceExhausted, codes.Aborted:
		return true
	default:
		return false
	}
}

func (b Backoff) Wait(ctx context.Context, attempt int) error {
	if attempt < 0 {
		attempt = 0
	}
	d := b.Initial
	if b.Factor > 1 && attempt > 0 {
		mult := math.Pow(b.Factor, float64(attempt))
		d = time.Duration(float64(b.Initial) * mult)
	}
	if b.Max > 0 && d > b.Max {
		d = b.Max
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
```

(Optional small jitter later; not required for green tests.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ate/ -run 'TestIsResumeParkingHTTP|TestIsRetryableResume|TestBackoffWaitRespectsCancel' -count=1`

Expected: PASS

- [ ] **Step 5: Commit** (only if user requested commits; ask attribution first)

```bash
git add internal/ate/retry.go internal/ate/retry_test.go
git commit -m "$(cat <<'EOF'
Add ate resume parking matchers and backoff helpers.

EOF
)"
```

---

### Task 2: `RunTask` HTTP retries + log hygiene

**Files:**
- Modify: `internal/ate/task.go`
- Modify: `internal/ate/task_test.go`

**Interfaces:**
- Consumes: `IsResumeParkingHTTP`, `DefaultResumeBackoff`, `DefaultResumeAttempts`
- Produces: updated `TaskClient.RunTask` with retries

- [ ] **Step 1: Write the failing tests** (append to `task_test.go`)

```go
func TestRunTaskRetriesResumeParking503(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"prompt":"fix"`) {
			t.Fatalf("missing prompt on attempt %d: %s", n, body)
		}
		if n == 1 {
			http.Error(w, "actor is still resuming, retry", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(ate.TaskResponse{Status: "succeeded", Summary: "done"})
	}))
	defer srv.Close()

	client := &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()}
	var log strings.Builder
	resp, err := client.RunTask(context.Background(), "acme-repo-claude", ate.TaskRequest{
		Prompt: "fix", Branch: "b", RepoURL: "https://github.com/acme/repo.git",
	}, &log)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "succeeded" {
		t.Fatalf("status=%q", resp.Status)
	}
	if n != 2 {
		t.Fatalf("attempts=%d", n)
	}
	if !strings.Contains(log.String(), "ate: actor still resuming, retry") {
		t.Fatalf("expected retry notice in log: %q", log.String())
	}
	if strings.Contains(log.String(), "actor is still resuming, retry") {
		t.Fatalf("raw 503 body leaked into log: %q", log.String())
	}
}

func TestRunTaskDoesNotRetryOther503(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "credential provider slow, retry", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client := &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()}
	_, err := client.RunTask(context.Background(), "a", ate.TaskRequest{Prompt: "p", Branch: "b", RepoURL: "u"}, nil)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err=%v", err)
	}
	if n != 1 {
		t.Fatalf("attempts=%d want 1", n)
	}
}
```

Add `io` to imports in `task_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ate/ -run 'TestRunTaskRetriesResumeParking503|TestRunTaskDoesNotRetryOther503' -count=1`

Expected: FAIL (no retry / raw body still logged)

- [ ] **Step 3: Implement retry loop in `RunTask`**

Replace the single-shot Do/Read path with a loop:

```go
func (c *TaskClient) RunTask(ctx context.Context, actor string, req TaskRequest, log io.Writer) (TaskResponse, error) {
	var out TaskResponse
	if strings.TrimSpace(c.BaseURL) == "" {
		return out, fmt.Errorf("ate task_url is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/tasks"
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	backoff := DefaultResumeBackoff()
	var data []byte
	var statusCode int
	for attempt := 0; attempt < DefaultResumeAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return out, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("ate-target-actor", TargetHeader(c.Atespace, actor))

		resp, err := client.Do(httpReq)
		if err != nil {
			return out, fmt.Errorf("post task: %w", err)
		}
		data, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("read task response: %w", err)
		}
		statusCode = resp.StatusCode
		if IsResumeParkingHTTP(statusCode, data) {
			if log != nil {
				_, _ = fmt.Fprintf(log, "ate: actor still resuming, retry %d\n", attempt+1)
			}
			if attempt+1 >= DefaultResumeAttempts {
				break
			}
			if err := backoff.Wait(ctx, attempt); err != nil {
				return out, err
			}
			continue
		}
		break
	}
	if log != nil && len(data) > 0 {
		_, _ = log.Write(data)
		if data[len(data)-1] != '\n' {
			_, _ = io.WriteString(log, "\n")
		}
	}
	if statusCode < 200 || statusCode >= 300 {
		return out, fmt.Errorf("task HTTP %d: %s", statusCode, truncate(string(data), 512))
	}
	// … existing JSON decode / status checks unchanged …
}
```

Keep the existing success-path decode logic after the loop.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ate/ -count=1`

Expected: PASS (including existing `TestRunTaskSuccess` / `TestRunTaskHTTPError`)

- [ ] **Step 5: Commit** (if requested)

```bash
git add internal/ate/task.go internal/ate/task_test.go
git commit -m "$(cat <<'EOF'
Retry ate task HTTP 503 while actor is still resuming.

EOF
)"
```

---

### Task 3: FakeControl sequenced errors + AteAgent resume retries

**Files:**
- Modify: `internal/ate/fake.go`
- Modify: `internal/worker/ate.go`
- Modify: `internal/worker/ate_test.go`
- Optionally add: `internal/ate/resume.go` with `ResumeActorWithRetry` if you prefer keeping the loop out of the worker

**Interfaces:**
- Consumes: `IsRetryableResume`, `DefaultResumeBackoff`, `DefaultResumeAttempts`
- Produces: `FakeControl.ResumeErrs []error` (consumed then cleared per call); worker retries resume

- [ ] **Step 1: Extend `FakeControl`**

```go
// ResumeErrs, if non-empty, are returned in order from ResumeActor before
// falling back to ResumeErr / success.
ResumeErrs []error
```

In `ResumeActor`, after `f.Resumes++`:

```go
if len(f.ResumeErrs) > 0 {
	err := f.ResumeErrs[0]
	f.ResumeErrs = f.ResumeErrs[1:]
	if err != nil {
		return nil, err
	}
	// nil entry means success for this attempt; fall through to mark running
} else if f.ResumeErr != nil {
	return nil, f.ResumeErr
}
```

- [ ] **Step 2: Write failing worker tests**

```go
func TestAteAgentRetriesResourceExhaustedResume(t *testing.T) {
	fake := ate.NewFakeControl()
	fake.ResumeErrs = []error{
		status.Error(codes.ResourceExhausted, "no room"),
		nil,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ate.TaskResponse{Status: "succeeded", Summary: "ok"})
	}))
	defer srv.Close()
	agent, err := worker.NewAteAgent(worker.AteAgentOptions{
		Config: ate.Config{
			Endpoint: "localhost:1", Atespace: "shuttle",
			ActorTemplate: "shuttle-claude", TaskURL: srv.URL,
		},
		Control: fake, Owner: "acme", RepoName: "widget",
		Tasks:      &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()},
		SyncBranch: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Execute(context.Background(), worker.Request{
		Agent: "claude", Prompt: "x", Branch: "shuttle/issue-1",
		RepoURL: "https://github.com/acme/widget.git", WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.Resumes != 2 {
		t.Fatalf("resumes=%d", fake.Resumes)
	}
}

func TestAteAgentRetriesAbortedResume(t *testing.T) {
	// same as above with codes.Aborted
}
```

Add imports: `google.golang.org/grpc/codes`, `google.golang.org/grpc/status`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/worker/ -run 'TestAteAgentRetries' -count=1`

Expected: FAIL (`resumes=1` or immediate error)

- [ ] **Step 4: Implement resume retry in `AteAgent.Execute`**

Replace the single `ResumeActor` call with:

```go
backoff := ate.DefaultResumeBackoff()
var resumeErr error
for attempt := 0; attempt < ate.DefaultResumeAttempts; attempt++ {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusFailed}, err
	}
	_, resumeErr = w.control.ResumeActor(ctx, w.cfg.Atespace, actorName)
	if resumeErr == nil {
		break
	}
	if !ate.IsRetryableResume(resumeErr) {
		break
	}
	if attempt+1 >= ate.DefaultResumeAttempts {
		break
	}
	if err := backoff.Wait(ctx, attempt); err != nil {
		return Result{Status: StatusFailed}, err
	}
}
if resumeErr != nil {
	return Result{Status: StatusFailed}, fmt.Errorf("resume actor: %w", resumeErr)
}
```

Prefer extracting `ate.ResumeActorWithRetry(ctx, ctrl, atespace, name)` if the worker file gets noisy — same behavior.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ate/ ./internal/worker/ -count=1`

Expected: PASS

- [ ] **Step 6: Commit** (if requested)

```bash
git add internal/ate/fake.go internal/worker/ate.go internal/worker/ate_test.go
git commit -m "$(cat <<'EOF'
Retry ate ResumeActor on RESOURCE_EXHAUSTED and ABORTED.

EOF
)"
```

---

### Task 4: Kind helpers — v1.0.1 + prebuilt ate-system + GHCR worker

**Files:**
- Modify: `deploy/ate/kind/lib.sh`
- Modify: `deploy/ate/kind/install-ate.sh`
- Modify: `deploy/ate/kind/apply-workerpool.sh`
- Modify: `deploy/ate/kind/README.md`
- Modify: `deploy/ate/README.md` (only if it states v1.0.0 or source-build install)

**Interfaces:**
- Produces env defaults: `ATE_REF=v1.0.1`, `ATE_IMAGE_REPO`, `ATE_IMAGE_TAG`, optional `ATE_WORKER_IMAGE`

- [ ] **Step 1: Update `lib.sh` defaults**

```bash
export ATE_REF="${ATE_REF:-v1.0.1}"
export ATE_IMAGE_REPO="${ATE_IMAGE_REPO:-ghcr.io/agent-substrate/substrate}"
export ATE_IMAGE_TAG="${ATE_IMAGE_TAG:-${ATE_REF}}"
# Optional override for WorkerPool.spec.workerImage (digest-pinned ref).
export ATE_WORKER_IMAGE="${ATE_WORKER_IMAGE:-}"
export ATE_WORKER_IMAGE_NAME="${ATE_WORKER_IMAGE_NAME:-ateworker-gvisor}"
```

- [ ] **Step 2: Update `install-ate.sh`**

Add flags to the existing `go run ./cmd/ate-setup deploy ate-system \` invocation:

```bash
  --image-repo "${ATE_IMAGE_REPO}" \
  --image-tag "${ATE_IMAGE_TAG}" \
```

Keep `--sandbox-classes=gvisor` and existing credential / SPIFFE flags.

- [ ] **Step 3: Rewrite `apply-workerpool.sh` to resolve GHCR digest**

Replace the `ko build` block with:

1. If `ATE_WORKER_IMAGE` is set, use it (must contain `@sha256:`).
2. Else resolve:
   - `ref="${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}:${ATE_IMAGE_TAG}"`
   - Prefer `crane digest "$ref"` if `crane` is on PATH (MIT).
   - Else `podman/docker buildx imagetools inspect` / `skopeo inspect` — pick one path that works with the kind provider already required (`podman` default):

```bash
resolve_worker_image() {
  if [[ -n "${ATE_WORKER_IMAGE}" ]]; then
    echo "${ATE_WORKER_IMAGE}"
    return
  fi
  local ref="${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}:${ATE_IMAGE_TAG}"
  local digest=""
  if command -v crane >/dev/null 2>&1; then
    digest="$(crane digest "${ref}")"
  else
    # podman inspect of a pulled image
    container pull "${ref}"
    digest="$(container image inspect "${ref}" --format '{{.Digest}}')"
  fi
  [[ "${digest}" == sha256:* ]] || { echo "failed to resolve digest for ${ref}" >&2; exit 1; }
  echo "${ATE_IMAGE_REPO}/${ATE_WORKER_IMAGE_NAME}@${digest}"
}
```

If the published name is not `ateworker-gvisor`, set `ATE_WORKER_IMAGE_NAME` after checking the v1.0.1 GHCR package list / release notes (implementation-time verification). Document the chosen name in README.

Keep the existing sed into `.generated-workerpool.yaml` and `kubectl apply` / rollout wait.

Remove the `ko install` requirement from the default path.

- [ ] **Step 4: Update kind README**

Document:

| Variable | Default | Meaning |
|---|---|---|
| `ATE_REF` | `v1.0.1` | substrate tag |
| `ATE_IMAGE_REPO` | `ghcr.io/agent-substrate/substrate` | prebuilt repo |
| `ATE_IMAGE_TAG` | `$ATE_REF` | prebuilt tag |
| `ATE_WORKER_IMAGE` | _(resolved)_ | optional full digest-pinned worker override |
| `ATE_WORKER_IMAGE_NAME` | `ateworker-gvisor` | image name under the repo |

Add: existing clusters need `make ate-kind-down` then `make ate-kind-up` to pick up the new pin. First bring-up still pulls from GHCR; worker is no longer built with `ko` by default.

- [ ] **Step 5: Sanity-check scripts**

Run: `bash -n deploy/ate/kind/lib.sh deploy/ate/kind/install-ate.sh deploy/ate/kind/apply-workerpool.sh`

Expected: no output, exit 0

Optional (manual, not CI): `make ate-kind-up` on a machine with podman — confirm install log shows `images: pre-built from ghcr.io/...` and WorkerPool image is `@sha256:…`.

- [ ] **Step 6: Commit** (if requested)

```bash
git add deploy/ate/kind/lib.sh deploy/ate/kind/install-ate.sh deploy/ate/kind/apply-workerpool.sh deploy/ate/kind/README.md deploy/ate/README.md
git commit -m "$(cat <<'EOF'
Pin kind ate helpers to v1.0.1 GHCR prebuilts.

EOF
)"
```

---

### Task 5: Full unit verify + spec cross-check

**Files:** none new

- [ ] **Step 1: Run unit tests**

Run (unsandboxed if sandbox breaks network DNS for modules):  
`go test ./internal/ate/ ./internal/worker/ -count=1`

Expected: PASS

- [ ] **Step 2: Spec coverage checklist**

Confirm each item is done:

- [ ] `ATE_REF` default `v1.0.1`
- [ ] `install-ate.sh` passes `--image-repo` / `--image-tag`
- [ ] Default WorkerPool path has no `ko build`
- [ ] HTTP retry requires 503 + `still resuming`
- [ ] Retryable resume: `RESOURCE_EXHAUSTED` + `ABORTED` only
- [ ] No raw 503 bodies in stage log on parking retries
- [ ] Request body rebuilt each HTTP attempt
- [ ] Docs mention re-up for existing clusters

- [ ] **Step 3: Final commit of any remaining doc/spec edits** (if requested)

```bash
git add docs/superpowers/specs/2026-11-21-ate-v101-prebuilt-and-retries-design.md docs/superpowers/plans/2026-11-21-ate-v101-prebuilt-and-retries.md
git commit -m "$(cat <<'EOF'
Document ate v1.0.1 prebuilt install and resume retries.

EOF
)"
```

---

## Plan self-review

1. **Spec coverage:** A (pin + install flags + A′ worker GHCR), B (HTTP + gRPC retries, log hygiene, FakeControl, tests), docs/re-up — all have tasks. Out-of-scope items have no tasks.
2. **Placeholders:** Worker image name is `ateworker-gvisor` with an explicit verify-at-implement step (not a vague TBD).
3. **Types:** Matchers and backoff names are consistent across Tasks 1–3.
