package deploykit

import (
	"encoding/json"
	"strings"

	"github.com/opencharly/sdk/vmshared"
)

// tmp_mode_guard.go — the /tmp mode invariant of every image charly builds.
//
// /tmp is 1777 by FHS, and every process in the container is entitled to write there. A build step
// can still perturb it, and one has been observed to: the container-nesting candy's pre-pull of its
// agent image runs a NESTED podman, after which /tmp is left at 0755 root:root in the COMMITTED
// layer (localized in situ: 1777 before that step, 755 after). Every LATER unprivileged write to
// /tmp then fails with EACCES — layer-claude-code's installer download dies on
// `cp: cannot create regular file '/tmp/claude-code-install.sh': Permission denied`, and the
// githubrunner box's uid-1000 supervisord start hits the same thing.
//
// Two properties matter for the cure:
//
//   - It must land AT THE POINT OF PERTURBATION, not at the end of the stage. The failing consumer
//     is a LATER candy (STEP 70) than the flipper (STEP 64) in the same stage, so an end-of-stage
//     re-assert would run after the damage is already done. Hence: after any candy whose steps
//     invoke a nested engine, before the next candy.
//   - It must run as ROOT. An unprivileged chmod on a root-owned directory fails, so the emitter
//     brackets the guard with the same USER switching it uses around its own root-only work.
//
// The cure is the INVARIANT, not the incident: no candy has to know another candy's side effects,
// and the rule keeps holding for the next step that does the same thing.

// nestedContainerEngines are the client binaries whose execution inside a build step creates its
// own mount namespace and may leave the image's /tmp no longer world-writable in the committed
// layer. Matching is deliberately loose (it reads the ops' wire form), because a false positive
// costs one idempotent `chmod 1777 /tmp` layer while a false negative costs a broken image build.
var nestedContainerEngines = []string{"podman", "buildah", "docker", "skopeo"}

// OpsInvokeNestedContainerEngine reports whether any of ops mentions a nested container engine.
// It reads the ops' WIRE form — the same JSON the ai.opencharly.description label carries — so it
// needs no per-verb field list and stays correct as op verbs are added.
func OpsInvokeNestedContainerEngine(ops []vmshared.Op) bool {
	if len(ops) == 0 {
		return false
	}
	raw, err := json.Marshal(ops)
	if err != nil {
		return false
	}
	text := string(raw)
	for _, word := range nestedContainerEngines {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// EmitTmpModeGuard re-asserts the FHS mode of /tmp (1777). Callers must have arranged for this to
// run as root — see emitTmpModeGuardAfterCandy in generate.go, which brackets it with USER
// switching so the emitter's own inUserMode tracking stays correct.
func (g *Generator) EmitTmpModeGuard(b *strings.Builder) {
	b.WriteString("# /tmp is 1777 by FHS — re-asserted after a step that runs a nested container engine,\n")
	b.WriteString("# which can leave it at 0755 root:root in the committed layer and break every later\n")
	b.WriteString("# unprivileged /tmp write (observed: the container-nesting pre-pull).\n")
	b.WriteString("RUN chmod 1777 /tmp\n\n")
}
