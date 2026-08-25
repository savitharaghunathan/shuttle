package pipeline

func isTerminalStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "skipped", "cancelled", "rejected":
		return true
	default:
		return false
	}
}

func isReadyEligible(status string) bool {
	switch status {
	case "", "pending", "ready":
		return true
	default:
		return false
	}
}

func depsSucceeded(stage Stage, status map[string]string) bool {
	for _, dep := range stage.DependsOn {
		if status[dep] != "succeeded" {
			return false
		}
	}
	return true
}

// ReadyIDs returns stage IDs whose dependencies have all succeeded and the
// stage itself is pending or ready (not yet terminal or in-flight).
func ReadyIDs(stages []Stage, status map[string]string) []string {
	var ready []string
	for _, stage := range stages {
		st := status[stage.ID]
		if !isReadyEligible(st) || isTerminalStatus(st) {
			continue
		}
		if !depsSucceeded(stage, status) {
			continue
		}
		ready = append(ready, stage.ID)
	}
	return ready
}
