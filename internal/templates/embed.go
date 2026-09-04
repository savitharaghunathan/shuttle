package templates

import (
	"embed"
	"fmt"
	"sort"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

//go:embed pr-factory.yaml pr-factory-headless.yaml sdlc.yaml sdlc-headless.yaml
var files embed.FS

var templateFiles = map[string]string{
	"pr-factory":          "pr-factory.yaml",
	"pr-factory-headless": "pr-factory-headless.yaml",
	"sdlc":                "sdlc.yaml",
	"sdlc-headless":       "sdlc-headless.yaml",
}

func Get(id string) (*pipeline.Pipeline, error) {
	path, ok := templateFiles[id]
	if !ok {
		return nil, fmt.Errorf("template %q not found", id)
	}

	data, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %q: %w", id, err)
	}
	p, err := pipeline.LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", id, err)
	}
	return p, nil
}

func IDs() []string {
	ids := make([]string, 0, len(templateFiles))
	for id := range templateFiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
