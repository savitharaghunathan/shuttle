# API / CLI Run Create Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Share create-run template/input resolution between CLI and HTTP so `POST /v1/runs` matches CLI policy (headless swap, spec/issue rules, issue → SpecContent), rejects issues for repos the server doesn't serve, and exposes `outputs_mode` on run JSON.

**Architecture:** New `internal/runinput` owns `ResolveTemplate` + `ResolveInput` and their typed errors. CLI maps the errors to flag wording; `api.createRun` maps them to HTTP status codes and adds a served-repo check. `pipeline.OutputsMode` is the single definition of strict/bridge.

**Tech Stack:** Go, chi, cobra, existing `issue` / `registry` / `home` / `pipeline` packages, httptest

**Spec:** [`docs/superpowers/specs/2026-10-16-api-cli-run-parity-design.md`](../specs/2026-10-16-api-cli-run-parity-design.md) (rev 4)

Tasks 3–5 land on one branch: between Task 3 and Task 5 the smoke script's create body is rejected. Don't merge Task 3 alone.

## Global Constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged: localhost API unauthenticated
- Prefer MIT / Apache-2.0 OSS libraries
- Commits: `git commit -s` (Signed-off-by) with trailer `Assisted-by: Cursor <cursoragent@cursor.com>`
- No OpenAPI, no async create, no new log/artifact endpoints, no per-repo engine in this plan
- Tests that touch go-git or `$TMPDIR` fail inside the Cursor sandbox; run suites unsandboxed

---

## File Map

| File | Responsibility |
|------|----------------|
| `internal/runinput/errors.go` | Typed errors shared by CLI and API |
| `internal/runinput/template.go` | `ResolveTemplate` |
| `internal/runinput/template_test.go` | Template matrix (moved from CLI) |
| `internal/runinput/input.go` | `ResolveInput`, `Options`, `Input` |
| `internal/runinput/input_test.go` | Issue/spec resolution unit tests |
| `internal/pipeline/outputs.go` | Add exported `OutputsMode` |
| `internal/pipeline/outputs_test.go` | Cover `OutputsMode` |
| `internal/cli/root.go` | Call `runinput`; `cliRunInputError` mapping; thin `outputsMode` wrapper |
| `internal/cli/root_test.go` | Drop moved matrix; keep flag-message tests; add headless-unsupported test |
| `internal/api/server.go` | `headless`, shared resolvers, served-repo check, status mapping, `SpecContent`, `outputs_mode` |
| `internal/api/server_test.go` | Parity + served-repo + outputs_mode tests |
| `examples/scripts/smoke-test-app.sh` | Post `issue_url` only |
| `README.md` | Document API rules + CLI headless change |

---

### Task 0: `pipeline.OutputsMode`

**Files:**
- Modify: `internal/pipeline/outputs.go`
- Modify: `internal/pipeline/outputs_test.go`

**Interfaces:**
- Produces: `func OutputsMode(templateID string, pl *Pipeline) string` — `"strict"`, `"bridge"`, or `""` (no declared outputs or nil pipeline)

- [ ] **Step 1: Write the failing test** (append to `outputs_test.go`)

```go
func TestOutputsMode(t *testing.T) {
	withOutputs := &Pipeline{ID: "x", Stages: []Stage{{ID: "plan", Outputs: []string{"summary"}}}}
	noOutputs := &Pipeline{ID: "y", Stages: []Stage{{ID: "build"}}}

	tests := []struct {
		id   string
		pl   *Pipeline
		want string
	}{
		{id: "sdlc", pl: withOutputs, want: "strict"},
		{id: "sdlc-headless", pl: withOutputs, want: "strict"},
		{id: "sdlc-dry", pl: withOutputs, want: "bridge"},
		{id: "pr-factory", pl: noOutputs, want: ""},
		{id: "sdlc", pl: nil, want: ""},
	}
	for _, tc := range tests {
		if got := OutputsMode(tc.id, tc.pl); got != tc.want {
			t.Fatalf("OutputsMode(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -count=1 -run TestOutputsMode -v`
Expected: FAIL — `undefined: OutputsMode`

- [ ] **Step 3: Implement** (add to `outputs.go` below `IsStrictOutputTemplate`)

```go
// OutputsMode reports how declared stage outputs are enforced: "strict" fails a
// stage that produced no artifact, "bridge" falls back to worker stdout. It
// returns "" when the pipeline declares no outputs.
func OutputsMode(templateID string, pl *Pipeline) string {
	if pl == nil {
		return ""
	}
	declaresOutputs := false
	for _, stage := range pl.Stages {
		if len(stage.Outputs) > 0 {
			declaresOutputs = true
			break
		}
	}
	if !declaresOutputs {
		return ""
	}
	if IsStrictOutputTemplate(templateID) {
		return "strict"
	}
	return "bridge"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -count=1 -run TestOutputsMode -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/outputs.go internal/pipeline/outputs_test.go
git commit -s -m "$(cat <<'EOF'
feat(pipeline): export OutputsMode for CLI and API run JSON

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 1: `runinput` errors + `ResolveTemplate`, CLI mapping

**Files:**
- Create: `internal/runinput/errors.go`
- Create: `internal/runinput/template.go`
- Create: `internal/runinput/template_test.go`
- Modify: `internal/cli/root.go` (call `runinput.ResolveTemplate`; add `cliRunInputError`; delete `resolveTemplateID` at ~line 127)
- Modify: `internal/cli/root_test.go` (delete `TestResolveTemplateID`; add headless-unsupported test)

**Interfaces:**
- Produces (`package runinput`):
  - `var ErrSpecAndIssue error`
  - `type InputRequiredError struct{ Product string }`
  - `type HeadlessUnsupportedError struct{ TemplateID string }`
  - `type RepoNotImportedError struct{ Owner, Name string }` (used in Task 2)
  - `type FetchError struct{ Err error }` with `Unwrap()` (used in Task 2)
  - `func ResolveTemplate(templateID string, headless, hasIssue, hasSpec bool) (string, error)`
- Produces (`package cli`): `func cliRunInputError(err error) error`

- [ ] **Step 1: Create `errors.go`** (no behavior to test on its own; tests come with `ResolveTemplate`)

```go
package runinput

import (
	"errors"
	"fmt"
)

// ErrSpecAndIssue is returned when a run is given both a spec and an issue.
var ErrSpecAndIssue = errors.New("use either issue_url or spec_path, not both")

// InputRequiredError reports a product template started with no spec or issue.
type InputRequiredError struct{ Product string }

func (e *InputRequiredError) Error() string {
	return "spec_path or issue_url is required for " + e.Product
}

// HeadlessUnsupportedError reports headless requested for a template that has
// no headless variant.
type HeadlessUnsupportedError struct{ TemplateID string }

func (e *HeadlessUnsupportedError) Error() string {
	return "headless is not supported for template " + e.TemplateID + " (no headless variant)"
}

// RepoNotImportedError reports an issue whose repo is missing from the registry.
type RepoNotImportedError struct{ Owner, Name string }

func (e *RepoNotImportedError) Error() string {
	return fmt.Sprintf("repo %s/%s not imported; run: shuttle import repo https://github.com/%s/%s", e.Owner, e.Name, e.Owner, e.Name)
}

// FetchError wraps a failure to fetch an issue body. It is not a caller error.
type FetchError struct{ Err error }

func (e *FetchError) Error() string { return "fetch issue: " + e.Err.Error() }

func (e *FetchError) Unwrap() error { return e.Err }
```

- [ ] **Step 2: Write the failing test** (`template_test.go`)

```go
package runinput

import (
	"errors"
	"testing"
)

func TestResolveTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		headless bool
		hasIssue bool
		hasSpec  bool
		want     string
	}{
		{name: "default is gated pr-factory", hasSpec: true, want: "pr-factory"},
		{name: "bare issue is pr-factory-headless", hasIssue: true, want: "pr-factory-headless"},
		{name: "bare headless is pr-factory-headless", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps pr-factory", template: "pr-factory", headless: true, hasSpec: true, want: "pr-factory-headless"},
		{name: "headless swaps sdlc", template: "sdlc", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "headless no-op on sdlc-headless", template: "sdlc-headless", headless: true, hasSpec: true, want: "sdlc-headless"},
		{name: "headless no-op on headless overlay path", template: "pipelines/sdlc-dry-headless.yaml", headless: true, want: "pipelines/sdlc-dry-headless.yaml"},
		{name: "sdlc with issue", template: "sdlc", hasIssue: true, want: "sdlc"},
		{name: "overlay needs no spec", template: "sdlc-dry", want: "sdlc-dry"},
		{name: "pipeline path needs no spec", template: "pipelines/sdlc-dry.yaml", want: "pipelines/sdlc-dry.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTemplate(tc.template, tc.headless, tc.hasIssue, tc.hasSpec)
			if err != nil {
				t.Fatalf("ResolveTemplate(%q) error: %v", tc.template, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveTemplate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestResolveTemplateInputRequired(t *testing.T) {
	tests := []struct {
		template string
		headless bool
		product  string
	}{
		{template: "pr-factory", product: "pr-factory"},
		{template: "pr-factory-headless", product: "pr-factory"},
		{template: "", headless: true, product: "pr-factory"},
		{template: "sdlc", product: "sdlc"},
		{template: "sdlc-headless", product: "sdlc"},
	}
	for _, tc := range tests {
		_, err := ResolveTemplate(tc.template, tc.headless, false, false)
		var required *InputRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("ResolveTemplate(%q) error = %v, want *InputRequiredError", tc.template, err)
		}
		if required.Product != tc.product {
			t.Fatalf("ResolveTemplate(%q) product = %q, want %q", tc.template, required.Product, tc.product)
		}
	}
}

func TestResolveTemplateSpecAndIssue(t *testing.T) {
	_, err := ResolveTemplate("sdlc", false, true, true)
	if !errors.Is(err, ErrSpecAndIssue) {
		t.Fatalf("error = %v, want ErrSpecAndIssue", err)
	}
}

func TestResolveTemplateHeadlessUnsupported(t *testing.T) {
	for _, id := range []string{"demo", "sdlc-dry", "pipelines/sdlc-dry.yaml"} {
		_, err := ResolveTemplate(id, true, false, true)
		var unsupported *HeadlessUnsupportedError
		if !errors.As(err, &unsupported) {
			t.Fatalf("ResolveTemplate(%q, headless) error = %v, want *HeadlessUnsupportedError", id, err)
		}
		if unsupported.TemplateID != id {
			t.Fatalf("TemplateID = %q, want %q", unsupported.TemplateID, id)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runinput/ -count=1 -run TestResolveTemplate -v`
Expected: FAIL — `undefined: ResolveTemplate`

- [ ] **Step 4: Implement `template.go`**

```go
package runinput

import (
	"path/filepath"
	"strings"
)

// ResolveTemplate applies template defaulting, the headless swap, and input
// validation for a new run. It does no I/O.
func ResolveTemplate(templateID string, headless, hasIssue, hasSpec bool) (string, error) {
	if hasIssue && hasSpec {
		return "", ErrSpecAndIssue
	}

	if templateID == "" {
		if headless || hasIssue {
			templateID = "pr-factory-headless"
		} else {
			templateID = "pr-factory"
		}
	}
	if headless {
		switch {
		case templateID == "pr-factory":
			templateID = "pr-factory-headless"
		case templateID == "sdlc":
			templateID = "sdlc-headless"
		case isHeadlessTemplate(templateID):
		default:
			return "", &HeadlessUnsupportedError{TemplateID: templateID}
		}
	}

	if !hasIssue && !hasSpec {
		switch templateID {
		case "pr-factory", "pr-factory-headless":
			return "", &InputRequiredError{Product: "pr-factory"}
		case "sdlc", "sdlc-headless":
			return "", &InputRequiredError{Product: "sdlc"}
		}
	}
	return templateID, nil
}

func isHeadlessTemplate(templateID string) bool {
	base := filepath.Base(templateID)
	for _, ext := range []string{".yaml", ".yml"} {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.HasSuffix(base, "-headless")
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runinput/ -count=1 -run TestResolveTemplate -v`
Expected: PASS

- [ ] **Step 6: Wire CLI** — in `newRunCommand` (`internal/cli/root.go` ~line 75) replace the `resolveTemplateID` call:

```go
			resolvedTemplate, err := runinput.ResolveTemplate(templateID, headless, issueURL != "", specPath != "")
			if err != nil {
				return cliRunInputError(err)
			}
```

Delete `resolveTemplateID` and its doc comment. Add next to `resolveRunInput`:

```go
// cliRunInputError rewrites runinput errors with the CLI's flag names.
func cliRunInputError(err error) error {
	var required *runinput.InputRequiredError
	var unsupported *runinput.HeadlessUnsupportedError
	switch {
	case errors.Is(err, runinput.ErrSpecAndIssue):
		return errors.New("use either --issue or --spec, not both")
	case errors.As(err, &required):
		return fmt.Errorf("--spec is required for %s (or use --issue)", required.Product)
	case errors.As(err, &unsupported):
		return fmt.Errorf("--headless is not supported for template %s (no headless variant)", unsupported.TemplateID)
	default:
		return err
	}
}
```

Add import `"github.com/savitharaghunathan/shuttle/internal/runinput"`.

- [ ] **Step 7: Update CLI tests** — in `internal/cli/root_test.go` delete `TestResolveTemplateID` (now covered in `runinput`). Keep `TestRunRequiresSpecForPRFactory` / `TestRunRequiresSpecForSDLC` unchanged. Add:

```go
func TestRunHeadlessUnsupportedTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "sdlc-dry", "--headless", "--spec", "spec.md"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run --headless on sdlc-dry succeeded, want error")
	}
	if !strings.Contains(err.Error(), "--headless is not supported for template sdlc-dry") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunRejectsSpecAndIssue(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs([]string{"run", "-t", "sdlc", "--spec", "spec.md", "--issue", "https://github.com/acme/widget/issues/1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("run with --spec and --issue succeeded, want error")
	}
	if !strings.Contains(err.Error(), "use either --issue or --spec, not both") {
		t.Fatalf("error=%v", err)
	}
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/runinput/ ./internal/cli/ -count=1 -run 'ResolveTemplate|RunRequires|RunHeadless|RunRejects' -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/runinput/ internal/cli/root.go internal/cli/root_test.go
git commit -s -m "$(cat <<'EOF'
feat(cli)!: move template resolution into internal/runinput

Typed errors let CLI and API word failures for their own surface.

BREAKING CHANGE: --headless on a template with no headless variant is
now an error instead of a silent gated run.

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 2: `runinput.ResolveInput`

**Files:**
- Create: `internal/runinput/input.go`
- Create: `internal/runinput/input_test.go`
- Modify: `internal/cli/root.go` (replace `resolveRunInput` body with a call to `runinput.ResolveInput`)

**Interfaces:**
- Consumes: `RepoNotImportedError`, `FetchError` from Task 1
- Produces:

```go
type Input struct {
	CWD         string
	SpecContent string
	IssueRef    string
}

type Options struct {
	FallbackCWD string
	LookupRepo  func(owner, name string) (localPath string, err error)
	FetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}

func ResolveInput(ctx context.Context, issueURL string, opts Options) (Input, error)
```

- [ ] **Step 1: Write the failing tests** (`input_test.go`)

```go
package runinput

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/registry"
)

const issueURL = "https://github.com/acme/widget/issues/42"

func lookupAt(path string) func(string, string) (string, error) {
	return func(owner, name string) (string, error) { return path, nil }
}

func fetchReturning(body string) func(context.Context, string, string, int) (string, error) {
	return func(context.Context, string, string, int) (string, error) { return body, nil }
}

func TestResolveInputSpecOnlyUsesFallbackCWD(t *testing.T) {
	in, err := ResolveInput(context.Background(), "", Options{FallbackCWD: "/served"})
	if err != nil {
		t.Fatal(err)
	}
	if in.CWD != "/served" || in.SpecContent != "" || in.IssueRef != "" {
		t.Fatalf("input = %+v, want CWD /served and no spec/issue", in)
	}
}

func TestResolveInputFetchesSpec(t *testing.T) {
	in, err := ResolveInput(context.Background(), issueURL, Options{
		FallbackCWD: "/other",
		LookupRepo:  lookupAt("/imported"),
		FetchSpec:   fetchReturning("# Title\n\nbody"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.CWD != "/imported" {
		t.Fatalf("CWD = %q, want /imported (FallbackCWD ignored on issue path)", in.CWD)
	}
	if in.SpecContent != "# Title\n\nbody" {
		t.Fatalf("SpecContent = %q", in.SpecContent)
	}
	if in.IssueRef != "acme/widget#42" {
		t.Fatalf("IssueRef = %q, want acme/widget#42", in.IssueRef)
	}
}

func TestResolveInputBadIssueURL(t *testing.T) {
	_, err := ResolveInput(context.Background(), "not-a-url", Options{LookupRepo: lookupAt("/x")})
	if err == nil || !strings.Contains(err.Error(), "unsupported issue url") {
		t.Fatalf("error = %v, want unsupported issue url", err)
	}
}

func TestResolveInputRequiresLookup(t *testing.T) {
	_, err := ResolveInput(context.Background(), issueURL, Options{})
	if err == nil {
		t.Fatal("issue url without LookupRepo succeeded, want error")
	}
}

func TestResolveInputRepoNotImported(t *testing.T) {
	_, err := ResolveInput(context.Background(), issueURL, Options{
		LookupRepo: func(string, string) (string, error) { return "", registry.ErrNotFound },
	})
	var notImported *RepoNotImportedError
	if !errors.As(err, &notImported) {
		t.Fatalf("error = %v, want *RepoNotImportedError", err)
	}
	if notImported.Owner != "acme" || notImported.Name != "widget" {
		t.Fatalf("repo = %s/%s, want acme/widget", notImported.Owner, notImported.Name)
	}
}

func TestResolveInputFetchFailure(t *testing.T) {
	cause := errors.New("gh: not logged in")
	_, err := ResolveInput(context.Background(), issueURL, Options{
		LookupRepo: lookupAt("/imported"),
		FetchSpec: func(context.Context, string, string, int) (string, error) {
			return "", cause
		},
	})
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("error = %v, want *FetchError", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error %v does not unwrap to cause", err)
	}
}
```

Before writing the not-imported test, confirm the sentinel name: `rg -n 'ErrNotFound' internal/registry`. The CLI already uses `registry.ErrNotFound`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runinput/ -count=1 -run TestResolveInput -v`
Expected: FAIL — `undefined: ResolveInput`

- [ ] **Step 3: Implement `input.go`**

```go
package runinput

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/registry"
)

// Input is the resolved checkout and spec for a new run.
type Input struct {
	CWD         string
	SpecContent string
	IssueRef    string
}

// Options supplies the environment ResolveInput needs. LookupRepo is required
// when an issue URL is given; FetchSpec defaults to issue.FetchSpec.
type Options struct {
	FallbackCWD string
	LookupRepo  func(owner, name string) (localPath string, err error)
	FetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}

// ResolveInput resolves where a run executes and, for issue runs, fetches the
// issue body as the run spec. Spec files are passed to StartRun by the caller.
func ResolveInput(ctx context.Context, issueURL string, opts Options) (Input, error) {
	if issueURL == "" {
		cwd := opts.FallbackCWD
		if cwd == "" {
			wd, err := os.Getwd()
			if err != nil {
				return Input{}, err
			}
			cwd = wd
		}
		return Input{CWD: cwd}, nil
	}

	owner, name, number, err := issue.ParseIssueURL(issueURL)
	if err != nil {
		return Input{}, err
	}
	if opts.LookupRepo == nil {
		return Input{}, errors.New("runinput: LookupRepo is required to resolve an issue url")
	}
	path, err := opts.LookupRepo(owner, name)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return Input{}, &RepoNotImportedError{Owner: owner, Name: name}
		}
		return Input{}, err
	}

	fetch := opts.FetchSpec
	if fetch == nil {
		fetch = issue.FetchSpec
	}
	spec, err := fetch(ctx, owner, name, number)
	if err != nil {
		return Input{}, &FetchError{Err: err}
	}
	return Input{
		CWD:         path,
		SpecContent: spec,
		IssueRef:    fmt.Sprintf("%s/%s#%d", owner, name, number),
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runinput/ -count=1 -v`
Expected: PASS

- [ ] **Step 5: Wire CLI** — replace `resolveRunInput` in `internal/cli/root.go`. Drop the unused `specPath` parameter (the repo's lint set — `govet`, `ineffassign`, `staticcheck`, `unused` — never reports unused parameters, so don't wait for it) and update the call at ~line 85 to `resolveRunInput(cmd.Context(), issueURL)`:

```go
func resolveRunInput(ctx context.Context, issueURL string) (cwd, specContent, issueRef string, err error) {
	var reg *registry.Registry
	defer func() {
		if reg != nil {
			_ = reg.Close()
		}
	}()
	opts := runinput.Options{
		LookupRepo: func(owner, name string) (string, error) {
			if reg == nil {
				opened, err := openRegistry()
				if err != nil {
					return "", err
				}
				reg = opened
			}
			entry, err := reg.Get(owner, name)
			if err != nil {
				return "", err
			}
			return entry.Path, nil
		},
	}
	in, err := runinput.ResolveInput(ctx, issueURL, opts)
	if err != nil {
		return "", "", "", err
	}
	return in.CWD, in.SpecContent, in.IssueRef, nil
}
```

The registry is opened on the first lookup only, so spec-only runs and malformed URLs never touch `SHUTTLE_HOME`. `root.go` keeps its `registry` import (for `*registry.Registry`, which `openRegistry` in `import.go` returns).

CLI fetch failures now read `fetch issue: <gh error>` instead of the bare `gh` error (accepted in spec rev 4).

- [ ] **Step 6: Run CLI tests**

Run: `go test ./internal/runinput/ ./internal/cli/ -count=1 && go test -tags=integration ./internal/cli/ -count=1 -run 'Issue|ShipOpensPR'`
Expected: PASS (`TestE2ESDLCIssueSmoke` and `TestE2ESDLCShipOpensPR` exercise the issue path end to end)

- [ ] **Step 7: Commit**

```bash
git add internal/runinput/ internal/cli/root.go
git commit -s -m "$(cat <<'EOF'
refactor: move issue/spec input resolution into internal/runinput

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 3: API `createRun` parity

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`

**Interfaces:**
- Consumes: `runinput.ResolveTemplate`, `runinput.ResolveInput`, `runinput.FetchError` (Tasks 1–2)
- Produces: `api.Options.LookupRepo`, `api.Options.FetchSpec` (optional injectors; nil in production); request field `headless`

- [ ] **Step 1: Write the failing tests** (append to `server_test.go`)

Add a test env helper that serves a fixed directory and accepts any template id, with injectable lookups:

```go
func newParityTestEnv(t *testing.T, lookup func(string, string) (string, error), fetch func(context.Context, string, string, int) (string, error)) *recoveryTestEnv {
	t.Helper()
	t.Setenv("SHUTTLE_HOME", t.TempDir())
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "built"},
	}))
	cfg := &config.Config{
		WorkRoot:       filepath.Join(dir, ".shuttle"),
		Concurrency:    1,
		DefaultWorker:  "fake",
		DefaultTimeout: 5 * time.Minute,
	}
	eng := pipeline.NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0
	eng.TemplateLoader = func(id string) (*pipeline.Pipeline, error) {
		if id == "unknown" {
			return nil, errors.New("template unknown not found")
		}
		return &pipeline.Pipeline{ID: id, Stages: []pipeline.Stage{{ID: "build"}}}, nil
	}
	return &recoveryTestEnv{
		t:     t,
		dir:   dir,
		store: s,
		reg:   reg,
		handler: api.NewHandler(eng, api.Options{
			CWD:         dir,
			PipelineIDs: []string{"demo"},
			LookupRepo:  lookup,
			FetchSpec:   fetch,
		}),
	}
}

func writeSpecFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(path, []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeRun(t *testing.T, resp *httptest.ResponseRecorder) (id, templateID, cwd, runDir string) {
	t.Helper()
	var run struct {
		ID         string `json:"id"`
		TemplateID string `json:"template_id"`
		CWD        string `json:"cwd"`
		RunDir     string `json:"run_dir"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v\n%s", err, resp.Body.String())
	}
	return run.ID, run.TemplateID, run.CWD, run.RunDir
}

func assertStatus(t *testing.T, resp *httptest.ResponseRecorder, want int, bodyContains string) {
	t.Helper()
	if resp.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, want, resp.Body.String())
	}
	if bodyContains != "" && !strings.Contains(resp.Body.String(), bodyContains) {
		t.Fatalf("body %s missing %q", resp.Body.String(), bodyContains)
	}
}

func TestCreateRunRequiresSpecOrIssueForSDLC(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"sdlc"}`), http.StatusBadRequest, "spec_path or issue_url is required for sdlc")
}

func TestCreateRunRejectsSpecAndIssueTogether(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	spec := writeSpecFile(t, env.dir)
	body := `{"template_id":"sdlc","spec_path":"` + spec + `","issue_url":"https://github.com/acme/widget/issues/1"}`
	assertStatus(t, env.post("/v1/runs", body), http.StatusBadRequest, "not both")
}

func TestCreateRunRejectsRelativeSpecPath(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"sdlc","spec_path":"specs/x.md"}`), http.StatusBadRequest, "absolute")
}

func TestCreateRunHeadlessSwapsSDLC(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	spec := writeSpecFile(t, env.dir)
	resp := env.post("/v1/runs", `{"template_id":"sdlc","headless":true,"spec_path":"`+spec+`"}`)
	assertStatus(t, resp, http.StatusCreated, "")
	if _, templateID, _, _ := decodeRun(t, resp); templateID != "sdlc-headless" {
		t.Fatalf("template_id = %q, want sdlc-headless", templateID)
	}
}

func TestCreateRunHeadlessAlreadyHeadlessIsNoop(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	spec := writeSpecFile(t, env.dir)
	resp := env.post("/v1/runs", `{"template_id":"sdlc-headless","headless":true,"spec_path":"`+spec+`"}`)
	assertStatus(t, resp, http.StatusCreated, "")
	if _, templateID, _, _ := decodeRun(t, resp); templateID != "sdlc-headless" {
		t.Fatalf("template_id = %q, want sdlc-headless", templateID)
	}
}

func TestCreateRunHeadlessUnsupportedTemplate(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"demo","headless":true}`), http.StatusBadRequest, "headless is not supported for template demo")
}

func TestCreateRunIssueURLFetchesSpecContent(t *testing.T) {
	var servedDir string
	env := newParityTestEnv(t,
		func(string, string) (string, error) { return servedDir, nil },
		func(context.Context, string, string, int) (string, error) { return "# Stub Issue\n\nbody\n", nil },
	)
	servedDir = env.dir

	resp := env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"https://github.com/acme/widget/issues/42","cwd":"/tmp/ignored"}`)
	assertStatus(t, resp, http.StatusCreated, "")
	_, _, cwd, runDir := decodeRun(t, resp)
	if cwd != env.dir {
		t.Fatalf("cwd = %q, want served dir %q (request cwd ignored on issue path)", cwd, env.dir)
	}
	spec, err := os.ReadFile(filepath.Join(runDir, "inputs", "spec.md"))
	if err != nil {
		t.Fatalf("read inputs/spec.md: %v", err)
	}
	if !strings.Contains(string(spec), "Stub Issue") {
		t.Fatalf("inputs/spec.md = %q, want issue body", spec)
	}
}

func TestCreateRunIssueForOtherRepoIs400(t *testing.T) {
	other := t.TempDir()
	fetched := false
	env := newParityTestEnv(t,
		func(string, string) (string, error) { return other, nil },
		func(context.Context, string, string, int) (string, error) {
			fetched = true
			return "", errors.New("must not fetch")
		},
	)
	resp := env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"https://github.com/acme/widget/issues/42"}`)
	assertStatus(t, resp, http.StatusBadRequest, "this server serves")
	if fetched {
		t.Fatal("issue fetched for a repo this server does not serve")
	}
}

func TestCreateRunIssueUsesHomeRegistry(t *testing.T) {
	env := newParityTestEnv(t, nil, func(context.Context, string, string, int) (string, error) {
		return "# From registry\n", nil
	})
	reg, err := registry.Open(filepath.Join(os.Getenv("SHUTTLE_HOME"), "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Upsert(registry.Repo{Owner: "acme", Name: "widget", URL: "https://github.com/acme/widget", Path: env.dir}); err != nil {
		t.Fatal(err)
	}
	_ = reg.Close()

	resp := env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"https://github.com/acme/widget/issues/7"}`)
	assertStatus(t, resp, http.StatusCreated, `"issue_ref":"acme/widget#7"`)

	resp = env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"https://github.com/acme/other/issues/7"}`)
	assertStatus(t, resp, http.StatusBadRequest, "not imported")
}

func TestCreateRunIssueFetchFailureIs502(t *testing.T) {
	var servedDir string
	env := newParityTestEnv(t,
		func(string, string) (string, error) { return servedDir, nil },
		func(context.Context, string, string, int) (string, error) { return "", errors.New("gh: not logged in") },
	)
	servedDir = env.dir
	resp := env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"https://github.com/acme/widget/issues/42"}`)
	assertStatus(t, resp, http.StatusBadGateway, "fetch issue")
}

func TestCreateRunMalformedIssueURLIs400(t *testing.T) {
	env := newParityTestEnv(t, func(string, string) (string, error) { return "", nil }, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"sdlc","issue_url":"not-a-url"}`), http.StatusBadRequest, "unsupported issue url")
}

func TestCreateRunUnknownTemplateIs400(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"unknown"}`), http.StatusBadRequest, "not found")
}

func TestCreateRunDemoStillWorksWithoutSpec(t *testing.T) {
	env := newParityTestEnv(t, nil, nil)
	assertStatus(t, env.post("/v1/runs", `{"template_id":"demo"}`), http.StatusCreated, "")
}
```

Add `"errors"` and `internal/registry` to the test file imports. `newParityTestEnv` pins `SHUTTLE_HOME` so no test can reach the developer's real `~/.shuttle`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -count=1 -run 'CreateRun' -v`
Expected: FAIL — compile error on unknown `api.Options` fields `LookupRepo` / `FetchSpec`

- [ ] **Step 3: Implement** in `internal/api/server.go`

Options, server fields, and request:

```go
type Options struct {
	CWD         string
	PipelineIDs []string
	// LookupRepo and FetchSpec override the home registry and gh issue fetch.
	// Leave nil in production.
	LookupRepo func(owner, name string) (string, error)
	FetchSpec  func(ctx context.Context, owner, name string, number int) (string, error)
}

type Server struct {
	engine      *pipeline.Engine
	cwd         string
	pipelineIDs []string
	lookupRepo  func(owner, name string) (string, error)
	fetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}
```

In `NewHandler` set `lookupRepo: opts.LookupRepo, fetchSpec: opts.FetchSpec`. Add `Headless bool \`json:"headless,omitempty"\`` to `createRunRequest`.

Replace `createRun`:

```go
func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if req.TemplateID == "" {
		writeError(w, http.StatusBadRequest, errors.New("template_id is required"))
		return
	}
	if req.SpecPath != "" && !filepath.IsAbs(req.SpecPath) {
		writeError(w, http.StatusBadRequest, errors.New("spec_path must be an absolute path"))
		return
	}
	templateID, err := runinput.ResolveTemplate(req.TemplateID, req.Headless, req.IssueURL != "", req.SpecPath != "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.engine.TemplateLoader == nil {
		writeError(w, http.StatusInternalServerError, errors.New("template loader is not configured"))
		return
	}
	pl, err := s.engine.TemplateLoader(templateID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	opts := runinput.Options{FallbackCWD: req.CWD, FetchSpec: s.fetchSpec}
	if opts.FallbackCWD == "" {
		opts.FallbackCWD = s.cwd
	}
	lookup, closeLookup := s.servedRepoLookup()
	defer closeLookup()
	opts.LookupRepo = lookup
	in, err := runinput.ResolveInput(r.Context(), req.IssueURL, opts)
	if err != nil {
		writeError(w, inputErrorStatus(err), err)
		return
	}

	runID, err := s.engine.StartRun(r.Context(), pipeline.StartParams{
		TemplateID:  templateID,
		Pipeline:    pl,
		CWD:         in.CWD,
		SpecPath:    req.SpecPath,
		SpecContent: in.SpecContent,
		Agent:       req.Agent,
		Model:       req.Model,
		IssueRef:    in.IssueRef,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.engine.RunUntilBlockOrDone(r.Context(), runID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeRun(w, http.StatusCreated, runID)
}
```

Helpers (same file):

```go
// servedRepoLookup returns a repo lookup for one request that only accepts the
// repo this server was started in: the engine's config, DB, and work root all
// belong to that repo. It runs before the issue fetch, so a mismatch never
// calls gh. The home registry is opened on first use and closed by the
// returned func.
func (s *Server) servedRepoLookup() (func(owner, name string) (string, error), func()) {
	var reg *registry.Registry
	base := s.lookupRepo
	if base == nil {
		base = func(owner, name string) (string, error) {
			if reg == nil {
				if err := os.MkdirAll(home.Dir(), 0o755); err != nil {
					return "", err
				}
				opened, err := registry.Open(home.RegistryDB())
				if err != nil {
					return "", err
				}
				reg = opened
			}
			entry, err := reg.Get(owner, name)
			if err != nil {
				return "", err
			}
			return entry.Path, nil
		}
	}
	lookup := func(owner, name string) (string, error) {
		path, err := base(owner, name)
		if err != nil {
			return "", err
		}
		served := s.cwd
		if served == "" {
			if served, err = os.Getwd(); err != nil {
				return "", err
			}
		}
		if !sameDir(path, served) {
			return "", fmt.Errorf("issue repo %s/%s is imported at %s; this server serves %s (run shuttle serve from that repo)", owner, name, path, served)
		}
		return path, nil
	}
	closeFn := func() {
		if reg != nil {
			_ = reg.Close()
		}
	}
	return lookup, closeFn
}

// sameDir compares by file identity so symlinks, /var vs /private/var, and
// case-insensitive filesystems don't cause false mismatches.
func sameDir(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ai, bi)
	}
	return cleanAbs(a) == cleanAbs(b)
}

func cleanAbs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

func inputErrorStatus(err error) int {
	var fetchErr *runinput.FetchError
	if errors.As(err, &fetchErr) {
		return http.StatusBadGateway
	}
	return http.StatusBadRequest
}
```

The lookup is always set; `ResolveInput` only calls it on the issue path, so spec-only requests never open the registry. A served-repo mismatch comes back from `ResolveInput` as a plain error, which `inputErrorStatus` maps to 400.

Imports: add `"context"`, `"path/filepath"`, `internal/home`, `internal/registry`, `internal/runinput`; remove `internal/issue` (no longer used). `home.Dir()` / `home.RegistryDB()` are the same calls `openRegistry` in `internal/cli/import.go` uses.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -count=1`
Expected: PASS (new parity tests and existing recovery tests)

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go
git commit -s -m "$(cat <<'EOF'
fix(api): align POST /v1/runs with CLI create-run policy

Accept headless, require spec or issue for product templates, fetch
issue bodies into the run spec, and reject issues for a repo other
than the one being served. Issue fetch failures return 502.

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 4: API `outputs_mode` on run JSON

**Files:**
- Modify: `internal/api/server.go` (`runResponse`, `writeRun`, `runsToJSON`)
- Modify: `internal/cli/root.go` (`outputsMode` → wrapper over `pipeline.OutputsMode`)
- Modify: `internal/api/server_test.go`

**Interfaces:**
- Consumes: `pipeline.OutputsMode` (Task 0)
- Produces: `"outputs_mode,omitempty"` on list rows and on every `writeRun` response (create, get, resume, cancel, approve, reject)

- [ ] **Step 1: Write the failing test**

```go
func TestRunJSONIncludesOutputsMode(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"plan": {Status: worker.StatusSucceeded, Summary: "planned"},
	}))
	cfg := &config.Config{WorkRoot: filepath.Join(dir, ".shuttle"), Concurrency: 1, DefaultWorker: "fake", DefaultTimeout: 5 * time.Minute}
	eng := pipeline.NewEngine(s, reg, cfg)
	eng.RetryBackoff = 0
	eng.TemplateLoader = func(id string) (*pipeline.Pipeline, error) {
		return &pipeline.Pipeline{ID: id, Stages: []pipeline.Stage{{ID: "plan", Outputs: []string{"summary"}}}}, nil
	}
	handler := api.NewHandler(eng, api.Options{CWD: dir})
	env := &recoveryTestEnv{t: t, dir: dir, handler: handler, store: s, reg: reg}

	spec := writeSpecFile(t, dir)
	for template, want := range map[string]string{"sdlc": "strict", "custom": "bridge"} {
		resp := env.post("/v1/runs", `{"template_id":"`+template+`","spec_path":"`+spec+`"}`)
		assertStatus(t, resp, http.StatusCreated, `"outputs_mode":"`+want+`"`)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/runs", nil)
	listResp := httptest.NewRecorder()
	handler.ServeHTTP(listResp, listReq)
	for _, want := range []string{`"outputs_mode":"strict"`, `"outputs_mode":"bridge"`} {
		if !strings.Contains(listResp.Body.String(), want) {
			t.Fatalf("list body %s missing %s", listResp.Body.String(), want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -count=1 -run TestRunJSONIncludesOutputsMode -v`
Expected: FAIL — body missing `outputs_mode`

- [ ] **Step 3: Implement**

Add `OutputsMode string \`json:"outputs_mode,omitempty"\`` to `runResponse` and to the `runJSON` struct inside `runsToJSON`.

In `writeRun`, after loading the run:

```go
	outputsMode := ""
	if s.engine.TemplateLoader != nil {
		if pl, err := s.engine.TemplateLoader(run.TemplateID); err == nil {
			outputsMode = pipeline.OutputsMode(run.TemplateID, pl)
		}
	}
```

and set `OutputsMode: outputsMode` on `resp`.

In `runsToJSON`, reuse the existing per-template cache entry `pl` (already loaded for `current_stage`) — do **not** call `TemplateLoader` again:

```go
			OutputsMode:  pipeline.OutputsMode(run.TemplateID, pl),
```

`pipeline.OutputsMode` handles a nil `pl` (template no longer loads) by returning `""`.

CLI (`internal/cli/root.go`) — replace the body of `outputsMode`:

```go
func outputsMode(templateID string) string {
	pl, err := loadPipelineTemplate(templateID)
	if err != nil {
		return ""
	}
	return pipeline.OutputsMode(templateID, pl)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ ./internal/cli/ -count=1 && go test -tags=integration ./internal/cli/ -count=1 -run StatusShows`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/server_test.go internal/cli/root.go
git commit -s -m "$(cat <<'EOF'
feat(api): include outputs_mode on run list and detail JSON

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 5: Smoke script posts `issue_url` only

**Files:**
- Modify: `examples/scripts/smoke-test-app.sh`

Under Task 3 the script's current create body (`spec_path` + `cwd` + `issue_url`) returns 400 `use either issue_url or spec_path, not both`. The server now fetches the issue itself, and requires the test app to be imported at the path `shuttle serve` runs in.

- [ ] **Step 1: Remove the local issue pre-fetch**

Delete:
- `ISSUE_SPEC="$TEST_APP/.shuttle/smoke-issue-spec.md"` (line 21)
- the `GH_REPO=...` and `ISSUE_NUM=...` python blocks (lines 52–61)
- `mkdir -p "$TEST_APP/.shuttle"`, the `echo "Fetching issue spec..."` line, and the `gh issue view ... > "$ISSUE_SPEC"` command (lines 63–66)
- `echo "spec=$ISSUE_SPEC (gitignored under your test app)"` (line 69)

Keep `need gh` with a comment, because the server on the same machine fetches issues with `gh`:

```bash
need gh   # shuttle serve fetches the issue body with gh
```

- [ ] **Step 2: Replace the create body**

```bash
echo "== 1. create run =="
echo "(requires $TEST_APP imported: cd $TEST_APP && $SHUTTLE_BIN import repo .)"
CREATE_BODY=$(TEMPLATE="$TEMPLATE" AGENT="$AGENT" ISSUE_URL="$ISSUE_URL" python3 <<'PY'
import json, os
print(json.dumps({
    "template_id": os.environ["TEMPLATE"],
    "agent": os.environ["AGENT"],
    "issue_url": os.environ["ISSUE_URL"],
}))
PY
)
```

Replace the `CREATE=$(curl -s ...)` call so a rejected create fails cleanly instead of crashing in `json_field id`:

```bash
CREATE=$(curl -s -w '\n%{http_code}' -X POST "$BASE/v1/runs" \
  -H 'Content-Type: application/json' \
  -d "$CREATE_BODY")
CREATE_CODE="${CREATE##*$'\n'}"
CREATE="${CREATE%$'\n'*}"
if [[ "$CREATE_CODE" != "201" ]]; then
  echo "ERROR: create returned HTTP $CREATE_CODE: $(printf '%s' "$CREATE" | python3 -c "import json,sys; print(json.load(sys.stdin).get('error',''))" 2>/dev/null || printf '%s' "$CREATE")" >&2
  echo "  The issue's repo must be imported at $TEST_APP (the dir shuttle serve runs in):" >&2
  echo "  cd $TEST_APP && $SHUTTLE_BIN import repo ." >&2
  exit 1
fi
```

Leave the existing `json.tool` print and `run_dir` work-root assertion in place.

- [ ] **Step 3: Check the script parses**

Run: `bash -n examples/scripts/smoke-test-app.sh && rg -n 'ISSUE_SPEC|GH_REPO|ISSUE_NUM' examples/scripts/smoke-test-app.sh`
Expected: no syntax errors; `rg` prints nothing

- [ ] **Step 4: Manual smoke** (needs a real test app, `gh` auth, and an agent; not run in CI)

```bash
make build
examples/scripts/serve-test-app.sh            # terminal 1
make smoke-test-app ISSUE_URL=https://github.com/<owner>/<repo>/issues/<n>   # terminal 2
```

Expected: create returns 201 with `issue_ref` set and `run_dir` under `$SHUTTLE_TEST_APP/.shuttle/runs/`.

- [ ] **Step 5: Commit**

```bash
git add examples/scripts/smoke-test-app.sh
git commit -s -m "$(cat <<'EOF'
chore(scripts): let the server fetch the issue in the API smoke test

POST /v1/runs now fetches issue bodies and rejects spec_path together
with issue_url, so the local gh pre-fetch is gone.

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

### Task 6: README + full verify

**Files:**
- Modify: `README.md` (the `shuttle serve` paragraph in "Run state and logs", and the `--headless` notes)

- [ ] **Step 1: Document** — after the line ``shuttle serve` starts an unauthenticated local HTTP API on `127.0.0.1:8080`. Treat it as local-only.``, add:

```markdown
`POST /v1/runs` follows the same rules as `shuttle run`:

- `template_id` is required. `"headless": true` swaps `pr-factory` / `sdlc` to their headless templates; already-headless ids are unchanged, and templates with no headless variant are rejected.
- `pr-factory` and `sdlc` (and their headless variants) need exactly one of `spec_path` (absolute path) or `issue_url`.
- `issue_url` fetches the issue body into the run spec, for any template; it is no longer just a label. The issue's repo must be imported at the directory `shuttle serve` runs in; the request `cwd` is ignored for issue runs. Any fetch failure, including an issue that doesn't exist, returns 502 with `gh`'s error.

Run JSON includes `outputs_mode` (`strict` or `bridge`) when the template declares stage outputs.
```

Near the existing `--headless` sentence in the SDLC section, add: `--headless` on a template with no headless variant (for example a local overlay) is an error rather than a silent gated run.

- [ ] **Step 2: Full verify** (unsandboxed)

Run: `make verify`
Expected: `0 issues.`; all unit packages `ok`; integration `ok`

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -s -m "$(cat <<'EOF'
docs: document API create-run rules and outputs_mode

Assisted-by: Cursor <cursoragent@cursor.com>
EOF
)"
```

---

## Self-review (plan vs spec rev 4)

| Spec requirement | Task |
|------------------|------|
| Typed errors; CLI maps with `errors.Is`/`As`, no string rewriting | Task 1 |
| `ResolveTemplate` (neutral errors; headless no-op on `*-headless`; headless unsupported error) | Task 1 |
| CLI `--headless` on non-swappable template errors | Task 1 |
| `ResolveInput` without specPath; issue ignores FallbackCWD; not-imported + fetch errors typed | Task 2 |
| API: absolute `spec_path`; headless; `SpecContent`; per-request registry; injectors | Task 3 |
| API: served-repo rule → 400, checked in `LookupRepo` before fetch, `os.SameFile` compare | Task 3 |
| API: fetch failure (incl. nonexistent issue) → 502; malformed URL / unknown template → 400 | Task 3 |
| Lazy registry open (CLI + API); production registry path tested; `SHUTTLE_HOME` pinned in tests | Tasks 2–3 |
| CLI `resolveRunInput` drops `specPath`; fetch errors gain `fetch issue:` prefix | Task 2 |
| Primary assert `inputs/spec.md`; demo without spec still works | Task 3 |
| `outputs_mode` via shared `OutputsMode`; list cache reuse; all `writeRun` responses | Task 0 + 4 |
| Smoke script posts `issue_url` only; fails cleanly on non-201 | Task 5 |
| README (API rules, served repo, 502, `issue_url` no longer metadata-only, outputs_mode, CLI headless change) | Task 6 |
| Non-goals (OpenAPI, async, endpoints, per-repo engine, `req.CWD` hole) | Honored |
