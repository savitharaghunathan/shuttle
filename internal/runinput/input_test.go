package runinput

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/savitharaghunathan/shuttle/internal/registry"
)

const issueURL = "https://github.com/acme/widget/issues/42"

func lookupAt(path string) func(string, string) (string, error) {
	return func(owner, name string) (string, error) { return path, nil }
}

func fetchReturning(body string) func(context.Context, string, string, int) (string, error) {
	return func(context.Context, string, string, int) (string, error) { return body, nil }
}

func TestResolveInputSpecOnlyUsesFallbackCWD(t *testing.T) {
	in, err := ResolveInput(context.Background(), "", Options{FallbackCWD: "/served"})
	if err != nil {
		t.Fatal(err)
	}
	if in.CWD != "/served" || in.SpecContent != "" || in.IssueRef != "" {
		t.Fatalf("input = %+v, want CWD /served and no spec/issue", in)
	}
}

func TestResolveInputFetchesSpec(t *testing.T) {
	in, err := ResolveInput(context.Background(), issueURL, Options{
		FallbackCWD: "/other",
		LookupRepo:  lookupAt("/imported"),
		FetchSpec:   fetchReturning("# Title\n\nbody"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.CWD != "/imported" {
		t.Fatalf("CWD = %q, want /imported (FallbackCWD ignored on issue path)", in.CWD)
	}
	if in.SpecContent != "# Title\n\nbody" {
		t.Fatalf("SpecContent = %q", in.SpecContent)
	}
	if in.IssueRef != "acme/widget#42" {
		t.Fatalf("IssueRef = %q, want acme/widget#42", in.IssueRef)
	}
}

func TestResolveInputBadIssueURL(t *testing.T) {
	_, err := ResolveInput(context.Background(), "not-a-url", Options{LookupRepo: lookupAt("/x")})
	if err == nil || !strings.Contains(err.Error(), "unsupported issue url") {
		t.Fatalf("error = %v, want unsupported issue url", err)
	}
}

func TestResolveInputRequiresLookup(t *testing.T) {
	_, err := ResolveInput(context.Background(), issueURL, Options{})
	if err == nil {
		t.Fatal("issue url without LookupRepo succeeded, want error")
	}
}

func TestResolveInputRepoNotImported(t *testing.T) {
	_, err := ResolveInput(context.Background(), issueURL, Options{
		LookupRepo: func(string, string) (string, error) { return "", registry.ErrNotFound },
	})
	var notImported *RepoNotImportedError
	if !errors.As(err, &notImported) {
		t.Fatalf("error = %v, want *RepoNotImportedError", err)
	}
	if notImported.Owner != "acme" || notImported.Name != "widget" {
		t.Fatalf("repo = %s/%s, want acme/widget", notImported.Owner, notImported.Name)
	}
}

func TestResolveInputFetchFailure(t *testing.T) {
	cause := errors.New("gh: not logged in")
	_, err := ResolveInput(context.Background(), issueURL, Options{
		LookupRepo: lookupAt("/imported"),
		FetchSpec: func(context.Context, string, string, int) (string, error) {
			return "", cause
		},
	})
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) {
		t.Fatalf("error = %v, want *FetchError", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error %v does not unwrap to cause", err)
	}
}
