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
  command: ["true"]
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
