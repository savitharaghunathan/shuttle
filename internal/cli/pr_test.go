package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

func TestPROpenExistingPRSkipsCreate(t *testing.T) {
	dir := t.TempDir()
	initRepoWithOriginURL(t, dir, "https://github.com/acme/widget.git")

	runner := &fakeGHRunner{
		replies: []ghReply{
			{out: `[{"url":"https://github.com/acme/widget/pull/11"}]`},
		},
	}

	var out, errOut bytes.Buffer
	cmd := newPROpenCommand(&out, &errOut, runner)
	cmd.SetArgs([]string{"--branch", "shuttle/issue-42", "--issue", "acme/widget#42", "--cwd", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%d want 1", len(runner.calls))
	}
	want := []string{"pr", "list", "--head", "shuttle/issue-42", "--state", "open", "-R", "acme/widget", "--json", "url"}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("call[0]=%v want %v", runner.calls[0], want)
	}
	if got := out.String(); got != "pr: https://github.com/acme/widget/pull/11\n" {
		t.Fatalf("out=%q", got)
	}
}

func TestPROpenCreatesWhenNoExistingPR(t *testing.T) {
	dir := t.TempDir()
	initRepoWithOriginURL(t, dir, "git@github.com:acme/widget.git")

	runner := &fakeGHRunner{
		replies: []ghReply{
			{out: `[]`},
			{out: "https://github.com/acme/widget/pull/12\n"},
		},
	}

	var out, errOut bytes.Buffer
	cmd := newPROpenCommand(&out, &errOut, runner)
	cmd.SetArgs(filterEmptyFlags([]string{"--branch", "shuttle/issue-42", "--issue", "", "--cwd", dir}))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%d want 2", len(runner.calls))
	}
	wantList := []string{"pr", "list", "--head", "shuttle/issue-42", "--state", "open", "-R", "acme/widget", "--json", "url"}
	wantCreate := []string{"pr", "create", "--head", "shuttle/issue-42", "-R", "acme/widget", "--fill"}
	if !reflect.DeepEqual(runner.calls[0], wantList) {
		t.Fatalf("call[0]=%v want %v", runner.calls[0], wantList)
	}
	if !reflect.DeepEqual(runner.calls[1], wantCreate) {
		t.Fatalf("call[1]=%v want %v", runner.calls[1], wantCreate)
	}
	if got := out.String(); got != "pr: https://github.com/acme/widget/pull/12\n" {
		t.Fatalf("out=%q", got)
	}
}

func TestPROpenFailsWhenPRListErrors(t *testing.T) {
	dir := t.TempDir()
	initRepoWithOriginURL(t, dir, "https://github.com/acme/widget.git")

	runner := &fakeGHRunner{
		replies: []ghReply{
			{err: errors.New("exit status 1")},
		},
	}

	var out, errOut bytes.Buffer
	cmd := newPROpenCommand(&out, &errOut, runner)
	cmd.SetArgs([]string{"--branch", "shuttle/issue-42", "--cwd", dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%d want 1", len(runner.calls))
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "check existing pull request") {
		t.Fatalf("error=%q", got)
	}
}

func TestPROpenWithIssueUsesIssueTitleAndClosesBody(t *testing.T) {
	dir := t.TempDir()
	initRepoWithOriginURL(t, dir, "https://github.com/acme/widget.git")

	runner := &fakeGHRunner{
		replies: []ghReply{
			{out: `[]`},
			{out: `{"title":"Fix race in queue worker"}`},
			{out: "https://github.com/acme/widget/pull/13\n"},
		},
	}

	var out, errOut bytes.Buffer
	cmd := newPROpenCommand(&out, &errOut, runner)
	cmd.SetArgs([]string{"--branch", "shuttle/issue-42", "--issue", "acme/widget#42", "--cwd", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls=%d want 3", len(runner.calls))
	}
	wantList := []string{"pr", "list", "--head", "shuttle/issue-42", "--state", "open", "-R", "acme/widget", "--json", "url"}
	wantIssue := []string{"issue", "view", "42", "-R", "acme/widget", "--json", "title"}
	wantCreate := []string{
		"pr", "create",
		"--head", "shuttle/issue-42",
		"-R", "acme/widget",
		"--title", "Fix race in queue worker",
		"--body", "Closes #42",
	}
	if !reflect.DeepEqual(runner.calls[0], wantList) {
		t.Fatalf("call[0]=%v want %v", runner.calls[0], wantList)
	}
	if !reflect.DeepEqual(runner.calls[1], wantIssue) {
		t.Fatalf("call[1]=%v want %v", runner.calls[1], wantIssue)
	}
	if !reflect.DeepEqual(runner.calls[2], wantCreate) {
		t.Fatalf("call[2]=%v want %v", runner.calls[2], wantCreate)
	}
}

func TestFilterEmptyFlagsDropsEmptyIssuePair(t *testing.T) {
	got := filterEmptyFlags([]string{"pr", "open", "--branch", "x", "--issue", "", "--cwd", "/tmp/repo"})
	want := []string{"pr", "open", "--branch", "x", "--cwd", "/tmp/repo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterEmptyFlags=%v want %v", got, want)
	}
}

type ghReply struct {
	out string
	err error
}

type fakeGHRunner struct {
	calls   [][]string
	replies []ghReply
}

func (f *fakeGHRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(f.calls) > len(f.replies) {
		return nil, errors.New("unexpected gh call")
	}
	reply := f.replies[len(f.calls)-1]
	return []byte(reply.out), reply.err
}

func initRepoWithOriginURL(t *testing.T, dir, originURL string) {
	t.Helper()
	repository, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if _, err := repository.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{originURL}}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
}

func TestRootRegistersPRCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	found, _, err := cmd.Find([]string{"pr"})
	if err != nil {
		t.Fatalf("Find pr: %v", err)
	}
	if found == nil || found.Name() != "pr" {
		t.Fatalf("expected pr command, got %v", found)
	}
}
