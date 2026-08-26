package pipeline

import (
	"fmt"
	"sort"

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
