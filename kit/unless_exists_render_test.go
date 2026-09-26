package kit

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestHostRenderHonoursUnlessExists pins the RCA 2026-09-26 fix: the host/machine-venue
// renderer ignored `unless_exists`, so a guarded download/command ran unconditionally on a
// host where the tool was already present. walkOp now wraps the rendered command in the same
// gate the container emitter uses (kit.WrapUnlessExists*).
func TestHostRenderHonoursUnlessExists(t *testing.T) {
	// --- a guarded download renders a command carrying the gate ---
	exec := newFakeExec()
	step := spec.InstallStepView{
		Op: &spec.Op{Download: "https://example/get-tool.sh", Extract: "sh", UnlessExists: "/usr/bin/helm"},
	}
	if _, err := walkOp(context.Background(), exec, step); err != nil {
		t.Fatalf("walkOp: %v", err)
	}
	if len(exec.sysScripts) != 1 {
		t.Fatalf("want 1 RunSystem script, got %d: %v", len(exec.sysScripts), exec.sysScripts)
	}
	got := exec.sysScripts[0]
	if !strings.Contains(got, "if [ -e '/usr/bin/helm' ]; then") {
		t.Fatalf("download not gated on unless_exists:\n%s", got)
	}
	if strings.Contains(got, "install -d") {
		t.Fatalf("extract=sh with empty to must not emit install -d:\n%s", got)
	}

	// --- a guarded run: command uses the Block form (multi-line safe) ---
	exec2 := newFakeExec()
	step2 := spec.InstallStepView{Op: &spec.Op{Command: "echo hi", UnlessExists: "/opt/x"}}
	if _, err := walkOp(context.Background(), exec2, step2); err != nil {
		t.Fatalf("walkOp: %v", err)
	}
	if len(exec2.sysScripts) != 1 || !strings.Contains(exec2.sysScripts[0], "if [ -e '/opt/x' ]; then") {
		t.Fatalf("run: command not gated:\n%v", exec2.sysScripts)
	}

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
