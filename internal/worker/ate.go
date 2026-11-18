package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"github.com/savitharaghunathan/shuttle/internal/repo"
)

// RemoteAgent is the only agent supported under --remote in v1.
const RemoteAgent = "claude"

// AteAgent runs agent stages on Agent Substrate.
type AteAgent struct {
	cfg      ate.Config
	control  ate.Control
	tasks    *ate.TaskClient
	owner    string
	repoName string
	syncFn   func(workDir, branch string) error
}

// AteAgentOptions configures AteAgent.
type AteAgentOptions struct {
	Config   ate.Config
	Control  ate.Control
	Owner    string
	RepoName string
	// SyncBranch defaults to repo.FetchAndCheckoutBranch.
	SyncBranch func(workDir, branch string) error
	Tasks      *ate.TaskClient
}

// NewAteAgent builds an ate-backed worker named "claude".
func NewAteAgent(opts AteAgentOptions) (*AteAgent, error) {
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	if opts.Control == nil {
		return nil, fmt.Errorf("ate control client is required")
	}
	if strings.TrimSpace(opts.Owner) == "" || strings.TrimSpace(opts.RepoName) == "" {
		return nil, fmt.Errorf("ate worker requires repo owner and name")
	}
	tasks := opts.Tasks
	if tasks == nil {
		tasks = &ate.TaskClient{BaseURL: opts.Config.TaskURL, Atespace: opts.Config.Atespace}
	}
	syncFn := opts.SyncBranch
	if syncFn == nil {
		syncFn = repo.FetchAndCheckoutBranch
	}
	return &AteAgent{
		cfg:      opts.Config,
		control:  opts.Control,
		tasks:    tasks,
		owner:    opts.Owner,
		repoName: opts.RepoName,
		syncFn:   syncFn,
	}, nil
}

func (w *AteAgent) Name() string { return RemoteAgent }

func (w *AteAgent) Execute(ctx context.Context, req Request) (Result, error) {
	agent := req.Agent
	if agent == "" {
		agent = RemoteAgent
	}
	if agent != RemoteAgent {
		return Result{Status: StatusFailed}, fmt.Errorf("remote mode supports agent %q only, got %q", RemoteAgent, agent)
	}
	if strings.TrimSpace(req.Branch) == "" {
		return Result{Status: StatusFailed}, fmt.Errorf("remote agent requires branch")
	}
	if strings.TrimSpace(req.RepoURL) == "" {
		return Result{Status: StatusFailed}, fmt.Errorf("remote agent requires repo_url")
	}

	actorName, err := ate.ActorName(w.owner, w.repoName, RemoteAgent)
	if err != nil {
		return Result{Status: StatusFailed}, err
	}

	if _, err := ate.EnsureActor(ctx, w.control, w.cfg.Atespace, actorName, w.cfg.ActorTemplate); err != nil {
		return Result{Status: StatusFailed}, err
	}
	if _, err := w.control.ResumeActor(ctx, w.cfg.Atespace, actorName); err != nil {
		return Result{Status: StatusFailed}, fmt.Errorf("resume actor: %w", err)
	}

	var log io.Writer = req.Output
	var logFile *os.File
	if req.LogPath != "" {
		f, err := os.OpenFile(req.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return Result{Status: StatusFailed}, fmt.Errorf("open log: %w", err)
		}
		logFile = f
		defer logFile.Close()
		if log != nil {
			log = io.MultiWriter(log, logFile)
		} else {
			log = logFile
		}
	}

	taskResp, taskErr := w.tasks.RunTask(ctx, actorName, ate.TaskRequest{
		Prompt:   req.Prompt,
		Model:    req.Model,
		Branch:   req.Branch,
		RepoURL:  req.RepoURL,
		ReadOnly: req.ReadOnly,
		RunID:    req.RunID,
		StageID:  req.StageID,
	}, log)

	if taskErr != nil {
		_, _ = w.control.SuspendActor(ctx, w.cfg.Atespace, actorName)
		return Result{Status: StatusFailed, Summary: taskErr.Error()}, taskErr
	}

	if err := w.syncFn(req.WorkDir, req.Branch); err != nil {
		_, _ = w.control.SuspendActor(ctx, w.cfg.Atespace, actorName)
		return Result{Status: StatusFailed}, fmt.Errorf("sync branch %s: %w", req.Branch, err)
	}

	if _, err := w.control.SuspendActor(ctx, w.cfg.Atespace, actorName); err != nil {
		return Result{Status: StatusFailed}, fmt.Errorf("suspend actor: %w", err)
	}

	summary := taskResp.Summary
	if summary == "" {
		summary = fmt.Sprintf("ate actor %s completed on %s", actorName, req.Branch)
	}
	return Result{Status: StatusSucceeded, Summary: summary}, nil
}
