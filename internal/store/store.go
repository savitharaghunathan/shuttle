package store

import (
	"database/sql"
	"errors"
	"fmt"
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
	ID         string
	TemplateID string
	Status     RunStatus
	CWD        string
	RunDir     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
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

	return &Store{db: db}, nil
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
INSERT INTO runs (id, template_id, status, cwd, run_dir, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.TemplateID,
		string(run.Status),
		run.CWD,
		run.RunDir,
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
SELECT id, template_id, status, cwd, run_dir, created_at, updated_at
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
