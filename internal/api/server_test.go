package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/api"
	"github.com/savitharaghunathan/shuttle/internal/config"
	"github.com/savitharaghunathan/shuttle/internal/pipeline"
	"github.com/savitharaghunathan/shuttle/internal/registry"
	"github.com/savitharaghunathan/shuttle/internal/store"
	"github.com/savitharaghunathan/shuttle/internal/worker"
	"github.com/savitharaghunathan/shuttle/internal/worker/workertest"
)

type recoveryTestEnv struct {
	t       *testing.T
	dir     string
	handler http.Handler
	store   *store.Store
	reg     *worker.Registry
}

func newRecoveryTestEnv(t *testing.T, results map[string]worker.Result) *recoveryTestEnv {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "shuttle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	reg := worker.NewRegistry(nil)
	reg.Register("fake", workertest.NewFakeWorker(results))
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

	return &recoveryTestEnv{
		t:       t,
		dir:     dir,
		handler: api.NewHandler(eng, api.Options{CWD: dir, PipelineIDs: []string{"demo"}}),
		store:   s,
		reg:     reg,
	}
}

func (e *recoveryTestEnv) post(path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var reqBody *bytes.Buffer
	if body == "" {
		reqBody = bytes.NewBuffer(nil)
	} else {
		reqBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, reqBody)
	req = req.WithContext(context.Background())
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp := httptest.NewRecorder()
	e.handler.ServeHTTP(resp, req)
	return resp
}

func (e *recoveryTestEnv) createRun(body string) (id, status string) {
	e.t.Helper()
	resp := e.post("/v1/runs", body)
	if resp.Code != http.StatusCreated {
		e.t.Fatalf("POST /v1/runs status=%d body=%s", resp.Code, resp.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		e.t.Fatal(err)
	}
	return created.ID, created.Status
}

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

func TestResumeEndpointNotFound(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	resp := env.post("/v1/runs/00000000/resume", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", resp.Code, resp.Body.String())
	}
}

func TestResumeEndpointInvalidState(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "ok"},
	})
	id, status := env.createRun(`{"template_id":"demo"}`)
	if status != string(store.RunStatusSucceeded) {
		t.Fatalf("precondition status=%q want succeeded", status)
	}
	resp := env.post("/v1/runs/"+id+"/resume", "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s want 409", resp.Code, resp.Body.String())
	}
}

func TestResumeEndpointRunIDPrefix(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	id, status := env.createRun(`{"template_id":"demo"}`)
	if status != string(store.RunStatusFailed) {
		t.Fatalf("precondition status=%q want failed", status)
	}
	env.reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "ok"},
	}))
	prefix := id
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	resp := env.post("/v1/runs/"+prefix+"/resume", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != id {
		t.Fatalf("id=%q want %q", got.ID, id)
	}
	if got.Status != string(store.RunStatusSucceeded) {
		t.Fatalf("status=%q want succeeded", got.Status)
	}
}

func TestRetryEndpointSuccess(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	specPath := filepath.Join(env.dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("retry spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentID, status := env.createRun(`{"template_id":"demo","spec_path":"` + specPath + `"}`)
	if status != string(store.RunStatusFailed) {
		t.Fatalf("precondition status=%q want failed", status)
	}
	env.reg.Register("fake", workertest.NewFakeWorker(map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "ok"},
	}))

	resp := env.post("/v1/runs/"+parentID+"/retry", `{"agent":"shell"}`)
	if resp.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var got struct {
		ID          string   `json:"id"`
		ParentRunID string   `json:"parent_run_id"`
		Status      string   `json:"status"`
		Warnings    []string `json:"warnings"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.ID == parentID {
		t.Fatalf("child id=%q should differ from parent", got.ID)
	}
	if got.ParentRunID != parentID {
		t.Fatalf("parent_run_id=%q want %q", got.ParentRunID, parentID)
	}
	if got.Status != string(store.RunStatusSucceeded) {
		t.Fatalf("status=%q want succeeded", got.Status)
	}
	child, err := env.store.GetRun(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Agent != "shell" {
		t.Fatalf("agent=%q want shell override", child.Agent)
	}
}

func TestRetryEndpointSucceededRequiresForce(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusSucceeded, Summary: "ok"},
	})
	specPath := filepath.Join(env.dir, "spec.md")
	if err := os.WriteFile(specPath, []byte("spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentID, status := env.createRun(`{"template_id":"demo","spec_path":"` + specPath + `"}`)
	if status != string(store.RunStatusSucceeded) {
		t.Fatalf("precondition status=%q want succeeded", status)
	}

	resp := env.post("/v1/runs/"+parentID+"/retry", "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("retry without force status=%d body=%s want 409", resp.Code, resp.Body.String())
	}

	forced := env.post("/v1/runs/"+parentID+"/retry", `{"force":true}`)
	if forced.Code != http.StatusCreated {
		t.Fatalf("forced retry status=%d body=%s", forced.Code, forced.Body.String())
	}
	var got struct {
		ID          string `json:"id"`
		ParentRunID string `json:"parent_run_id"`
	}
	if err := json.Unmarshal(forced.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == parentID {
		t.Fatal("forced retry must create a new run id")
	}
	if got.ParentRunID != parentID {
		t.Fatalf("parent_run_id=%q want %q", got.ParentRunID, parentID)
	}
}

func TestRetryEndpointNotFound(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	resp := env.post("/v1/runs/00000000/retry", "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404", resp.Code, resp.Body.String())
	}
}

func TestRetryEndpointMissingSpec(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	parentID, status := env.createRun(`{"template_id":"demo"}`)
	if status != string(store.RunStatusFailed) {
		t.Fatalf("precondition status=%q want failed", status)
	}
	run, err := env.store.GetRun(parentID)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(run.RunDir, "inputs", "spec.md")
	if _, err := os.Stat(specPath); !os.IsNotExist(err) {
		t.Fatalf("spec should be missing at %s", specPath)
	}

	resp := env.post("/v1/runs/"+parentID+"/retry", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s want 400", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "no spec") {
		t.Fatalf("body=%q want no spec error", resp.Body.String())
	}
}

func TestRecoveryEndpointAmbiguousPrefix(t *testing.T) {
	env := newRecoveryTestEnv(t, map[string]worker.Result{
		"build": {Status: worker.StatusFailed, Summary: "failed"},
	})
	for _, id := range []string{
		"a1b2c3d4-1111-4111-8111-000000000000",
		"a1b2c3d4-2222-4222-8222-000000000000",
	} {
		if err := env.store.CreateRun(store.Run{
			ID: id, TemplateID: "demo", Status: store.RunStatusFailed,
			CWD: env.dir, RunDir: filepath.Join(env.dir, id),
		}); err != nil {
			t.Fatal(err)
		}
	}
	resp := env.post("/v1/runs/a1b2c3d4/resume", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s want 400 ambiguous prefix", resp.Code, resp.Body.String())
	}
}

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
