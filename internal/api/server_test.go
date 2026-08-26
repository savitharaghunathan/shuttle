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
