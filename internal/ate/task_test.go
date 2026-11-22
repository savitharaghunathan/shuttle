package ate_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/ate"
)

func TestRunTaskSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("ate-target-actor") != "shuttle/acme-repo-claude" {
			t.Fatalf("header=%q", r.Header.Get("ate-target-actor"))
		}
		_ = json.NewEncoder(w).Encode(ate.TaskResponse{
			Status: "succeeded", Summary: "done", Branch: "shuttle/issue-1", Commit: "abc",
		})
	}))
	defer srv.Close()

	client := &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()}
	var log strings.Builder
	resp, err := client.RunTask(context.Background(), "acme-repo-claude", ate.TaskRequest{
		Prompt: "fix", Branch: "shuttle/issue-1", RepoURL: "https://github.com/acme/repo.git",
	}, &log)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Commit != "abc" {
		t.Fatalf("commit=%q", resp.Commit)
	}
	if !strings.Contains(log.String(), "succeeded") {
		t.Fatalf("log=%q", log.String())
	}
}

func TestRunTaskHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	client := &ate.TaskClient{BaseURL: srv.URL, Atespace: "shuttle", HTTPClient: srv.Client()}
	_, err := client.RunTask(context.Background(), "a", ate.TaskRequest{Branch: "b", RepoURL: "u"}, nil)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err=%v", err)
	}
}

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
