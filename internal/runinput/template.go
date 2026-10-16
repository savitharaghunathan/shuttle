package runinput

import (
	"path/filepath"
	"strings"
)

// ResolveTemplate applies template defaulting, the headless swap, and input
// validation for a new run. It does no I/O.
func ResolveTemplate(templateID string, headless, hasIssue, hasSpec bool) (string, error) {
	if hasIssue && hasSpec {
		return "", ErrSpecAndIssue
	}

	if templateID == "" {
		if headless || hasIssue {
			templateID = "pr-factory-headless"
		} else {
			templateID = "pr-factory"
		}
	}
	if headless {
		switch {
		case templateID == "pr-factory":
			templateID = "pr-factory-headless"
		case templateID == "sdlc":
			templateID = "sdlc-headless"
		case isHeadlessTemplate(templateID):
		default:
			return "", &HeadlessUnsupportedError{TemplateID: templateID}
		}
	}

	if !hasIssue && !hasSpec {
		switch templateID {
		case "pr-factory", "pr-factory-headless":
			return "", &InputRequiredError{Product: "pr-factory"}
		case "sdlc", "sdlc-headless":
			return "", &InputRequiredError{Product: "sdlc"}
		}
	}
	return templateID, nil
}

func isHeadlessTemplate(templateID string) bool {
	base := filepath.Base(templateID)
	for _, ext := range []string{".yaml", ".yml"} {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.HasSuffix(base, "-headless")
}
