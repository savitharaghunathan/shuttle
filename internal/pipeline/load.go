package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func LoadBytes(b []byte) (*Pipeline, error) {
	var p Pipeline
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse pipeline yaml: %w", err)
	}
	return &p, nil
}
