package runinput

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/savitharaghunathan/shuttle/internal/issue"
	"github.com/savitharaghunathan/shuttle/internal/registry"
)

// Input is the resolved checkout and spec for a new run.
type Input struct {
	CWD         string
	SpecContent string
	IssueRef    string
}

// Options supplies the environment ResolveInput needs. LookupRepo is required
// when an issue URL is given; FetchSpec defaults to issue.FetchSpec.
type Options struct {
	FallbackCWD string
	LookupRepo  func(owner, name string) (localPath string, err error)
	FetchSpec   func(ctx context.Context, owner, name string, number int) (string, error)
}

// ResolveInput resolves where a run executes and, for issue runs, fetches the
// issue body as the run spec. Spec files are passed to StartRun by the caller.
func ResolveInput(ctx context.Context, issueURL string, opts Options) (Input, error) {
	if issueURL == "" {
		cwd := opts.FallbackCWD
		if cwd == "" {
			wd, err := os.Getwd()
			if err != nil {
				return Input{}, err
			}
			cwd = wd
		}
		return Input{CWD: cwd}, nil
	}

	owner, name, number, err := issue.ParseIssueURL(issueURL)
	if err != nil {
		return Input{}, err
	}
	if opts.LookupRepo == nil {
		return Input{}, errors.New("runinput: LookupRepo is required to resolve an issue url")
	}
	path, err := opts.LookupRepo(owner, name)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return Input{}, &RepoNotImportedError{Owner: owner, Name: name}
		}
		return Input{}, err
	}

	fetch := opts.FetchSpec
	if fetch == nil {
		fetch = issue.FetchSpec
	}
	spec, err := fetch(ctx, owner, name, number)
	if err != nil {
		return Input{}, &FetchError{Err: err}
	}
	return Input{
		CWD:         path,
		SpecContent: spec,
		IssueRef:    fmt.Sprintf("%s/%s#%d", owner, name, number),
	}, nil
}
