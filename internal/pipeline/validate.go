package pipeline

import "fmt"

func Validate(p *Pipeline) error {
	if p == nil {
		return fmt.Errorf("pipeline is nil")
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("pipeline has no stages")
	}

	ids := make(map[string]int, len(p.Stages))
	for i, stage := range p.Stages {
		if stage.ID == "" {
			return fmt.Errorf("stage at index %d has empty id", i)
		}
		if prev, ok := ids[stage.ID]; ok {
			return fmt.Errorf("duplicate stage id %q at indexes %d and %d", stage.ID, prev, i)
		}
		ids[stage.ID] = i
	}

	for _, stage := range p.Stages {
		for _, dep := range stage.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("stage %q depends on missing stage %q", stage.ID, dep)
			}
		}
	}

	inDegree := make(map[string]int, len(p.Stages))
	for id := range ids {
		inDegree[id] = 0
	}
	for _, stage := range p.Stages {
		inDegree[stage.ID] = len(stage.DependsOn)
	}

	queue := make([]string, 0, len(p.Stages))
	for _, stage := range p.Stages {
		if inDegree[stage.ID] == 0 {
			queue = append(queue, stage.ID)
		}
	}

	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++

		for _, stage := range p.Stages {
			for _, dep := range stage.DependsOn {
				if dep != id {
					continue
				}
				inDegree[stage.ID]--
				if inDegree[stage.ID] == 0 {
					queue = append(queue, stage.ID)
				}
			}
		}
	}
	if visited != len(p.Stages) {
		return fmt.Errorf("pipeline contains a cycle")
	}

	roots := 0
	for _, stage := range p.Stages {
		if len(stage.DependsOn) == 0 {
			roots++
		}
	}
	if roots == 0 {
		return fmt.Errorf("pipeline has no root stages")
	}

	return nil
}
