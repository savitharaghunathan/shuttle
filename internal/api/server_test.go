package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/api"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/savitharaghunathan/shuttle/internal/worker/workertest"
	"os"
	"os/exec"
)

func TestCreateRunAndGetStatus(t *testing.T) {
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
		return &pipeline.Pipeline{
			ID: id,
			Stages: []pipeline.Stage{
				{ID: "build"},
			},
		}, nil
	}

	handler := api.NewHandler(eng, api.Options{
		CWD:         dir,
		PipelineIDs: []string{"demo"},
	})

	body := bytes.NewBufferString(`{"template_id":"demo"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	createReq = createReq.WithContext(context.Background())
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	handler.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/runs status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("created run id is empty")
	}
	if created.Status != string(store.RunStatusSucceeded) {
		t.Fatalf("created status=%q want succeeded", created.Status)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/runs/"+created.ID, nil)
	getResp := httptest.NewRecorder()
	handler.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET /v1/runs/{id} status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Stages []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("run id=%q want %q", got.ID, created.ID)
	}
	if got.Status != string(store.RunStatusSucceeded) {
		t.Fatalf("run status=%q want succeeded", got.Status)
	}
	if len(got.Stages) != 1 || got.Stages[0].ID != "build" || got.Stages[0].Status != string(store.StageStatusSucceeded) {
		t.Fatalf("stages=%+v want succeeded build stage", got.Stages)
	}
}

func TestListRunsEndpoint(t *testing.T) {
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
		return &pipeline.Pipeline{
			ID: id,
			Stages: []pipeline.Stage{
				{ID: "build"},
			},
		}, nil
	}

	handler := api.NewHandler(eng, api.Options{
		CWD:         dir,
		PipelineIDs: []string{"demo"},
	})

	// create two runs
	for i := 0; i < 2; i++ {
		body := bytes.NewBufferString(`{"template_id":"demo"}`)
		createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
		createReq = createReq.WithContext(context.Background())
		createReq.Header.Set("Content-Type", "application/json")
		createResp := httptest.NewRecorder()
		handler.ServeHTTP(createResp, createReq)
		if createResp.Code != http.StatusCreated {
			t.Fatalf("POST /v1/runs status=%d body=%s", createResp.Code, createResp.Body.String())
		}
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/runs?limit=1", nil)
	getResp := httptest.NewRecorder()
	handler.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET /v1/runs status=%d body=%s", getResp.Code, getResp.Body.String())
	}

	var list []map[string]any
	if err := json.Unmarshal(getResp.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len=%d want 1", len(list))
	}
}

func TestResumeEndpoint(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	reg := worker.NewRegistry(nil)
	// initial worker fails
	reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
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
		return &pipeline.Pipeline{
			ID: id,
			Stages: []pipeline.Stage{
				{ID: "build"},
			},
		}, nil
	}

	handler := api.NewHandler(eng, api.Options{
		CWD:         dir,
		PipelineIDs: []string{"demo"},
	})

	body := bytes.NewBufferString(`{"template_id":"demo"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	createReq = createReq.WithContext(context.Background())
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	handler.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/runs status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != string(store.RunStatusFailed) {
		t.Fatalf("created status=%q want failed", created.Status)
	}

	// replace worker with succeeding implementation for resume
	reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "built"},
	}))

	resReq := httptest.NewRequest(http.MethodPost, "/v1/runs/"+created.ID+"/resume", nil)
	resResp := httptest.NewRecorder()
	handler.ServeHTTP(resResp, resReq)
	if resResp.Code != http.StatusOK {
		t.Fatalf("POST /v1/runs/{id}/resume status=%d body=%s", resResp.Code, resResp.Body.String())
	}
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resResp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != string(store.RunStatusSucceeded) {
		t.Fatalf("resume status=%q want succeeded", got.Status)
	}
}

func TestRetryEndpointWarnings(t *testing.T) {
	dir := t.TempDir()
	// initialize a git repo with uncommitted change to trigger warnings
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	// create an uncommitted file
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("retry me"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
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
		return &pipeline.Pipeline{
			ID: id,
			Stages: []pipeline.Stage{
				{ID: "build"},
			},
		}, nil
	}

	handler := api.NewHandler(eng, api.Options{
		CWD:         dir,
		PipelineIDs: []string{"demo"},
	})

	body := bytes.NewBufferString(`{"template_id":"demo","spec_path":"` + specPath + `"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/runs", body)
	createReq = createReq.WithContext(context.Background())
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	handler.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/runs status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != string(store.RunStatusFailed) {
		t.Fatalf("created status=%q want failed", created.Status)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/v1/runs/"+created.ID+"/retry", nil)
	retryResp := httptest.NewRecorder()
	handler.ServeHTTP(retryResp, retryReq)
	if retryResp.Code != http.StatusCreated {
		t.Fatalf("POST /v1/runs/{id}/retry status=%d body=%s", retryResp.Code, retryResp.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(retryResp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w, ok := got["warnings"].([]any); !ok || len(w) == 0 {
		t.Fatalf("warnings empty or missing: %v", got["warnings"])
	}
}
