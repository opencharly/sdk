package kit

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/spec/spec"
)

// TestEffectiveEnv_HostVarsOverlay / TestEffectiveEnv_NoHostVarsReturnsBase: relocated
// from charly/check_members_test.go (#55 decoupling cone, Batch D) — assert
// kit.Runner.EffectiveEnv()'s own behavior directly, zero charly coupling.

// TestEffectiveEnv_HostVarsOverlay: ${HOST:…} addresses overlay onto the active env in
// kit.Runner.EffectiveEnv — the single injection point that makes cross-member
// addressing work for the primary AND on:-swapped venues.
func TestEffectiveEnv_HostVarsOverlay(t *testing.T) {
	base := map[string]string{"USER": "user"}
	kr := NewRunner(RunnerConfig{
		Env:      base,
		HostVars: map[string]string{"HOST:web": "charly-web"},
	})
	env := kr.EffectiveEnv()
	if env["USER"] != "user" {
		t.Errorf("base var lost: %v", env)
	}
	if env["HOST:web"] != "charly-web" {
		t.Errorf("host var not overlaid: %v", env)
	}
	// The base env map must stay clean (copy-on-overlay).
	if _, leaked := base["HOST:web"]; leaked {
		t.Errorf("EffectiveEnv mutated the shared base Env")
	}
}

// TestEffectiveEnv_NoHostVarsReturnsBase: with no HostVars and no Scenario,
// EffectiveEnv returns the base map directly (behaviour unchanged).
func TestEffectiveEnv_NoHostVarsReturnsBase(t *testing.T) {
	base := map[string]string{"USER": "user"}
	kr := NewRunner(RunnerConfig{Env: base})
	if got := kr.EffectiveEnv(); !reflect.DeepEqual(got, base) {
		t.Errorf("EffectiveEnv = %v, want the base map %v", got, base)
	}
}

// TestProbeNeverHang_HonorsAuthorTimeout: relocated from charly's
// poll_probe_neverhang_test.go TestRunner_ProbeNeverHang_HonorsAuthorTimeout (#55
// decoupling cone, Batch D) — the per-probe ceiling is the floor (ProbeTimeout) unless the
// author declared a LONGER timeout:, which must be honored (+ a small buffer) so a
// legitimately-slow probe is never cut short.
func TestProbeNeverHang_HonorsAuthorTimeout(t *testing.T) {
	r := NewRunner(RunnerConfig{ProbeTimeout: 120 * time.Second})
	cases := []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{"no timeout → floor", "", 120 * time.Second},
		{"shorter timeout → floor", "10s", 120 * time.Second},
		{"longer timeout → honored + buffer", "5m", 5*time.Minute + 30*time.Second},
		{"unparseable → floor", "nonsense", 120 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.ProbeNeverHang(&spec.Op{Timeout: tc.timeout})
			if got != tc.want {
				t.Errorf("ProbeNeverHang(timeout=%q) = %s, want %s", tc.timeout, got, tc.want)
			}
		})
	}
	// A zero-value RunnerConfig falls back to the kit-internal defensive const.
	if got := NewRunner(RunnerConfig{}).ProbeNeverHang(&spec.Op{}); got != runnerProbeNeverHangFallback {
		t.Errorf("zero-value ProbeTimeout: got %s, want the kit fallback %s", got, runnerProbeNeverHangFallback)
	}
}

// blockingExecutor blocks RunCapture until the per-probe context is cancelled for any
// command containing blockOn (simulating a wedged `podman exec` under heavy load), and
// delegates everything else to a canned response. It HONORS the passed ctx (the
// never-hang contract), so the only thing that can unblock it is the per-probe deadline
// RunOne wraps every dispatch in.
type blockingExecutor struct {
	blockOn     string
	otherPrefix string
	otherStdout string
	otherExit   int
}

func (b *blockingExecutor) RunCapture(ctx context.Context, cmd string) (string, string, int, error) {
	if b.blockOn != "" && strings.Contains(cmd, b.blockOn) {
		<-ctx.Done() // wedged: only the per-probe deadline frees us
		return "", "blocked", 0, ctx.Err()
	}
	if strings.Contains(cmd, b.otherPrefix) {
		return b.otherStdout, "", b.otherExit, nil
	}
	return "", "no fake response for: " + cmd, 127, nil
}
func (b *blockingExecutor) Kind() string { return "fake" }

// commandVerbResolver is a minimal VerbResolver that dispatches a "command" plugin_input
// straight to exec.RunCapture — mirroring what the real compiled-in `command` verb
// (candy/plugin-command) does, so the wedge occurs INSIDE RunOne's per-probe ctx-shadow
// (the actual mechanism under test), not in test-only plumbing.
type commandVerbResolver struct{ exec Executor }

func (c *commandVerbResolver) RunVerb(ctx context.Context, op *spec.Op) (spec.CheckResult, bool) {
	cmd, _ := op.PluginInput["command"].(string)
	stdout, _, exit, err := c.exec.RunCapture(ctx, cmd)
	if err != nil {
		return spec.CheckResult{Status: StatusFail, Message: err.Error()}, true
	}
	if exit != 0 {
		return spec.CheckResult{Status: StatusFail, Message: fmt.Sprintf("exit=%d", exit)}, true
	}
	return spec.CheckResult{Status: StatusPass, Message: stdout}, true
}
func (c *commandVerbResolver) RunProvisionAct(context.Context, *spec.Op, string) (spec.CheckResult, bool) {
	return spec.CheckResult{}, false
}

// TestRunner_PerProbeNeverHang: relocated from charly's poll_probe_neverhang_test.go (#55
// decoupling cone, Batch D) — the load-robustness regression guard: a single wedged probe
// must be cancelled INDIVIDUALLY (at ProbeTimeout) and the pass must continue to the next
// probe — instead of hanging the whole pass until an outer watchdog kills the whole
// process. Driven via a tight 100ms ProbeTimeout directly on RunnerConfig (no wire change
// needed, no real waits) through the REAL r.Run(ctx, checks) path, so RunOne's own
// per-probe ctx-shadow (not test-local plumbing) is what cancels the wedge.
func TestRunner_PerProbeNeverHang(t *testing.T) {
	be := &blockingExecutor{blockOn: "WEDGEPROBE", otherPrefix: "echo healthy", otherStdout: "ok\n", otherExit: 0}
	r := NewRunner(RunnerConfig{
		Exec: be, Mode: ModeLive, Env: map[string]string{}, ProbeTimeout: 100 * time.Millisecond,
		Verbs: &commandVerbResolver{exec: be},
	})

	checks := []spec.Op{
		{Plugin: "command", PluginInput: map[string]any{"command": "WEDGEPROBE check"}}, // wedges → must be cancelled at ProbeTimeout
		{Plugin: "command", PluginInput: map[string]any{"command": "echo healthy"}},     // must still run after the wedge
	}

	done := make(chan []CheckResult, 1)
	go func() { done <- r.Run(context.Background(), checks) }()

	select {
	case results := <-done:
		if len(results) != 2 {
			t.Fatalf("want 2 results, got %d", len(results))
		}
		if results[0].Status != StatusFail {
			t.Errorf("wedged probe: want StatusFail (cancelled at per-probe deadline), got %v (%s)", results[0].Status, results[0].Message)
		}
		if results[1].Status != StatusPass {
			t.Errorf("probe after the wedge: want StatusPass (pass continued), got %v (%s)", results[1].Status, results[1].Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("r.Run hung on the wedged probe — per-probe never-hang not enforced (the whole-pass-guillotine regression)")
	}
}
