package kit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestRenderOpCommand_Verbs covers the pure op→shell render extracted from package main
// (the ONE copy the OCI build path AND the out-of-process kit.WalkPlans share).
func TestRenderOpCommand_Verbs(t *testing.T) {
	cases := []struct {
		name       string
		op         *spec.Op
		wantHandle bool
		wantSubstr string
	}{
		{"write", &spec.Op{Write: "/etc/marker", Mode: "0644", Content: "hi\n"}, true, `install -d -m0755 "/etc" && install -m0644 /dev/stdin "/etc/marker"`},
		{"mkdir", &spec.Op{Mkdir: "/opt/x"}, true, "install -d -m0755"},
		{"link", &spec.Op{Link: "/usr/local/bin/foo", Target: "/opt/foo"}, true, "ln -sfn"},
		{"command", &spec.Op{Command: "echo hi"}, true, "echo hi"},
		{"plugin:command", &spec.Op{Plugin: "command", PluginInput: map[string]any{"command": "echo two"}}, true, "echo two"},
		{"download none", &spec.Op{Download: "https://x/bin", To: "/usr/local/bin/b", Extract: "none"}, true, "curl -fL"},
		// copy is staged via PutFile, never rendered → handled=false.
		{"copy not rendered", &spec.Op{Copy: "file"}, false, ""},
		// an act-`plugin:` verb (a builtin ProvisionActor) renders via the in-proc registry,
		// not here → handled=false.
		{"act plugin not rendered", &spec.Op{Plugin: "file", PluginInput: map[string]any{"file": "/x"}}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, handled := RenderOpCommand(c.op, "", nil)
			if handled != c.wantHandle {
				t.Fatalf("handled = %v, want %v (cmd=%q)", handled, c.wantHandle, cmd)
			}
			if c.wantHandle && !strings.Contains(cmd, c.wantSubstr) {
				t.Fatalf("cmd %q missing %q", cmd, c.wantSubstr)
			}
		})
	}
}

// TestRenderEnvdBody covers the env.d body renderer the deploy walk PutFiles to the venue.
func TestRenderEnvdBody(t *testing.T) {
	body := RenderEnvdBody("mycandy", map[string]string{"FOO": "bar"}, []string{"/opt/bin"})
	if !strings.Contains(body, "export FOO=bar") {
		t.Errorf("missing env export: %s", body)
	}
	if !strings.Contains(body, `export PATH="/opt/bin:$PATH"`) {
		t.Errorf("missing PATH prepend: %s", body)
	}
}

// TestReplaceOrAppendManagedBlock covers the managed-block insert/replace the env.d
// finalizer uses (the SAME pure helper package main re-exports).
func TestReplaceOrAppendManagedBlock(t *testing.T) {
	// Append into an empty file.
	out := ReplaceOrAppendManagedBlock("", "BODY1", "")
	if !strings.Contains(out, managedBlockBegin) || !strings.Contains(out, "BODY1") {
		t.Fatalf("append: missing fence/body: %s", out)
	}
	// Replace in place (idempotent body swap).
	out2 := ReplaceOrAppendManagedBlock(out, "BODY2", "")
	if strings.Contains(out2, "BODY1") || !strings.Contains(out2, "BODY2") {
		t.Fatalf("replace: body not swapped: %s", out2)
	}
	if strings.Count(out2, managedBlockBegin) != 1 {
		t.Fatalf("replace: duplicated fence: %s", out2)
	}
}

// TestRenderDownloadScript_EmptyToNoInstallD pins the fix for RCA 2026-09-26: an
// `extract: sh` download (a self-installing script, e.g. helm's get-helm-3) carries no
// `to:`, and the host renderer emitted `install -d -m0755` with an EMPTY path — failing
// with `install: cannot create directory` on that empty path. A declared `to:` must still
// create its dir.
func TestRenderDownloadScript_EmptyToNoInstallD(t *testing.T) {
	sh := RenderDownloadScript(&spec.Op{Download: "https://example/get-tool.sh", Extract: "sh"}, nil)
	if strings.Contains(sh, "install -d") {
		t.Fatalf("extract=sh with empty to must not emit install -d; got:\n%s", sh)
	}
	if !strings.Contains(sh, `"$ovtmp/archive"`) {
		t.Fatalf("extract=sh must run the staged script; got:\n%s", sh)
	}

	withTo := RenderDownloadScript(&spec.Op{Download: "https://example/t.tgz", To: "/opt/x", Extract: "tar.gz"}, nil)
	if !strings.Contains(withTo, "install -d -m0755 /opt/x") {
		t.Fatalf("a declared to must still create its dir; got:\n%s", withTo)
	}
}

// TestRenderDownloadScript_SubstitutesCandyVarsInDestination asserts the author's OWN `var:` spelling
// works in a download DESTINATION, exactly as it already did in the URL. Without the substitution the
// destination is single-quoted and the shell never expands it, so the download lands at a path
// LITERALLY named "${VAR}" — measured on a `target: local` deploy (ledger WI-6): the file appeared as
// /home/<user>/${FC_TARBALL} and the next step then failed with "Cannot open: No such file or
// directory" for the real path.
func TestRenderDownloadScript_SubstitutesCandyVarsInDestination(t *testing.T) {
	vars := map[string]string{"FC_TARBALL": "/var/cache/charly/fc.tgz", "FC_PREFIX": "/opt/firecracker"}

	// extract: none — a fetched file at a named destination.
	fetched := RenderDownloadScript(&spec.Op{
		Download: "https://example/fc.tgz",
		To:       "${FC_TARBALL}",
		Extract:  "none",
	}, vars)

	if strings.Contains(fetched, "${FC_TARBALL}") {
		t.Fatalf("the destination kept its unexpanded ${VAR}, so the shell writes a file literally named that:\n%s", fetched)
	}

	if !strings.Contains(fetched, "install -d -m0755 /var/cache/charly") {
		t.Fatalf("the destination's directory was not created from the substituted value:\n%s", fetched)
	}

	if !strings.Contains(fetched, "-o /var/cache/charly/fc.tgz") {
		t.Fatalf("the download did not target the substituted file path:\n%s", fetched)
	}

	// An archive into a named directory, with the URL carrying a var of its own.
	archive := RenderDownloadScript(&spec.Op{
		Download: "https://example/${FC_VERSION}/x.tgz",
		To:       "${FC_PREFIX}",
		Extract:  "tar.gz",
	}, map[string]string{"FC_PREFIX": "/opt/firecracker", "FC_VERSION": "v1.17.0"})

	if strings.Contains(archive, "${FC_PREFIX}") || strings.Contains(archive, "${FC_VERSION}") {
		t.Fatalf("an archive destination or URL kept an unexpanded var:\n%s", archive)
	}

	if !strings.Contains(archive, "-C /opt/firecracker") {
		t.Fatalf("the archive did not unpack into the substituted directory:\n%s", archive)
	}

	// An INCLUDE FILTER entry carrying a var: the third field the substitution covers, and the one a
	// reader is least likely to think of — a filter that keeps its unexpanded name selects nothing, so
	// the archive extracts empty while every command in the script still "succeeds".
	filtered := RenderDownloadScript(&spec.Op{
		Download:       "https://example/x.tgz",
		To:             "${FC_PREFIX}",
		Extract:        "tar.gz",
		ExtractInclude: []string{"${FC_PREFIX}/bin/firecracker", "jailer"},
	}, vars)

	if strings.Contains(filtered, "${FC_PREFIX}") {
		t.Fatalf("an include-filter entry kept its unexpanded var, so it would match nothing:\n%s", filtered)
	}

	if !strings.Contains(filtered, "/opt/firecracker/bin/firecracker") {
		t.Fatalf("the include filter was not emitted expanded:\n%s", filtered)
	}

	if !strings.Contains(filtered, " jailer") {
		t.Fatalf("a filter entry with no var was rewritten:\n%s", filtered)
	}
}

// TestSubstituteCandyVars_LongestNameFirst is the guard on the substitution itself: a shorter var name
// must never eat the head of a longer one, or ${FC_VERSION} would rewrite the first characters of
// ${FC_VERSION_DIR} and silently send the download somewhere that does not exist.
func TestSubstituteCandyVars_LongestNameFirst(t *testing.T) {
	vars := map[string]string{"FC_VERSION": "v1.17.0", "FC_VERSION_DIR": "/opt/fc-1.17.0"}

	if got := substituteCandyVars("${FC_VERSION_DIR}/vmlinux", vars); got != "/opt/fc-1.17.0/vmlinux" {
		t.Errorf("substituteCandyVars = %q, want the longer name to win", got)
	}

	// An unknown name is left for the shell (or the caller) — this is the candy's var: map, not the
	// process environment.
	if got := substituteCandyVars("${NOT_MINE}/x", vars); got != "${NOT_MINE}/x" {
		t.Errorf("an unknown name was rewritten: %q", got)
	}

	// Nothing to do must be a no-op.
	if got := substituteCandyVars("/plain/path", vars); got != "/plain/path" {
		t.Errorf("a plain path was rewritten: %q", got)
	}
}
