package worker

import (
	"fmt"

	"github.com/savitharaghunathan/shuttle/internal/ate"
	"github.com/savitharaghunathan/shuttle/internal/config"
)

// RegistryOptions customizes worker registration.
type RegistryOptions struct {
	Remote   bool
	Ate      ate.Config
	Control  ate.Control
	Owner    string
	RepoName string
}

// NewRegistryWithOptions builds a registry; when Remote is set, claude is AteAgent only.
func NewRegistryWithOptions(cfg *config.Config, opts RegistryOptions) (*Registry, error) {
	reg := NewRegistry(cfg)
	if !opts.Remote {
		return reg, nil
	}
	ateCfg := opts.Ate
	if cfg != nil && ateCfg.Endpoint == "" {
		ateCfg = cfg.Ate
	}
	if err := ateCfg.Validate(); err != nil {
		return nil, err
	}
	if opts.Control == nil {
		return nil, fmt.Errorf("remote mode requires an ate control client")
	}
	agent, err := NewAteAgent(AteAgentOptions{
		Config:   ateCfg,
		Control:  opts.Control,
		Owner:    opts.Owner,
		RepoName: opts.RepoName,
	})
	if err != nil {
		return nil, err
	}
	reg.Register(RemoteAgent, agent)
	return reg, nil
}

// ValidateRemoteAgent ensures --remote only runs with the supported agent.
func ValidateRemoteAgent(agent string) error {
	if agent == "" {
		agent = RemoteAgent
	}
	if agent != RemoteAgent {
		return fmt.Errorf("remote mode supports --agent %s only (got %q)", RemoteAgent, agent)
	}
	return nil
}
