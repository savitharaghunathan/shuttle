package issue

import "testing"

func TestParseIssueURL(t *testing.T) {
	owner, repo, num, err := ParseIssueURL("https://github.com/org/repo/issues/42")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "org" || repo != "repo" || num != 42 {
		t.Fatalf("got %s/%s #%d", owner, repo, num)
	}
}

func TestParseIssueURLRejectsInvalid(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://github.com/org/repo",
		"https://gitlab.com/org/repo/issues/1",
	} {
		if _, _, _, err := ParseIssueURL(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}
