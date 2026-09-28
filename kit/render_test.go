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
