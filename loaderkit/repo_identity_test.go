package loaderkit

import "testing"

// TestNormalizeRepoSpec covers all four spec shapes plus the "default" sentinel. Pure unit test,
// no I/O.
func TestNormalizeRepoSpec(t *testing.T) {
	cases := []struct {
		name        string
		spec        string
		wantRepo    string
		wantVersion string
	}{
		{name: "default sentinel", spec: "default",
			wantRepo: "github.com/opencharly/charly", wantVersion: ""},
		{name: "bare owner/repo", spec: "opencharly/charly",
			wantRepo: "github.com/opencharly/charly", wantVersion: ""},
		{name: "bare owner/repo @ ref", spec: "opencharly/charly@main",
			wantRepo: "github.com/opencharly/charly", wantVersion: "main"},
		{name: "host-qualified, no ref", spec: "github.com/foo/bar",
			wantRepo: "github.com/foo/bar", wantVersion: ""},
		{name: "host-qualified gitlab @ ref", spec: "gitlab.com/foo/bar@v1.0",
			wantRepo: "gitlab.com/foo/bar", wantVersion: "v1.0"},
		// Whitespace tolerance.
		{name: "leading/trailing whitespace", spec: "  opencharly/charly@main  ",
			wantRepo: "github.com/opencharly/charly", wantVersion: "main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotRepo, gotVersion := NormalizeRepoSpec(tc.spec)
			if gotRepo != tc.wantRepo || gotVersion != tc.wantVersion {
				t.Errorf("NormalizeRepoSpec(%q) = (%q, %q); want (%q, %q)",
					tc.spec, gotRepo, gotVersion, tc.wantRepo, tc.wantVersion)
			}
		})
	}
}

// TestRepoIdentity covers the repo-identity helper that drives the import-namespace cycle-break.
func TestRepoIdentity(t *testing.T) {
	cases := []struct{ ref, want string }{
		{"@github.com/o/r:v1.2.3", "github.com/o/r"},
		{"@github.com/o/r/candy/x:v1.2.3", "github.com/o/r"},
		{"@github.com/opencharly/charly:v2026.157.0650", "github.com/opencharly/charly"},
	}
	for _, c := range cases {
		if got := RepoIdentity(c.ref, "/base"); got != c.want {
			t.Errorf("RepoIdentity(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
	// A local path with no git origin yields "" (graceful degrade).
	if got := RepoIdentity("./sub", t.TempDir()); got != "" {
		t.Errorf("RepoIdentity(local, no-git) = %q, want \"\"", got)
	}
}

func TestNormalizeGitRemoteURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git@github.com:opencharly/charly.git", "github.com/opencharly/charly"},
		{"https://github.com/opencharly/charly.git", "github.com/opencharly/charly"},
		{"https://github.com/opencharly/charly", "github.com/opencharly/charly"},
		{"ssh://git@github.com/opencharly/charly.git", "github.com/opencharly/charly"},
		{"git://github.com/o/r.git", "github.com/o/r"},
		{"github.com/opencharly/charly", "github.com/opencharly/charly"},
	}
	for _, c := range cases {
		if got := NormalizeGitRemoteURL(c.in); got != c.want {
			t.Errorf("NormalizeGitRemoteURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeRepoIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"github.com/opencharly/charly", "github.com/opencharly/charly"},
		{"git@github.com:opencharly/charly.git", "github.com/opencharly/charly"},
		{"opencharly/charly", "github.com/opencharly/charly"}, // bare owner/repo → github.com
	}
	for _, c := range cases {
		if got := NormalizeRepoIdentity(c.in); got != c.want {
			t.Errorf("NormalizeRepoIdentity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
