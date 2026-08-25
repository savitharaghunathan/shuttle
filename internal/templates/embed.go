package templates

import (
	"embed"
	"fmt"

	"github.com/savitharaghunathan/shuttle/internal/pipeline"
)

//go:embed pr-factory.yaml sdlc.yaml
var files embed.FS

var templateFiles = map[string]string{
	"pr-factory": "pr-factory.yaml",
	"sdlc":       "sdlc.yaml",
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
