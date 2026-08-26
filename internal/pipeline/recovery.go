package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/savitharaghunathan/shuttle/internal/store"
)

var builtinWorkers = map[string]bool{"manual": true, "shell": true, "intake": true}

func (e *Engine) validateTemplate(runID string, pl *Pipeline, stages []store.StageRun) error {
	inTemplate := map[string]bool{}
	for _, st := range pl.Stages {
		inTemplate[st.ID] = true
	}
	inDB := map[string]bool{}
	for _, sr := range stages {
		inDB[sr.StageID] = true
	}
	var missingInTemplate, extraInTemplate []string
	for id := range inDB {
		if !inTemplate[id] {
			missingInTemplate = append(missingInTemplate, id)
		}
	}
	for id := range inTemplate {
		if !inDB[id] {
			extraInTemplate = append(extraInTemplate, id)
		}
	}
	if len(missingInTemplate) == 0 && len(extraInTemplate) == 0 {
		return nil
	}
	sort.Strings(missingInTemplate)
	sort.Strings(extraInTemplate)
	return fmt.Errorf(
		"template no longer matches run %s:\n  missing stages in template: %v\n  extra stages in template:   %v\nrestore the template definition, or start fresh with: shuttle retry %s",
		shortRunID(runID), missingInTemplate, extraInTemplate, shortRunID(runID))
}

func (e *Engine) validateWorkers(runID string, stages []store.StageRun) error {
	for _, sr := range stages {
		if builtinWorkers[sr.Worker] || sr.Worker == "" {
			continue
		}
		if _, ok := e.Registry.Get(sr.Worker); !ok {
			return fmt.Errorf(
				"worker %q is no longer configured for run %s\nadd it back to shuttle.yaml workers:, or retry with a different agent:\n  shuttle retry %s --agent <name>",
				sr.Worker, shortRunID(runID), shortRunID(runID))
		}
	}
	return nil
}

func shortRunID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func recoveryStamp(ts time.Time) string { return ts.UTC().Format("20060102T150405Z") }

func archiveStageArtifacts(runDir string, stageIDs []string, ts time.Time) error {
	base := filepath.Join(runDir, ".recovery", recoveryStamp(ts))
	for _, id := range stageIDs {
		dst := filepath.Join(base, id)
		sources := []string{
			filepath.Join(runDir, "logs", id+".log"),
			filepath.Join(runDir, "artifacts", id), // dir or legacy file
		}
		for _, src := range sources {
			if _, err := os.Stat(src); err != nil {
				continue // missing is fine
			}
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return fmt.Errorf("mkdir recovery %s: %w", dst, err)
			}
			target := filepath.Join(dst, filepath.Base(src))
			if err := os.Rename(src, target); err != nil {
				if err := copyTree(src, target); err != nil {
					return fmt.Errorf("archive %s: %w", src, err)
				}
				_ = os.RemoveAll(src)
			}
		}
	}
	return nil
}

// copyTree copies a file or directory tree (cross-device rename fallback).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
