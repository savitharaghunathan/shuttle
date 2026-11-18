package ate

import (
	"fmt"
	"strings"
)

// Config holds Agent Substrate connection settings (from shuttle.yaml ate:).
type Config struct {
	Endpoint      string `yaml:"endpoint"`
	Atespace      string `yaml:"atespace"`
	ActorTemplate string `yaml:"actor_template"`
	TaskURL       string `yaml:"task_url"`
	TokenFile     string `yaml:"token_file,omitempty"`
	Insecure      bool   `yaml:"insecure,omitempty"`
}

// Validate reports whether remote mode can run with this config.
func (c Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.Endpoint) == "" {
		missing = append(missing, "endpoint")
	}
	if strings.TrimSpace(c.Atespace) == "" {
		missing = append(missing, "atespace")
	}
	if strings.TrimSpace(c.ActorTemplate) == "" {
		missing = append(missing, "actor_template")
	}
	if strings.TrimSpace(c.TaskURL) == "" {
		missing = append(missing, "task_url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("ate config incomplete: missing %s", strings.Join(missing, ", "))
	}
	return nil
}
