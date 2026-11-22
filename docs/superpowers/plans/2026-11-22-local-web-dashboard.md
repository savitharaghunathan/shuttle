# Local Web Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Vite/React/TypeScript dashboard embedded in `shuttle serve` for list/detail/logs, run actions, and create-run (issue-URL primary), with Node required for `make build` / `make verify`.

**Architecture:** Chi serves `/v1/*` JSON (existing + log endpoint + detail timestamp parity) and embeds a React SPA from `internal/ui` (`go:embed`). The SPA polls active run detail; mutations keep synchronous `RunUntilBlockOrDone` with long client timeouts (cancel stays fast).

**Tech Stack:** Go 1.26, chi, `embed`, Vite 6, React 19, TypeScript, React Router, Vitest. MIT/Apache-2.0 deps only. Node 22.

**Spec:** [`docs/superpowers/specs/2026-11-22-local-web-dashboard-design.md`](../specs/2026-11-22-local-web-dashboard-design.md) (rev 3)

**Prerequisite:** SDLC dogfood plan complete; Full SDLC marked Shipped on this branch.

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- MIT / Apache-2.0 frontend deps only; no GPL/proprietary
- Node/npm **required** for `make build` and `make verify`
- No SSE/WebSocket; no spec upload API; no retry `force` in UI
- Create UX: issue URL primary; absolute `spec_path` secondary
- Ask user for attribution before commits; no `Co-authored-by: Cursor`
- Log hard max: **1 MiB** (`1 << 20`); header `X-Shuttle-Log-Truncated: true` when capped

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/api/log.go` | Stage log read + path sandbox + tail + byte cap |
| `internal/api/log_test.go` | Log endpoint tests |
| `internal/api/server.go` | Register log route; extend `runResponse` / `writeRun`; mount SPA |
| `internal/api/server_test.go` | Detail parity + SPA/API shadow tests |
| `internal/ui/embed.go` | `go:embed all:dist` + `Handler()` |
| `internal/ui/dist/*` | Built assets (Make copies from `web/dist`; placeholder `index.html` committed) |
| `web/` | Vite + React + TS app source |
| `web/package.json` | Scripts, engines node>=22, MIT deps |
| `web/.nvmrc` | `22` |
| `Makefile` | `ui-build`, require Node before `build`/`verify`, run Vitest |
| `.gitignore` | `web/dist/`, `web/node_modules/` |
| `.github/workflows/ci.yml` | setup-node + ui-build before Go jobs that compile embed |
| `README.md` | Dashboard docs |
| `docs/ROADMAP.md` | Local web dashboard → Shipped (final task) |

---

### Task 1: Detail JSON parity

**Files:**
- Modify: `internal/api/server.go` (`runResponse`, `writeRun`)
- Modify: `internal/api/server_test.go`

**Interfaces:**
- Produces: `runResponse` fields `CreatedAt`, `UpdatedAt`, `CurrentStage string` with JSON `created_at`, `updated_at`, `current_stage`

- [ ] **Step 1: Write failing test**

Add to `internal/api/server_test.go` (extend an existing get-run test or add):

```go
func TestGetRunIncludesTimestampsAndCurrentStage(t *testing.T) {
	env := newTestEnv(t) // use existing test helper name in this file
	id := env.createDemoRun() // or POST /v1/runs as other tests do
	resp := env.get("/v1/runs/" + id)
	if resp.Code != 200 {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"created_at", "updated_at", "current_stage"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing %s in %v", key, body)
		}
	}
}
```

Adapt helper names to match `server_test.go` (`newTestEnv` may differ — use the file’s existing harness).

- [ ] **Step 2: Run test — expect FAIL**

```bash
go test ./internal/api/ -run TestGetRunIncludesTimestampsAndCurrentStage -count=1
```

Expected: FAIL (missing fields).

- [ ] **Step 3: Implement**

In `runResponse` add:

```go
CreatedAt    string `json:"created_at"`
UpdatedAt    string `json:"updated_at"`
CurrentStage string `json:"current_stage"`
```

In `writeRun`, after loading stages / pipeline, set:

```go
resp.CreatedAt = run.CreatedAt.UTC().Format(time.RFC3339)
resp.UpdatedAt = run.UpdatedAt.UTC().Format(time.RFC3339)
resp.CurrentStage = pipeline.CurrentStage(pl, stages) // pl may be nil → CurrentStage returns ""
```

Reuse the same `TemplateLoader` lookup already used for `outputsMode`.

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./internal/api/ -run TestGetRunIncludesTimestampsAndCurrentStage -count=1
```

- [ ] **Step 5: Commit** (ask attribution first)

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -m "$(cat <<'EOF'
Add created_at, updated_at, and current_stage to run detail JSON.

EOF
)"
```

---

### Task 2: Stage log endpoint

**Files:**
- Create: `internal/api/log.go`
- Create: `internal/api/log_test.go`
- Modify: `internal/api/server.go` (route registration)

**Interfaces:**
- Produces: `GET /v1/runs/{id}/stages/{stage_id}/log`
- Produces: `const MaxLogBytes = 1 << 20`
- Produces: `func readStageLog(runDir string, logPath string, tailLines int) (body []byte, truncated bool, err error)`

- [ ] **Step 1: Write failing tests** in `internal/api/log_test.go`

```go
package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStageLogAllowsUnderRunDir(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(runDir, "stage.log")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, trunc, err := readStageLog(runDir, p, 0)
	if err != nil || trunc || string(body) != "hello\nworld\n" {
		t.Fatalf("body=%q trunc=%v err=%v", body, trunc, err)
	}
}

func TestReadStageLogRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	outside := filepath.Join(dir, "secret.log")
	_ = os.WriteFile(outside, []byte("nope"), 0o644)
	_, _, err := readStageLog(runDir, outside, 0)
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestReadStageLogTailAndCap(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, "run")
	_ = os.MkdirAll(runDir, 0o755)
	p := filepath.Join(runDir, "stage.log")
	_ = os.WriteFile(p, []byte("a\nb\nc\n"), 0o644)
	body, _, err := readStageLog(runDir, p, 2)
	if err != nil || string(body) != "b\nc\n" {
		t.Fatalf("tail got %q err=%v", body, err)
	}
	big := strings.Repeat("x", MaxLogBytes+10)
	_ = os.WriteFile(p, []byte(big), 0o644)
	body, trunc, err := readStageLog(runDir, p, 0)
	if err != nil || !trunc || len(body) != MaxLogBytes {
		t.Fatalf("cap len=%d trunc=%v err=%v", len(body), trunc, err)
	}
}
```

Also add an HTTP handler test via existing env: 404 when no log_path; 200 text/plain with truncated header.

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./internal/api/ -run 'TestReadStageLog|TestGetStageLog' -count=1
```

- [ ] **Step 3: Implement `internal/api/log.go`**

```go
package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const MaxLogBytes = 1 << 20

func readStageLog(runDir, logPath string, tailLines int) ([]byte, bool, error) {
	if strings.TrimSpace(logPath) == "" {
		return nil, false, os.ErrNotExist
	}
	absRun, err := filepath.Abs(runDir)
	if err != nil {
		return nil, false, err
	}
	candidate := logPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRun, candidate)
	}
	absFile, err := filepath.Abs(candidate)
	if err != nil {
		return nil, false, err
	}
	rel, err := filepath.Rel(absRun, absFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, false, fmt.Errorf("log path escapes run dir")
	}
	data, err := os.ReadFile(absFile)
	if err != nil {
		return nil, false, err
	}
	truncated := false
	if len(data) > MaxLogBytes {
		data = data[len(data)-MaxLogBytes:]
		truncated = true
	}
	if tailLines > 0 {
		data = tailBytesByLines(data, tailLines)
	}
	return data, truncated, nil
}

func tailBytesByLines(data []byte, n int) []byte {
	if n <= 0 {
		return data
	}
	s := string(data)
	parts := strings.Split(s, "\n")
	// If file ends with \n, last split is empty — keep behavior simple:
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= n {
		return data
	}
	return []byte(strings.Join(parts[len(parts)-n:], "\n") + "\n")
}

func (s *Server) getStageLog(w http.ResponseWriter, r *http.Request) {
	runID, err := s.engine.Store.ResolveRunID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	stageID := chi.URLParam(r, "stage_id")
	run, err := s.engine.Store.GetRun(runID)
	if err != nil {
		writeError(w, httpStatusForError(err), err)
		return
	}
	stages, err := s.engine.Store.GetStageRuns(runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var logPath string
	found := false
	for _, st := range stages {
		if st.StageID == stageID {
			found = true
			if st.LogPath != nil {
				logPath = *st.LogPath
			}
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, fmt.Errorf("stage %s not found", stageID))
		return
	}
	tail := 0
	if raw := r.URL.Query().Get("tail"); raw != "" {
		tail, err = strconv.Atoi(raw)
		if err != nil || tail < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid tail"))
			return
		}
	}
	body, truncated, err := readStageLog(run.RunDir, logPath, tail)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("log not found"))
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if truncated {
		w.Header().Set("X-Shuttle-Log-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
```

Register in `NewHandler` **before** SPA (with other `/v1` routes):

```go
r.Get("/v1/runs/{id}/stages/{stage_id}/log", s.getStageLog)
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./internal/api/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/api/log.go internal/api/log_test.go internal/api/server.go internal/api/server_test.go
git commit -m "$(cat <<'EOF'
Add sandboxed stage log HTTP endpoint with tail and size cap.

EOF
)"
```

---

### Task 3: UI embed package + Make Node gate + placeholder

**Files:**
- Create: `internal/ui/embed.go`
- Create: `internal/ui/dist/index.html` (minimal placeholder)
- Create: `internal/ui/dist/.gitkeep` only if needed — prefer real `index.html`
- Modify: `Makefile`
- Modify: `.gitignore`

**Interfaces:**
- Produces: `package ui` with `var Dist embed.FS` and `func Handler() http.Handler`

- [ ] **Step 1: Placeholder + embed**

`internal/ui/dist/index.html`:

```html
<!doctype html>
<html lang="en">
  <head><meta charset="utf-8"/><title>Shuttle</title></head>
  <body><p>UI placeholder — run make ui-build</p></body>
</html>
```

`internal/ui/embed.go`:

```go
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var Dist embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(Dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
```

- [ ] **Step 2: Makefile targets**

Add:

```makefile
NODE ?= node
NPM  ?= npm

.PHONY: ui-build check-node

check-node: ## require Node/npm for UI
	@command -v $(NODE) >/dev/null || { echo "node required for UI build; install Node 22+"; exit 1; }
	@command -v $(NPM) >/dev/null || { echo "npm required for UI build"; exit 1; }

ui-build: check-node ## build web/ into internal/ui/dist
	cd web && $(NPM) ci && $(NPM) run build
	rm -rf internal/ui/dist
	mkdir -p internal/ui/dist
	cp -R web/dist/. internal/ui/dist/

build: ui-build ## build bin/shuttle (requires Node)
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o $(BIN) $(CMD)
	@echo "built $(BIN)"
```

Until `web/` exists, keep `ui-build` depending on Task 4 — for this task only, make `ui-build` succeed if `web/package.json` missing by documenting order: **Task 4 before flipping `build` to require ui-build**. Practical sequence: create stub `web/package.json` in Task 4 first, or temporarily:

```makefile
ui-build: check-node
	@if [ ! -f web/package.json ]; then echo "web/ missing — complete Task 4"; exit 1; fi
	...
```

Add to `.gitignore`:

```
web/node_modules/
web/dist/
```

Do **not** gitignore `internal/ui/dist/` (placeholder + built assets committed after ui-build, or commit placeholder and refresh on build — prefer committing built assets from `make ui-build` in CI so clones without running npm still have last CI build; **this plan commits placeholder and CI/make always refresh**).

- [ ] **Step 3: Compile check**

```bash
go test ./internal/ui/ -count=1
go build -o /dev/null ./internal/ui/
```

- [ ] **Step 4: Commit**

```bash
git add internal/ui Makefile .gitignore
git commit -m "$(cat <<'EOF'
Add embeddable UI package and Node-gated Make ui-build target.

EOF
)"
```

---

### Task 4: Scaffold Vite React TypeScript app

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/tsconfig.app.json`, `web/tsconfig.node.json`, `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/.nvmrc`, `web/vitest.config.ts`

**Interfaces:**
- Produces: `npm run build` → `web/dist`
- Produces: Vite proxy `/v1` → `http://127.0.0.1:8080`
- Produces: `outDir` compatible with Make copy to `internal/ui/dist`

- [ ] **Step 1: Create app** (from repo root)

```bash
cd web 2>/dev/null || mkdir web && cd web
# Prefer npm create:
npm create vite@latest . -- --template react-ts
```

Then set `"engines": { "node": ">=22" }`, add `.nvmrc` with `22`, add dependencies:

```bash
npm install react-router-dom
npm install -D vitest jsdom @testing-library/react @testing-library/jest-dom
```

Verify each package license is MIT/Apache-2.0.

`vite.config.ts` essentials:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { "/v1": "http://127.0.0.1:8080" },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
  },
});
```

`package.json` scripts:

```json
{
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  }
}
```

- [ ] **Step 2: Smoke build**

```bash
cd web && npm ci && npm run build && npm test
```

Expected: `web/dist/index.html` exists; vitest passes (default).

- [ ] **Step 3: Wire Make ui-build and verify build**

```bash
make ui-build
make build
```

Expected: `bin/shuttle` builds with refreshed `internal/ui/dist`.

- [ ] **Step 4: Commit**

```bash
git add web internal/ui/dist Makefile
git commit -m "$(cat <<'EOF'
Scaffold Vite React TypeScript dashboard app under web/.

EOF
)"
```

---

### Task 5: API client + action helpers (Vitest)

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/actions.ts`
- Create: `web/src/api/actions.test.ts`
- Create: `web/src/api/types.ts`
- Modify: `Makefile` `verify` to run `cd web && npm test`

**Interfaces:**
- Produces:
  - `export type Run = { id: string; status: string; stages: Stage[]; ... }`
  - `export function waitingGateStageId(stages: { id: string; status: string }[]): string | null` — one waiting_approval → id; else null
  - `export function canResume(status: string): boolean` // failed|cancelled
  - `export function canRetry(status: string): boolean` // terminal
  - `export function canCancel(status: string): boolean` // running|waiting_approval (match engine)
  - `export const longTimeoutMs = 30 * 60 * 1000`
  - `export async function apiGet/json/post...`

- [ ] **Step 1: Failing Vitest**

```ts
import { describe, expect, it } from "vitest";
import { waitingGateStageId, canResume } from "./actions";

describe("waitingGateStageId", () => {
  it("returns the sole waiting_approval stage", () => {
    expect(
      waitingGateStageId([
        { id: "review", status: "succeeded" },
        { id: "approve_ship", status: "waiting_approval" },
      ]),
    ).toBe("approve_ship");
  });
  it("returns null when none or multiple", () => {
    expect(waitingGateStageId([{ id: "a", status: "succeeded" }])).toBeNull();
    expect(
      waitingGateStageId([
        { id: "a", status: "waiting_approval" },
        { id: "b", status: "waiting_approval" },
      ]),
    ).toBeNull();
  });
});

describe("canResume", () => {
  it("allows failed and cancelled", () => {
    expect(canResume("failed")).toBe(true);
    expect(canResume("cancelled")).toBe(true);
    expect(canResume("succeeded")).toBe(false);
  });
});
```

- [ ] **Step 2: Run — FAIL**

```bash
cd web && npm test
```

- [ ] **Step 3: Implement helpers + client**

`actions.ts` implements the functions above.  
`client.ts`:

```ts
export const longTimeoutMs = 30 * 60 * 1000;

export async function apiJSON<T>(
  path: string,
  init: RequestInit & { timeoutMs?: number } = {},
): Promise<T> {
  const timeoutMs = init.timeoutMs ?? 30_000;
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(path, { ...init, signal: ctrl.signal });
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) {
      throw new Error(data?.error ?? res.statusText);
    }
    return data as T;
  } finally {
    clearTimeout(t);
  }
}
```

Align `canCancel` / `canRetry` with `internal/pipeline` + API behavior (read engine Cancel and recovery allowed statuses while implementing).

- [ ] **Step 4: PASS + Makefile verify includes web tests**

```makefile
verify: build lint test ui-test ## build, lint, go tests, UI tests

ui-test: check-node
	cd web && $(NPM) ci && $(NPM) test
```

```bash
make ui-test
```

- [ ] **Step 5: Commit**

```bash
git add web Makefile
git commit -m "$(cat <<'EOF'
Add dashboard API client helpers and Vitest coverage for gate actions.

EOF
)"
```

---

### Task 6: Mount SPA on chi without shadowing `/v1`

**Files:**
- Modify: `internal/api/server.go` `NewHandler`
- Modify: `internal/api/server_test.go`
- Modify: `internal/cli/root.go` only if serve message should mention UI (optional here; README in Task 9)

- [ ] **Step 1: Failing test**

```go
func TestSPADoesNotShadowAPI(t *testing.T) {
	h := api.NewHandler(eng, opts) // existing harness
	// GET /v1/pipelines → JSON
	// GET /runs/fake → HTML index (SPA fallback)
}
```

- [ ] **Step 2: Implement mount**

```go
import "github.com/savitharaghunathan/shuttle/internal/ui"

// after all /v1 routes:
r.Mount("/", ui.Handler())
```

Ensure `/v1` routes remain registered on the same router **before** `Mount("/")`.

- [ ] **Step 3: PASS**

```bash
go test ./internal/api/ -count=1
```

- [ ] **Step 4: Commit**

```bash
git add internal/api
git commit -m "$(cat <<'EOF'
Serve embedded SPA from shuttle serve without shadowing /v1.

EOF
)"
```

---

### Task 7: Runs list + detail UI (poll + logs + actions)

**Files:**
- Create: `web/src/pages/RunsList.tsx`
- Create: `web/src/pages/RunDetail.tsx`
- Create: `web/src/components/StageList.tsx`
- Create: `web/src/components/LogViewer.tsx`
- Create: `web/src/components/ActionBar.tsx`
- Modify: `web/src/App.tsx` (routes)

**Interfaces:**
- Consumes: `apiJSON`, action helpers from Task 5
- Routes: `/`, `/runs/:id`

- [ ] **Step 1: Implement list page** — table of runs from `GET /v1/runs`; link to detail; CTA to `/runs/new`; empty state.

- [ ] **Step 2: Implement detail** — load `GET /v1/runs/:id`; poll every 2s while status not in `succeeded|failed|cancelled`; stop on terminal; ActionBar calls:
  - resume/retry/approve/reject with `timeoutMs: longTimeoutMs`, disable button while in flight
  - cancel with default timeout
  - approve/reject use `waitingGateStageId` (disable if null)
- [ ] **Step 3: LogViewer** — `GET /v1/runs/:id/stages/:stageId/log?tail=200` as text; show truncated notice if header set (expose via `fetch` not `apiJSON`).

- [ ] **Step 4: Manual smoke**

```bash
make build
# in test repo: shuttle serve
# open http://127.0.0.1:8080/ — list loads
```

- [ ] **Step 5: Commit**

```bash
git add web internal/ui/dist
git commit -m "$(cat <<'EOF'
Add runs list and detail views with polling, logs, and actions.

EOF
)"
```

---

### Task 8: New run form

**Files:**
- Create: `web/src/pages/NewRun.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Form fields**
  - template select from `GET /v1/pipelines`
  - Issue URL (primary)
  - Advanced: absolute spec path (helper text: path on machine running serve)
  - headless, agent, model, remote checkboxes/inputs
  - Validate exactly one of issue URL / spec path before submit

- [ ] **Step 2: Submit**

```ts
await apiJSON(`/v1/runs`, {
  method: "POST",
  timeoutMs: longTimeoutMs,
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    template_id,
    issue_url: issue || undefined,
    spec_path: spec || undefined,
    headless,
    agent,
    model,
    remote,
  }),
});
// navigate to /runs/:id
```

Show `error` inline on failure; disable submit while in flight.

- [ ] **Step 3: Manual smoke** with issue URL against `SHUTTLE_TEST_APP` / serve-test-app.

- [ ] **Step 4: Commit**

```bash
git add web internal/ui/dist
git commit -m "$(cat <<'EOF'
Add new-run form with issue-URL-first create flow.

EOF
)"
```

---

### Task 9: CI, README, ROADMAP

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

- [ ] **Step 1: CI Node + ui-build**

For `lint`, `test`, and `integration` jobs (any that `go build`/`go test` packages importing `internal/ui` or building the binary):

```yaml
- uses: actions/setup-node@v4
  with:
    node-version-file: web/.nvmrc
    cache: npm
    cache-dependency-path: web/package-lock.json

- name: UI build
  run: make ui-build

- name: UI test
  run: make ui-test
```

Place **before** Go test/lint steps that compile the module.

- [ ] **Step 2: README** — under serve section:
  - Open `http://127.0.0.1:8080/` for the dashboard
  - Localhost-only / unauthenticated
  - Node 22+ required to build
  - Create run: issue URL primary; absolute `spec_path` is server-local
  - Create/resume/approve POSTs can take minutes

- [ ] **Step 3: ROADMAP** — move **Local web dashboard** from Next → Shipped with a short shipped blurb.

- [ ] **Step 4: Full verify**

```bash
make verify
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml README.md docs/ROADMAP.md
git commit -m "$(cat <<'EOF'
Wire dashboard into CI and mark Local web dashboard shipped.

EOF
)"
```

---

## Self-review (plan vs spec)

| Spec requirement | Task |
|------------------|------|
| Detail `created_at`/`updated_at`/`current_stage` | 1 |
| Log endpoint + sandbox + tail + 1MiB cap + header | 2 |
| embed + Node required + empty-dir constraint | 3–4 |
| Vite/React/TS + proxy | 4 |
| Vitest action helpers in verify | 5 |
| SPA mount, deep links, no `/v1` shadow | 6 |
| List/detail/poll/logs/actions | 7 |
| Issue-first create + long POST | 8 |
| CI Node, README, ROADMAP Shipped | 9 |
| No force retry / no upload / no SSE | Global + out of scope |

Byte cap fixed at 1MiB. Cancel vs long mutations reflected in Task 5/7 timeouts.
