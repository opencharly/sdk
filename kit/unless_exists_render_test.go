package kit

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// shSyntaxOK runs `sh -n` over a rendered command — the guard against the class the
// substring tests cannot see (a `;`-terminated gate wrapped around a multi-line payload
// produces a trailing `;` in command position → syntax error).
func shSyntaxOK(t *testing.T, script string) {
	t.Helper()
	c := exec.Command("sh", "-n")
	c.Stdin = strings.NewReader(script)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("rendered command is not valid sh (%v):\n%s\n---\n%s", err, out, script)
	}
}

// TestHostRenderHonoursUnlessExists pins the RCA 2026-09-26 fix: the host/machine-venue
// renderer ignored `unless_exists`, so a guarded download/command ran unconditionally on a
// host where the tool was already present. walkOp now wraps the rendered command in the same
// gate the container emitter uses (kit.WrapUnlessExists*), in the newline-terminated Block
// form the host script context needs.
func TestHostRenderHonoursUnlessExists(t *testing.T) {
	// --- a guarded download renders a command carrying the gate, and it PARSES ---
	exec1 := newFakeExec()
	step := spec.InstallStepView{
		Op: &spec.Op{Download: "https://example/get-tool.sh", Extract: "sh", UnlessExists: "/usr/bin/helm"},
	}
	if _, err := walkOp(context.Background(), exec1, step); err != nil {
		t.Fatalf("walkOp: %v", err)
	}
	if len(exec1.sysScripts) != 1 {
		t.Fatalf("want 1 RunSystem script, got %d: %v", len(exec1.sysScripts), exec1.sysScripts)
	}
	got := exec1.sysScripts[0]
	if !strings.Contains(got, "if [ -e '/usr/bin/helm' ]; then") {
		t.Fatalf("download not gated on unless_exists:\n%s", got)
	}
	if strings.Contains(got, "install -d") {
		t.Fatalf("extract=sh with empty to must not emit install -d:\n%s", got)
	}
	shSyntaxOK(t, got)

	// --- a guarded run: command uses the Block form (multi-line safe) ---
	exec2 := newFakeExec()
	step2 := spec.InstallStepView{Op: &spec.Op{Command: "echo hi", UnlessExists: "/opt/x"}}
	if _, err := walkOp(context.Background(), exec2, step2); err != nil {
		t.Fatalf("walkOp: %v", err)
	}
	if len(exec2.sysScripts) != 1 || !strings.Contains(exec2.sysScripts[0], "if [ -e '/opt/x' ]; then") {
		t.Fatalf("run: command not gated:\n%v", exec2.sysScripts)
	}
	shSyntaxOK(t, exec2.sysScripts[0])

	// --- no guard → no gate emitted (regression guard) ---
	exec3 := newFakeExec()
	step3 := spec.InstallStepView{Op: &spec.Op{Command: "echo plain"}}
	if _, err := walkOp(context.Background(), exec3, step3); err != nil {
		t.Fatalf("walkOp: %v", err)
	}
	if strings.Contains(exec3.sysScripts[0], "already present") {
		t.Fatalf("unguarded step must not emit a gate:\n%s", exec3.sysScripts[0])
	}
}
