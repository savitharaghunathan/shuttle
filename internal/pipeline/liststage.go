package pipeline

import "github.com/savitharaghunathan/shuttle/internal/store"

// CurrentStage returns the human-facing "current" stage id using template order.
// stages is the DB rows for the run (any order). Returns "" when pl has no stages.
func CurrentStage(pl *Pipeline, stages []store.StageRun) string {
	if pl == nil || len(pl.Stages) == 0 {
		return ""
	}
	byID := make(map[string]store.StageRun, len(stages))
	for _, sr := range stages {
		byID[sr.StageID] = sr
	}

	// 1: first running
	for _, st := range pl.Stages {
		if byID[st.ID].Status == store.StageStatusRunning {
			return st.ID
		}
	}
	// 2: first waiting_approval
	for _, st := range pl.Stages {
		if byID[st.ID].Status == store.StageStatusWaitingApproval {
			return st.ID
		}
	}
	// 3: last failed/cancelled/rejected (in pipeline order)
	last := ""
	for _, st := range pl.Stages {
		switch byID[st.ID].Status {
		case store.StageStatusFailed, store.StageStatusCancelled, store.StageStatusRejected:
			last = st.ID
		}
	}
	if last != "" {
		return last
	}
	// 4: first skipped after the initial succeeded prefix (cancel-during-blocked)
	for _, st := range pl.Stages {
		s := byID[st.ID].Status
		if s == store.StageStatusSucceeded {
			continue
		}
		if s == store.StageStatusSkipped {
			return st.ID
		}
		break
	}
	// 5: last succeeded
	lastOk := ""
	for _, st := range pl.Stages {
		if byID[st.ID].Status == store.StageStatusSucceeded {
			lastOk = st.ID
		}
	}
	if lastOk != "" {
		return lastOk
	}
	// 6
	return "—"
}
