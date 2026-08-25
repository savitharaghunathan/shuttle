package repo

import "testing"

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		raw       string
		wantOwner string
		wantName  string
	}{
		{"https://github.com/org/repo", "org", "repo"},
		{"https://github.com/org/repo.git", "org", "repo"},
		{"git@github.com:org/repo.git", "org", "repo"},
		{"org/repo", "org", "repo"},
	}

	for _, tc := range tests {
		owner, name, err := ParseGitHubURL(tc.raw)
		if err != nil {
			t.Fatalf("ParseGitHubURL(%q): %v", tc.raw, err)
		}
		if owner != tc.wantOwner || name != tc.wantName {
			t.Fatalf("ParseGitHubURL(%q) = %q/%q, want %q/%q", tc.raw, owner, name, tc.wantOwner, tc.wantName)
		}
	}
}

func TestParseGitHubURLRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "https://gitlab.com/org/repo", "not-a-url"} {
		if _, _, err := ParseGitHubURL(raw); err == nil {
			t.Fatalf("ParseGitHubURL(%q) expected error", raw)
		}
	}
}

func TestCloneURLFromImport(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git"},
		{"git@github.com:org/repo", "git@github.com:org/repo.git"},
		{"https://github.com/org/repo", "https://github.com/org/repo.git"},
		{"https://github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"org/repo", "https://github.com/org/repo.git"},
	}
	for _, tc := range tests {
		got := CloneURLFromImport(tc.raw, "org", "repo")
		if got != tc.want {
			t.Fatalf("CloneURLFromImport(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
