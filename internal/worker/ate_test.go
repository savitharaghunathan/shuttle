package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAteAgentHappyPath(t *testing.T) {
	fake := ate.NewFakeControl()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ate.TaskResponse{Status: "succeeded", Summary: "ok", Branch: "shuttle/issue-1"})
	}))
	defer srv.Close()

	synced := false
	agent, err := worker.NewAteAgent(worker.AteAgentOptions{
		Config: ate.Config{
			Endpoint: "localhost:1", Atespace: "shuttle",
			ActorTemplate: "shuttle-claude", TaskURL: srv.URL,
		},
		Control:  fake,
		Owner:    "acme",
		RepoName: "widget",
		Tasks:    &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()},
		SyncBranch: func(workDir, branch string) error {
			synced = true
			if branch != "shuttle/issue-1" {
				t.Fatalf("branch=%s", branch)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := agent.Execute(context.Background(), worker.Request{
		Agent:   "claude",
		Prompt:  "do it",
		Branch:  "shuttle/issue-1",
		RepoURL: "https://github.com/acme/widget.git",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != worker.StatusSucceeded {
		t.Fatalf("status=%s", res.Status)
	}
	if !synced {
		t.Fatal("expected sync")
	}
	if fake.Resumes != 1 || fake.Suspends != 1 {
		t.Fatalf("resumes=%d suspends=%d", fake.Resumes, fake.Suspends)
	}
}

func TestAteAgentRejectsOtherAgent(t *testing.T) {
	fake := ate.NewFakeControl()
	agent, err := worker.NewAteAgent(worker.AteAgentOptions{
		Config: ate.Config{
			Endpoint: "localhost:1", Atespace: "shuttle",
			ActorTemplate: "shuttle-claude", TaskURL: "http://127.0.0.1:9",
		},
		Control: fake, Owner: "a", RepoName: "b",
		SyncBranch: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Execute(context.Background(), worker.Request{
		Agent: "cursor", Branch: "b", RepoURL: "https://github.com/a/b.git",
	})
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err=%v", err)
	}
}

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
	fake := ate.NewFakeControl()
	fake.ResumeErrs = []error{
		status.Error(codes.Aborted, "resume already in progress"),
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

func TestAteAgentFailsWhenSyncFails(t *testing.T) {
	fake := ate.NewFakeControl()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ate.TaskResponse{Status: "succeeded", Summary: "ok"})
	}))
	defer srv.Close()
	agent, err := worker.NewAteAgent(worker.AteAgentOptions{
		Config: ate.Config{
			Endpoint: "localhost:1", Atespace: "shuttle",
			ActorTemplate: "shuttle-claude", TaskURL: srv.URL,
		},
		Control: fake, Owner: "a", RepoName: "b",
		Tasks: &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()},
		SyncBranch: func(string, string) error {
			return context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Execute(context.Background(), worker.Request{
		Agent: "claude", Branch: "b", RepoURL: "https://github.com/a/b.git", WorkDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "sync") {
		t.Fatalf("err=%v", err)
	}
	if fake.Suspends != 1 {
		t.Fatalf("expected suspend after sync failure, got %d", fake.Suspends)
	}
}

func TestValidateRemoteAgent(t *testing.T) {
	if err := worker.ValidateRemoteAgent("claude"); err != nil {
		t.Fatal(err)
	}
	if err := worker.ValidateRemoteAgent(""); err != nil {
		t.Fatal(err)
	}
	if err := worker.ValidateRemoteAgent("cursor"); err == nil {
		t.Fatal("expected error")
	}
}
