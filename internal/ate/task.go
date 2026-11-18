package ate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TaskRequest is posted to the actor HTTP task API.
type TaskRequest struct {
	Prompt   string `json:"prompt"`
	Model    string `json:"model,omitempty"`
	Branch   string `json:"branch"`
	RepoURL  string `json:"repo_url"`
	ReadOnly bool   `json:"readonly,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	StageID  string `json:"stage_id,omitempty"`
}

// TaskResponse is returned by the actor when a task completes.
type TaskResponse struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Commit  string `json:"commit,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

// TaskClient posts tasks to an actor over HTTP.
type TaskClient struct {
	BaseURL    string
	Atespace   string
	HTTPClient *http.Client
}

// RunTask POSTs /v1/tasks and streams the response body to log while decoding JSON.
func (c *TaskClient) RunTask(ctx context.Context, actor string, req TaskRequest, log io.Writer) (TaskResponse, error) {
	var out TaskResponse
	if strings.TrimSpace(c.BaseURL) == "" {
		return out, fmt.Errorf("ate task_url is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/tasks"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("ate-target-actor", TargetHeader(c.Atespace, actor))

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0} // honor ctx deadline from caller
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("post task: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, fmt.Errorf("read task response: %w", err)
	}
	if log != nil && len(data) > 0 {
		_, _ = log.Write(data)
		if data[len(data)-1] != '\n' {
			_, _ = io.WriteString(log, "\n")
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("task HTTP %d: %s", resp.StatusCode, truncate(string(data), 512))
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode task response: %w", err)
	}
	if out.Status == "" {
		out.Status = "succeeded"
	}
	if out.Status != "succeeded" {
		return out, fmt.Errorf("task status %q: %s", out.Status, out.Summary)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// DefaultHTTPClient returns an HTTP client with a generous timeout for agent work.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &http.Client{Timeout: timeout}
}
