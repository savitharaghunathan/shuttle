package pipeline

type Pipeline struct {
	ID      string  `yaml:"id"`
	Version string  `yaml:"version"`
	Stages  []Stage `yaml:"stages"`
}

type Stage struct {
	ID        string   `yaml:"id"`
	Worker    string   `yaml:"worker"`
	DependsOn []string `yaml:"depends_on"`
	Prompt    string   `yaml:"prompt"`
	Command   []string `yaml:"command"`
	Outputs   []string `yaml:"outputs"`
	Retries   int      `yaml:"retries"`
	Timeout   string   `yaml:"timeout"`
}
