//go:build integration

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/savitharaghunathan/shuttle/internal/registry"
)

func TestRunAndStatusEmbeddedTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: shell
work_root: .shuttle
test:
  command: ["true"]
`
	if err := os.WriteFile("shuttle.yaml", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("stub feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "pr-factory", "--spec", specPath})

	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runOutput := out.String()
	if !strings.Contains(runOutput, "status: blocked") {
		t.Fatalf("run output %q missing blocked status", runOutput)
	}
	if !strings.Contains(runOutput, "shuttle approve") || !strings.Contains(runOutput, "approve_pr") {
		t.Fatalf("run output %q missing approval hint", runOutput)
	}
	runID := parseRunID(t, runOutput)

	out.Reset()
	statusCmd := NewRootCommand(&out, &errOut)
	statusCmd.SetArgs([]string{"status", runID})
	if err := statusCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	statusOutput := out.String()
	if !strings.Contains(statusOutput, "run: "+runID) {
		t.Fatalf("status output %q missing run id", statusOutput)
	}
	for _, want := range []string{"intake", "implement", "test", "approve_pr"} {
		if !strings.Contains(statusOutput, want) {
			t.Fatalf("status output %q missing stage %q", statusOutput, want)
		}
	}
}

func TestE2EDryPRFactory(t *testing.T) {
	root := moduleRoot(t)
	pipelinePath := filepath.Join(root, "pipelines", "pr-factory-dry.yaml")

	dir := t.TempDir()
	chdir(t, dir)

	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("dry pr factory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", pipelinePath, "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: blocked") {
		t.Fatalf("run output %q missing blocked status", runOutput)
	}
	for _, want := range []string{
		"stage: intake\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: approve_pr\tstatus: waiting_approval",
		"stage: open_pr\tstatus: pending",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}

	runID := parseRunID(t, runOutput)
	out.Reset()
	approveCmd := NewRootCommand(&out, &errOut)
	approveCmd.SetArgs([]string{"approve", runID, "approve_pr", "--message", "dry approved"})
	if err := approveCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	approveOutput := out.String()
	if !strings.Contains(approveOutput, "status: succeeded") {
		t.Fatalf("approve output %q missing succeeded status", approveOutput)
	}
	if !strings.Contains(approveOutput, "stage: open_pr\tstatus: succeeded") {
		t.Fatalf("approve output %q missing succeeded open_pr", approveOutput)
	}
}

func TestE2EDrySDLC(t *testing.T) {
	root := moduleRoot(t)
	pipelinePath := filepath.Join(root, "pipelines", "sdlc-dry.yaml")

	dir := t.TempDir()
	chdir(t, dir)

	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "dry sdlc\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", pipelinePath, "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: blocked") {
		t.Fatalf("run output %q missing blocked status", runOutput)
	}
	for _, want := range []string{
		"stage: plan\tstatus: succeeded",
		"stage: design\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: review\tstatus: succeeded",
		"stage: approve_ship\tstatus: waiting_approval",
		"stage: ship\tstatus: pending",
		"shuttle approve",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}

	runID := parseRunID(t, runOutput)
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "plan", "summary"), "dry plan summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "design", "summary"), "dry design summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "implement", "summary"), "dry implementation summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "review", "summary"), "dry review summary\n")
	assertRunFile(t, dir, runID, filepath.Join("logs", "test.log"), "dry tests passed\n")

	out.Reset()
	approveCmd := NewRootCommand(&out, &errOut)
	approveCmd.SetArgs([]string{"approve", runID, "approve_ship", "--message", "dry approved"})
	if err := approveCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	approveOutput := out.String()
	if !strings.Contains(approveOutput, "status: succeeded") {
		t.Fatalf("approve output %q missing succeeded status", approveOutput)
	}
	if !strings.Contains(approveOutput, "stage: ship\tstatus: succeeded") {
		t.Fatalf("approve output %q missing succeeded ship", approveOutput)
	}
}

func TestE2EDrySDLCHeadless(t *testing.T) {
	root := moduleRoot(t)
	pipelinePath := filepath.Join(root, "pipelines", "sdlc-dry-headless.yaml")

	dir := t.TempDir()
	chdir(t, dir)

	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "dry sdlc headless\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", pipelinePath, "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	for _, want := range []string{
		"stage: plan\tstatus: succeeded",
		"stage: design\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: review\tstatus: succeeded",
		"stage: ship\tstatus: succeeded",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}

	runID := parseRunID(t, runOutput)
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "plan", "summary"), "dry plan summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "design", "summary"), "dry design summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "implement", "summary"), "dry implementation summary\n")
	assertRunFile(t, dir, runID, filepath.Join("artifacts", "review", "summary"), "dry review summary\n")
	assertRunFile(t, dir, runID, filepath.Join("logs", "test.log"), "dry tests passed\n")
}

func TestE2EHeadlessSwapsSDLCTemplate(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: stub
readonly_worker: stub
work_root: .shuttle
test:
  command: ["/bin/sh", "-c", "printf 'tests passed\n'"]
ship:
  command: ["true"]
workers:
  stub:
    bin: /bin/sh
    args: ["-c", "mkdir -p \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID\" && printf '%s summary\n' \"$SHUTTLE_STAGE_ID\" > \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID/summary\" && cat \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID/summary\""]
`
	writeFile(t, "shuttle.yaml", cfg)
	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "embedded sdlc headless swap\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "sdlc", "--headless", "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "template: sdlc-headless") {
		t.Fatalf("run output %q missing sdlc-headless template", runOutput)
	}
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	for _, want := range []string{
		"plan summary",
		"design summary",
		"implement summary",
		"review summary",
		"stage: ship\tstatus: succeeded",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}
}

func TestE2ESDLCStrictEmbeddedFailsWithoutArtifact(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: quiet
readonly_worker: quiet
work_root: .shuttle
test:
  command: ["true"]
ship:
  command: ["true"]
workers:
  quiet:
    bin: /bin/sh
    args: ["-c", "true"]
`
	writeFile(t, "shuttle.yaml", cfg)
	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "strict sdlc failure\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "sdlc-headless", "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: failed") {
		t.Fatalf("run output %q missing failed status", runOutput)
	}
	if !strings.Contains(runOutput, "stage: plan\tstatus: failed") {
		t.Fatalf("run output %q missing failed plan", runOutput)
	}

	runID := parseRunID(t, runOutput)
	out.Reset()
	logsCmd := NewRootCommand(&out, &errOut)
	logsCmd.SetArgs([]string{"logs", runID, "plan"})
	if err := logsCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	logOutput := out.String()
	if !strings.Contains(logOutput, "missing declared output plan/summary") {
		t.Fatalf("logs output %q missing strict artifact failure", logOutput)
	}
}

func TestPRFactoryHeadlessRegression(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: stub
work_root: .shuttle
test:
  command: ["true"]
open_pr:
  command: ["true"]
workers:
  stub:
    bin: /bin/sh
    args: ["-c", "printf 'stub implement\n'"]
`
	writeFile(t, "shuttle.yaml", cfg)
	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "headless regression\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "--headless", "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "template: pr-factory-headless") {
		t.Fatalf("run output %q missing pr-factory-headless template", runOutput)
	}
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
}

func TestPRFactoryEmptyStdoutRegression(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: quiet
work_root: .shuttle
test:
  command: ["true"]
open_pr:
  command: ["true"]
workers:
  quiet:
    bin: /bin/sh
    args: ["-c", "true"]
`
	writeFile(t, "shuttle.yaml", cfg)
	specPath := filepath.Join(dir, "spec.md")
	writeFile(t, specPath, "empty stdout regression\n")

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "pr-factory-headless", "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	if !strings.Contains(runOutput, "stage: implement\tstatus: succeeded") {
		t.Fatalf("run output %q missing succeeded implement", runOutput)
	}
}

func TestE2ESDLCShipOpensPR(t *testing.T) {
	root := moduleRoot(t)
	realShuttle := buildShuttleBinary(t, root)

	dir := t.TempDir()
	initGitRepoWithOriginURL(t, dir, filepath.Join(t.TempDir(), "not-a-remote"))
	registerImportedRepo(t, dir, "acme", "widget")
	chdir(t, t.TempDir())

	ghLog := filepath.Join(dir, "gh.log")
	fakeBin := filepath.Join(dir, "fake-bin")
	writeFakeGH(t, fakeBin, ghLog)
	wrapper := writeShuttleWrapper(t, dir)

	t.Setenv("GH_LOG", ghLog)
	t.Setenv("REAL_SHUTTLE_BIN", realShuttle)
	t.Setenv("SHUTTLE_BIN", wrapper)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := sdlcStubConfig(`
ship:
  command: ["{{.ShuttleBin}}", "pr", "open", "--branch", "{{.Branch}}", "--issue", "{{.IssueRef}}"]
`)
	writeFile(t, filepath.Join(dir, "shuttle.yaml"), cfg)

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "sdlc-headless", "--issue", "https://github.com/acme/widget/issues/42"})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	if !strings.Contains(runOutput, "stage: ship\tstatus: succeeded") {
		t.Fatalf("run output %q missing succeeded ship", runOutput)
	}

	ghLogOutput := readFile(t, ghLog)
	for _, want := range []string{
		"issue view 42 -R acme/widget --json title,body",
		"pr list --head shuttle/issue-42 --state open -R acme/widget --json url",
		"issue view 42 -R acme/widget --json title",
		"pr create --head shuttle/issue-42 -R acme/widget --title Stub Issue --body Closes #42",
	} {
		if !strings.Contains(ghLogOutput, want) {
			t.Fatalf("gh log %q missing %q", ghLogOutput, want)
		}
	}
}

func TestE2ESDLCIssueSmoke(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithOriginURL(t, dir, filepath.Join(t.TempDir(), "not-a-remote"))
	registerImportedRepo(t, dir, "acme", "widget")
	chdir(t, t.TempDir())

	ghLog := filepath.Join(dir, "gh.log")
	fakeBin := filepath.Join(dir, "fake-bin")
	writeFakeGH(t, fakeBin, ghLog)
	t.Setenv("GH_LOG", ghLog)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := sdlcStubConfig(`
ship:
  command: ["true"]
`)
	writeFile(t, filepath.Join(dir, "shuttle.yaml"), cfg)

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "sdlc-headless", "--issue", "https://github.com/acme/widget/issues/42"})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	runID := parseRunID(t, runOutput)
	spec := readFile(t, filepath.Join(dir, ".shuttle", "runs", runID, "artifacts", "spec"))
	if !strings.Contains(spec, "# Stub Issue") || !strings.Contains(spec, "Stub issue body") {
		t.Fatalf("spec artifact %q missing issue contents", spec)
	}

	ghLogOutput := readFile(t, ghLog)
	if !strings.Contains(ghLogOutput, "issue view 42 -R acme/widget --json title,body") {
		t.Fatalf("gh log %q missing issue view", ghLogOutput)
	}
}

func TestE2ECustomAgentFromConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	cfg := `
default_worker: cursor
work_root: .shuttle
test:
  command: ["true"]
open_pr:
  command: ["true"]
workers:
  stubagent:
    bin: /bin/sh
    args: ["-c", "printf 'stub:%s' \"$1\"", "sh", "{{.Prompt}}"]
`
	if err := os.WriteFile("shuttle.yaml", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("custom agent feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	workersCmd := NewRootCommand(&out, &errOut)
	workersCmd.SetArgs([]string{"workers"})
	if err := workersCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "stubagent") {
		t.Fatalf("workers output %q missing stubagent", out.String())
	}

	out.Reset()
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{
		"run", "-t", "pr-factory-headless",
		"--spec", specPath,
		"--agent", "stubagent",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	for _, want := range []string{
		"→ stage implement (stubagent) starting...",
		"stage: intake\tstatus: succeeded",
		"stage: implement\tstatus: succeeded",
		"stage: test\tstatus: succeeded",
		"stage: open_pr\tstatus: succeeded",
	} {
		if !strings.Contains(runOutput, want) {
			t.Fatalf("run output %q missing %q", runOutput, want)
		}
	}
}

func TestE2EOpenPRIdempotentAndRetryReusesSpecBranch(t *testing.T) {
	root := moduleRoot(t)
	realShuttle := buildShuttleBinary(t, root)

	dir := t.TempDir()
	chdir(t, dir)
	initGitRepoWithOriginURL(t, dir, "https://github.com/acme/widget.git")

	ghLog := filepath.Join(dir, "gh.log")
	fakeBin := filepath.Join(dir, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	ghScript := filepath.Join(fakeBin, "gh")
	ghBody := `#!/bin/sh
echo "$@" >> "$GH_LOG"
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  echo '[{"url":"https://github.com/acme/widget/pull/99"}]'
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  echo "unexpected gh pr create" >&2
  exit 99
fi
echo "unexpected gh invocation: $*" >&2
exit 98
`
	if err := os.WriteFile(ghScript, []byte(ghBody), 0o755); err != nil {
		t.Fatal(err)
	}

	wrapper := filepath.Join(dir, "shuttle-wrapper.sh")
	wrapperBody := "#!/bin/sh\nexec \"$REAL_SHUTTLE_BIN\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GH_LOG", ghLog)
	t.Setenv("REAL_SHUTTLE_BIN", realShuttle)
	t.Setenv("SHUTTLE_BIN", wrapper)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := `
default_worker: stub
work_root: .shuttle
test:
  command: ["/bin/sh", "-c", "printf 'tests passed\n'"]
workers:
  stub:
    bin: /bin/sh
    args: ["-c", "printf 'stub implement'"]
`
	if err := os.WriteFile("shuttle.yaml", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("integration idempotent open_pr\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	runCmd := NewRootCommand(&out, &errOut)
	runCmd.SetArgs([]string{"run", "-t", "pr-factory-headless", "--spec", specPath})
	if err := runCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	runOutput := out.String()
	if !strings.Contains(runOutput, "status: succeeded") {
		t.Fatalf("run output %q missing succeeded status", runOutput)
	}
	if !strings.Contains(runOutput, "stage: open_pr\tstatus: succeeded") {
		t.Fatalf("run output %q missing open_pr success", runOutput)
	}
	parentRunID := parseRunID(t, runOutput)

	out.Reset()
	errOut.Reset()
	retryCmd := NewRootCommand(&out, &errOut)
	retryCmd.SetArgs([]string{"retry", parentRunID, "--force"})
	if err := retryCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	retryOutput := out.String()
	if !strings.Contains(retryOutput, "status: succeeded") {
		t.Fatalf("retry output %q missing succeeded status", retryOutput)
	}
	if !strings.Contains(retryOutput, "stage: open_pr\tstatus: succeeded") {
		t.Fatalf("retry output %q missing open_pr success", retryOutput)
	}
	_ = parseRetryRunID(t, retryOutput)

	ghCallsRaw, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(ghCallsRaw)), "\n")
	if len(lines) < 3 {
		t.Fatalf("gh log=%q, want at least 3 calls (run open_pr + retry preflight + retry open_pr)", string(ghCallsRaw))
	}

	var wantBranch string
	for _, line := range lines {
		if strings.Contains(line, "pr create") {
			t.Fatalf("unexpected create call in gh log line %q", line)
		}
		if !strings.Contains(line, "pr list") {
			t.Fatalf("unexpected gh log line %q", line)
		}
		branch := flagValue(t, line, "--head")
		if wantBranch == "" {
			wantBranch = branch
			continue
		}
		if branch != wantBranch {
			t.Fatalf("branch mismatch in gh calls: got %q want %q; line=%q", branch, wantBranch, line)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

func parseRunID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "run: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "run: "))
		}
	}
	t.Fatalf("run id not found in output %q", output)
	return ""
}

func parseRetryRunID(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "retry: ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "retry: "))
			if idx := strings.Index(rest, " "); idx >= 0 {
				return rest[:idx]
			}
			return rest
		}
	}
	t.Fatalf("retry run id not found in output %q", output)
	return ""
}

func flagValue(t *testing.T, line, flag string) string {
	t.Helper()
	parts := strings.Fields(line)
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == flag {
			return parts[i+1]
		}
	}
	t.Fatalf("flag %q not found in %q", flag, line)
	return ""
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertRunFile(t *testing.T, cwd, runID, relPath, want string) {
	t.Helper()
	got := readFile(t, filepath.Join(cwd, ".shuttle", "runs", runID, relPath))
	if got != want {
		t.Fatalf("%s = %q, want %q", relPath, got, want)
	}
}

func sdlcStubConfig(extra string) string {
	return `
default_worker: stub
readonly_worker: stub
work_root: .shuttle
test:
  command: ["/bin/sh", "-c", "printf 'tests passed\n'"]
workers:
  stub:
    bin: /bin/sh
    args: ["-c", "mkdir -p \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID\" && printf '%s summary\n' \"$SHUTTLE_STAGE_ID\" > \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID/summary\" && cat \"$SHUTTLE_RUN_DIR/artifacts/$SHUTTLE_STAGE_ID/summary\""]
` + extra
}

func registerImportedRepo(t *testing.T, path, owner, name string) {
	t.Helper()
	shuttleHome := t.TempDir()
	t.Setenv("SHUTTLE_HOME", shuttleHome)

	reg, err := registry.Open(filepath.Join(shuttleHome, "registry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	if err := reg.Upsert(registry.Repo{
		Owner: owner,
		Name:  name,
		URL:   "https://github.com/" + owner + "/" + name,
		Path:  path,
	}); err != nil {
		t.Fatal(err)
	}
}

func writeFakeGH(t *testing.T, fakeBin, ghLog string) {
	t.Helper()
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
echo "$@" >> "$GH_LOG"
if [ "$1" = "issue" ] && [ "$2" = "view" ]; then
  case "$*" in
    *"title,body"*) echo '{"title":"Stub Issue","body":"Stub issue body"}' ;;
    *) echo '{"title":"Stub Issue"}' ;;
  esac
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  echo '[]'
  exit 0
fi
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  echo 'https://github.com/acme/widget/pull/42'
  exit 0
fi
echo "unexpected gh invocation: $*" >&2
exit 98
`
	if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = ghLog
}

func writeShuttleWrapper(t *testing.T, dir string) string {
	t.Helper()
	wrapper := filepath.Join(dir, "shuttle-wrapper.sh")
	body := "#!/bin/sh\nexec \"$REAL_SHUTTLE_BIN\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func buildShuttleBinary(t *testing.T, root string) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "shuttle")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/shuttle")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build shuttle binary: %v\n%s", err, string(out))
	}
	return binPath
}

func initGitRepoWithOriginURL(t *testing.T, dir, originURL string) {
	t.Helper()
	repository, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if _, err := repository.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{originURL}}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
}
