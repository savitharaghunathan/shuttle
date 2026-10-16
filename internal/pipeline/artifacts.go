package pipeline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxArtifactBodyBytes = 128 * 1024

const maxTestLogBodyBytes = 32 * 1024

func upstreamStageIDs(pl *Pipeline, stageID string) []string {
	if pl == nil {
		return nil
	}

	stageByID := make(map[string]Stage, len(pl.Stages))
	for _, stage := range pl.Stages {
		stageByID[stage.ID] = stage
	}

	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		stage, ok := stageByID[id]
		if !ok {
			return
		}
		for _, dep := range stage.DependsOn {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			visit(dep)
		}
	}
	visit(stageID)

	upstream := make([]string, 0, len(seen))
	for _, stage := range pl.Stages {
		if seen[stage.ID] {
			upstream = append(upstream, stage.ID)
		}
	}
	return upstream
}

func loadArtifactBodies(runDir, stageID string, pl *Pipeline) (map[string]string, error) {
	bodies := make(map[string]string)

	if err := loadWholeBody(bodies, "spec", filepath.Join(runDir, "artifacts", "spec")); err != nil {
		return nil, err
	}

	for _, dep := range upstreamStageIDs(pl, stageID) {
		if err := loadWholeBody(bodies, dep, filepath.Join(runDir, "artifacts", dep, "summary")); err != nil {
			return nil, err
		}
	}

	if stageID == "review" {
		if err := loadTailBody(bodies, "test", filepath.Join(runDir, "logs", "test.log"), maxTestLogBodyBytes); err != nil {
			return nil, err
		}
	}

	return bodies, nil
}

func loadWholeBody(bodies map[string]string, key, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat artifact %s: %w", key, err)
	}
	if info.Size() == 0 {
		return nil
	}
	if info.Size() > maxArtifactBodyBytes {
		return artifactBodyTooLarge(key)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read artifact %s: %w", key, err)
	}
	bodies[key] = string(data)
	return nil
}

func loadTailBody(bodies map[string]string, key, path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			bodies[key] = ""
			return nil
		}
		return fmt.Errorf("stat artifact %s: %w", key, err)
	}
	if info.Size() == 0 {
		bodies[key] = ""
		return nil
	}

	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	size := info.Size() - offset
	if size > maxArtifactBodyBytes {
		return artifactBodyTooLarge(key)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read artifact %s: %w", key, err)
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("read artifact %s: %w", key, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, size))
	if err != nil {
		return fmt.Errorf("read artifact %s: %w", key, err)
	}
	bodies[key] = string(data)
	return nil
}

func artifactBodyTooLarge(key string) error {
	return fmt.Errorf("artifact %s exceeds 128 KiB; shorten spec or prior stage output", key)
}
