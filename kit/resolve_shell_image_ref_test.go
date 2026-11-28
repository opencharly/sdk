package kit

import "testing"

// TestResolveShellImageRefReplacesTheRecordedTag is the EXECUTED coverage this dependency
// adoption owes (opencharly/sdk#658; the R10/A2 block asked for "executed coverage of the
// changed ResolveShellImageRef path").
//
// The sdk now pins github.com/opencharly/spec v0.2026332.1206, the release whose
// ResolveShellImageRef REPLACES a recorded ref's own tag instead of appending a second one
// (opencharly/spec#454). Before that release the same call returned
// "ghcr.io/opencharly/whisper:2026.325.2318:2026.332.0936" — the invalid reference the engine
// refused, which is opencharly/charly#1323. This test fails on the pre-adoption pin and passes
// on the adopted one, through the SAME re-export every consumer calls.
func TestResolveShellImageRefReplacesTheRecordedTag(t *testing.T) {
	cases := []struct{ name, tag, want string }{
		// the defect: a fully-qualified recorded ref plus a new tag
		{"ghcr.io/opencharly/whisper:2026.325.2318", "2026.332.0936", "ghcr.io/opencharly/whisper:2026.332.0936"},
		// a bare name keeps working exactly as before
		{"ghcr.io/opencharly/whisper", "2026.332.0936", "ghcr.io/opencharly/whisper:2026.332.0936"},
		// a registry PORT is not a tag
		{"localhost:5000/opencharly/whisper:2026.325.2318", "2026.332.0936", "localhost:5000/opencharly/whisper:2026.332.0936"},
	}
	for _, c := range cases {
		if got := ResolveShellImageRef("", c.name, c.tag); got != c.want {
			t.Fatalf("ResolveShellImageRef(\"\", %q, %q) = %q, want %q (a tag REPLACES the recorded one)",
				c.name, c.tag, got, c.want)
		}
	}
}
