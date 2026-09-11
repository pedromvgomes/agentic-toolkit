package sourcestore

import "testing"

func TestGitTransportURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"github bare host", "github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"gitlab bare host", "gitlab.com/group/repo.git", "https://gitlab.com/group/repo.git"},
		{"self-hosted bare", "git.company.io/team/proj.git", "https://git.company.io/team/proj.git"},
		{"https already", "https://github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"http already", "http://internal/repo.git", "http://internal/repo.git"},
		{"file scheme", "file:///tmp/repo.git", "file:///tmp/repo.git"},
		{"ssh scheme", "ssh://git@github.com/owner/repo.git", "ssh://git@github.com/owner/repo.git"},
		{"scp-like ssh", "git@github.com:owner/repo.git", "git@github.com:owner/repo.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitTransportURL(tc.in); got != tc.want {
				t.Errorf("gitTransportURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
