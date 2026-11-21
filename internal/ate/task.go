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
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0} // honor ctx deadline from caller
	}

	backoff := DefaultResumeBackoff()
	var data []byte
	var statusCode int
	for attempt := 0; attempt < DefaultResumeAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return out, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("ate-target-actor", TargetHeader(c.Atespace, actor))

		resp, err := client.Do(httpReq)
		if err != nil {
			return out, fmt.Errorf("post task: %w", err)
		}
		data, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("read task response: %w", err)
		}
		statusCode = resp.StatusCode
		if IsResumeParkingHTTP(statusCode, data) {
			if log != nil {
				_, _ = fmt.Fprintf(log, "ate: actor still resuming, retry %d\n", attempt+1)
			}
			if attempt+1 >= DefaultResumeAttempts {
				// exhausted attempts, break and handle below without leaking raw 503 body
				break
			}
			if err := backoff.Wait(ctx, attempt); err != nil {
				return out, err
			}
			continue
		}
		// non-retryable or success - stop retrying
		break
	}
	// Write body to log only when it's not a retryable "still resuming" 503 response.
	if log != nil && len(data) > 0 && !(statusCode == http.StatusServiceUnavailable && IsResumeParkingHTTP(statusCode, data)) {
		_, _ = log.Write(data)
		if data[len(data)-1] != '\n' {
			_, _ = io.WriteString(log, "\n")
		}
	}
	if statusCode < 200 || statusCode >= 300 {
		return out, fmt.Errorf("task HTTP %d: %s", statusCode, truncate(string(data), 512))
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
