package deploykit

import (
	"strings"
	"testing"

	"github.com/opencharly/sdk/vmshared"
)

// TestEmitTmpModeGuardPinsTheFHSMode pins the emitted contract: the guard re-asserts 1777 on /tmp,
// as exactly one RUN step. Its PLACEMENT (after the perturbing candy, before the consumer, as root)
// is proven on a real generated Containerfile — see the PR body.
func TestEmitTmpModeGuardPinsTheFHSMode(t *testing.T) {
	var b strings.Builder
	(&Generator{}).EmitTmpModeGuard(&b)
	got := b.String()
	if !strings.Contains(got, "RUN chmod 1777 /tmp\n") {
		t.Fatalf("guard does not assert the FHS /tmp mode:\n%s", got)
	}
	if n := strings.Count(got, "RUN "); n != 1 {
		t.Errorf("guard emits %d RUN steps, want exactly 1:\n%s", n, got)
	}
}

// TestOpsInvokeNestedContainerEngine covers the detection that decides WHERE the guard lands. It
// reads the ops' wire form, so a mention anywhere in the op (command, description, env) trips it —
// over-matching is the safe direction: an extra idempotent chmod, never a broken image build.
func TestOpsInvokeNestedContainerEngine(t *testing.T) {
	if OpsInvokeNestedContainerEngine(nil) {
		t.Error("nil ops tripped the guard")
	}
	if OpsInvokeNestedContainerEngine([]vmshared.Op{}) {
		t.Error("empty ops tripped the guard")
	}
	quiet := []vmshared.Op{{Mkdir: "/opt/thing"}}
	if OpsInvokeNestedContainerEngine(quiet) {
		t.Errorf("a plain mkdir op tripped the guard: %+v", quiet)
	}
	for _, word := range nestedContainerEngines {
		ops := []vmshared.Op{{Description: "pre-pull the agent image with " + word + " pull"}}
		if !OpsInvokeNestedContainerEngine(ops) {
			t.Errorf("an op mentioning %q did NOT trip the guard", word)
		}
	}
}
