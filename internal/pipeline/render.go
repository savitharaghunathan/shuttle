package pipeline

import (
	"bytes"
	"text/template"
)

type RenderContext struct {
	RunID          string
	StageID        string
	Cwd            string
	RunDir         string
	IssueRef       string
	Branch         string
	ShuttleBin     string
	Artifacts      map[string]string
	ArtifactBodies map[string]string
	Inputs         map[string]string
	Env            map[string]string
}

func Render(tmpl string, ctx RenderContext) (string, error) {
	t, err := template.New("prompt").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}
