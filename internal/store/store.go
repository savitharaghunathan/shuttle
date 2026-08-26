package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusBlocked   RunStatus = "blocked"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type StageStatus string

const (
	StageStatusPending         StageStatus = "pending"
	StageStatusReady           StageStatus = "ready"
	StageStatusRunning         StageStatus = "running"
	StageStatusSucceeded       StageStatus = "succeeded"
	StageStatusFailed          StageStatus = "failed"
	StageStatusSkipped         StageStatus = "skipped"
	StageStatusCancelled       StageStatus = "cancelled"
	StageStatusWaitingApproval StageStatus = "waiting_approval"
	StageStatusRejected        StageStatus = "rejected"
)

type Store struct {
	db *sql.DB
}

type Run struct {
	ID          string
	TemplateID  string
	Status      RunStatus
	CWD         string
	RunDir      string
	Agent       string
	Model       string
	IssueRef    string
	ParentRunID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type StageRun struct {
	ID        string
	RunID     string
	StageID   string
	Worker    string
	Status    StageStatus
	Attempt   int
	Retries   int
	TimeoutMS *int64
	Summary   *string
	LogPath   *string
}

type ListRunsOpts struct {
	Limit      int
	Statuses   []RunStatus
	Agent      string
	Model      string
	ModelUnset bool
	Template   string
	IssueRef   string
	Since      time.Time
	ParentID   string
}

var (
	ErrRunIDTooShort  = errors.New("run ID prefix too short (min 8 chars)")
	ErrRunIDNotFound  = errors.New("run not found")
	ErrRunIDAmbiguous = errors.New("ambiguous run ID prefix")
)

func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys %s: %w", dbPath, err)
	}

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite %s: %w", dbPath, err)
	}

	st := &Store{db: db}
	if err := st.migrateRuns(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate runs %s: %w", dbPath, err)
	}
	return st, nil
}

func migrateRuns(db *sql.DB) error {
	for _, col := range []string{"agent", "model", "issue_ref", "parent_run_id"} {
		_, err := db.Exec(fmt.Sprintf("ALTER TABLE runs ADD COLUMN %s TEXT NOT NULL DEFAULT ''", col))
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("add column %s: %w", col, err)
		}
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at DESC)`)
	if err != nil {
		return fmt.Errorf("create runs index: %w", err)
	}
	return nil
}

func (s *Store) migrateRuns() error {
	return migrateRuns(s.db)
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateRun(run Run) error {
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}

	_, err := s.db.Exec(`
INSERT INTO runs (id, template_id, status, cwd, run_dir, agent, model, issue_ref, parent_run_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.TemplateID,
		string(run.Status),
		run.CWD,
		run.RunDir,
		run.Agent,
		run.Model,
		run.IssueRef,
		run.ParentRunID,
		formatTime(run.CreatedAt),
		formatTime(run.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create run %s: %w", run.ID, err)
	}
	return nil
}

func (s *Store) GetRun(id string) (Run, error) {
	row := s.db.QueryRow(`
SELECT id, template_id, status, cwd, run_dir, agent, model, issue_ref, parent_run_id, created_at, updated_at
FROM runs
WHERE id = ?`, id)

	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, fmt.Errorf("get run %s: %w", id, err)
		}
		return Run{}, err
	}
	return run, nil
}

// ResolveRunID resolves a possibly-short prefix to a full run ID.
// Behavior:
// - trims and lower-cases input
// - exact match returns immediately
// - prefixes shorter than 8 return ErrRunIDTooShort
// - unique 8+ prefix returns the matching id
// - ambiguous prefix returns ErrRunIDAmbiguous
// - no matches returns ErrRunIDNotFound
func (s *Store) ResolveRunID(prefix string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(prefix))
	if p == "" {
		return "", ErrRunIDNotFound
	}
	// exact match first
	var exact string
	err := s.db.QueryRow(`SELECT id FROM runs WHERE id = ?`, p).Scan(&exact)
	if err == nil {
		return exact, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("resolve run id: %w", err)
	}
	if len(p) < 8 {
		return "", ErrRunIDTooShort
	}
	rows, err := s.db.Query(`SELECT id FROM runs WHERE id LIKE ? || '%' ORDER BY id LIMIT 2`, p)
	if err != nil {
		return "", fmt.Errorf("resolve run id: %w", err)
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", ErrRunIDNotFound
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%w: %q matches multiple runs", ErrRunIDAmbiguous, p)
	}
}

// ListRuns returns runs matching the provided filters, ordered newest first.
func (s *Store) ListRuns(opts ListRunsOpts) ([]Run, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	var where []string
	var args []any
	if len(opts.Statuses) > 0 {
		ph := make([]string, len(opts.Statuses))
		for i, st := range opts.Statuses {
			ph[i] = "?"
			args = append(args, string(st))
		}
		where = append(where, "status IN ("+strings.Join(ph, ",")+")")
	}
	if opts.Agent != "" {
		where = append(where, "agent = ?")
		args = append(args, opts.Agent)
	}
	if opts.ModelUnset {
		where = append(where, "model = ''")
	} else if opts.Model != "" {
		where = append(where, "model = ?")
		args = append(args, opts.Model)
	}
	if opts.Template != "" {
		where = append(where, "template_id = ?")
		args = append(args, opts.Template)
	}
	if opts.IssueRef != "" {
		where = append(where, "issue_ref = ?")
		args = append(args, opts.IssueRef)
	}
	if opts.ParentID != "" {
		where = append(where, "parent_run_id = ?")
		args = append(args, opts.ParentID)
	}
	if !opts.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, formatTime(opts.Since))
	}
	q := `SELECT id, template_id, status, cwd, run_dir, agent, model, issue_ref, parent_run_id, created_at, updated_at FROM runs`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) CreateStageRuns(stages []StageRun) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin create stage runs: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`
INSERT INTO stage_runs (id, run_id, stage_id, worker, status, attempt, retries, timeout_ms, summary, log_path)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare create stage runs: %w", err)
	}
	defer stmt.Close()

	for _, stage := range stages {
		if _, err = stmt.Exec(
			stage.ID,
			stage.RunID,
			stage.StageID,
			stage.Worker,
			string(stage.Status),
			stage.Attempt,
			stage.Retries,
			stage.TimeoutMS,
			stage.Summary,
			stage.LogPath,
		); err != nil {
			return fmt.Errorf("create stage run %s: %w", stage.ID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit create stage runs: %w", err)
	}
	return nil
}

func (s *Store) GetStageRuns(runID string) ([]StageRun, error) {
	rows, err := s.db.Query(`
SELECT id, run_id, stage_id, worker, status, attempt, retries, timeout_ms, summary, log_path
FROM stage_runs
WHERE run_id = ?
ORDER BY id`, runID)
	if err != nil {
		return nil, fmt.Errorf("get stage runs for run %s: %w", runID, err)
	}
	defer rows.Close()

	stages, err := scanStageRuns(rows)
	if err != nil {
		return nil, fmt.Errorf("scan stage runs for run %s: %w", runID, err)
	}
	return stages, nil
}

func (s *Store) UpdateRunStatus(id string, status RunStatus) error {
	_, err := s.db.Exec(`
UPDATE runs
SET status = ?, updated_at = ?
WHERE id = ?`, string(status), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("update run %s status: %w", id, err)
	}
	return nil
}

func (s *Store) UpdateStage(stage StageRun) error {
	_, err := s.db.Exec(`
UPDATE stage_runs
SET status = ?, attempt = ?, retries = ?, timeout_ms = ?, summary = ?, log_path = ?
WHERE id = ?`,
		string(stage.Status),
		stage.Attempt,
		stage.Retries,
		stage.TimeoutMS,
		stage.Summary,
		stage.LogPath,
		stage.ID,
	)
	if err != nil {
		return fmt.Errorf("update stage run %s: %w", stage.ID, err)
	}
	return nil
}

func (s *Store) ListWaitingApproval() ([]StageRun, error) {
	rows, err := s.db.Query(`
SELECT id, run_id, stage_id, worker, status, attempt, retries, timeout_ms, summary, log_path
FROM stage_runs
WHERE status = ?
ORDER BY run_id, id`, string(StageStatusWaitingApproval))
	if err != nil {
		return nil, fmt.Errorf("list waiting approval: %w", err)
	}
	defer rows.Close()

	stages, err := scanStageRuns(rows)
	if err != nil {
		return nil, fmt.Errorf("scan waiting approval: %w", err)
	}
	return stages, nil
}

func scanRun(row interface {
	Scan(dest ...any) error
}) (Run, error) {
	var run Run
	var status string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&run.ID,
		&run.TemplateID,
		&status,
		&run.CWD,
		&run.RunDir,
		&run.Agent,
		&run.Model,
		&run.IssueRef,
		&run.ParentRunID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Run{}, err
	}

	var err error
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Run{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Run{}, err
	}
	run.Status = RunStatus(status)

	return run, nil
}

func scanStageRuns(rows *sql.Rows) ([]StageRun, error) {
	var stages []StageRun
	for rows.Next() {
		stage, err := scanStageRun(rows)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stages, nil
}

func scanStageRun(row interface {
	Scan(dest ...any) error
}) (StageRun, error) {
	var stage StageRun
	var status string
	if err := row.Scan(
		&stage.ID,
		&stage.RunID,
		&stage.StageID,
		&stage.Worker,
		&status,
		&stage.Attempt,
		&stage.Retries,
		&stage.TimeoutMS,
		&stage.Summary,
		&stage.LogPath,
	); err != nil {
		return StageRun{}, err
	}
	stage.Status = StageStatus(status)
	return stage, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", value, err)
	}
	return t, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  template_id TEXT NOT NULL,
  status TEXT NOT NULL,
  cwd TEXT NOT NULL,
  run_dir TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS stage_runs (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id),
  stage_id TEXT NOT NULL,
  worker TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 0,
  retries INTEGER NOT NULL DEFAULT 0,
  timeout_ms INTEGER,
  summary TEXT,
  log_path TEXT,
  UNIQUE(run_id, stage_id)
);`
