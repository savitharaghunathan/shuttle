package runinput

import (
	"errors"
	"fmt"
)

// ErrSpecAndIssue is returned when a run is given both a spec and an issue.
var ErrSpecAndIssue = errors.New("use either issue_url or spec_path, not both")

// InputRequiredError reports a product template started with no spec or issue.
type InputRequiredError struct{ Product string }

func (e *InputRequiredError) Error() string {
	return "spec_path or issue_url is required for " + e.Product
}

// HeadlessUnsupportedError reports headless requested for a template that has
// no headless variant.
type HeadlessUnsupportedError struct{ TemplateID string }

func (e *HeadlessUnsupportedError) Error() string {
	return "headless is not supported for template " + e.TemplateID + " (no headless variant)"
}

// RepoNotImportedError reports an issue whose repo is missing from the registry.
type RepoNotImportedError struct{ Owner, Name string }

func (e *RepoNotImportedError) Error() string {
	return fmt.Sprintf("repo %s/%s not imported; run: shuttle import repo https://github.com/%s/%s", e.Owner, e.Name, e.Owner, e.Name)
}

// FetchError wraps a failure to fetch an issue body. It is not a caller error.
type FetchError struct{ Err error }

func (e *FetchError) Error() string { return "fetch issue: " + e.Err.Error() }

func (e *FetchError) Unwrap() error { return e.Err }
