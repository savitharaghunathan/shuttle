package issue

import "testing"

func TestParseRef(t *testing.T) {
	owner, repo, num, err := ParseRef("org-name/repo.name#42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "org-name" || repo != "repo.name" || num != 42 {
		t.Fatalf("got %q %q %d, want org-name repo.name 42", owner, repo, num)
	}

	// malformed
	if _, _, _, err := ParseRef("not-a-ref"); err == nil {
		t.Fatalf("expected error for malformed ref")
	}
}

func TestBranchNameIssue(t *testing.T) {
	b, err := BranchName("org/repo#7", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b != "shuttle/issue-7" {
		t.Fatalf("got %q, want shuttle/issue-7", b)
	}
}

func TestBranchNameSpecRun(t *testing.T) {
	runID := "a1b2c3d4-1111-2222-3333-444444444444"
	b, err := BranchName("", runID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b != "shuttle/run-a1b2c3d4" {
		t.Fatalf("got %q, want shuttle/run-a1b2c3d4", b)
	}
}

func TestResolveBranchSpecRetry(t *testing.T) {
	// child run references parent run which references root
	loader := func(id string) (string, string, string, error) {
		if id == "parent" {
			return "", "rootuuid11112222-3333-4444-5555-666666666666", "", nil
		}
		return "", "", "", nil
	}
	b, err := ResolveBranch("", "child", "parent", loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b != "shuttle/run-rootuuid" {
		t.Fatalf("got %q, want shuttle/run-rootuuid", b)
	}
}

func TestResolveBranchMalformedIssue(t *testing.T) {
	if _, err := ResolveBranch("badformat", "", "", nil); err == nil {
		t.Fatalf("expected error for malformed issue ref")
	}
}
