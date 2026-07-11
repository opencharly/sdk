package deploykit

import (
	"fmt"
	"path"
	"strings"

	"github.com/opencharly/sdk/buildkit"
	"github.com/opencharly/sdk/vmshared"
)

// candyHasImplicitBuild returns true if the candy has a detection file (pixi.toml,
// package.json, Cargo.toml, aur:) that would trigger a builder auto-append. Stub
// returning false — builders run via the writeCandySteps builder block.
func (g *Generator) candyHasImplicitBuild(_ CandyModel, _ *buildkit.ResolvedBox) bool {
	return false
}

// emitTasks renders a candy's tasks to b in strict order, with adjacent-coalescing
// for mkdir/link/setcap batches, parent-dir auto-insertion for copy/write, USER
// switches on user change, and implicit build-task auto-append. Returns the final
// USER (so writeCandySteps knows whether to reset the candy boundary). Relocated
// from charly core (P8); byte-identical. The `plugin` verb case is the ONLY host
// seam — dispatched through g.EmitPluginOp (the Provider registry stays core).
//
//nolint:gocyclo // task verb dispatcher in one loop managing shared runningUser/declaredDirs/index state
func (g *Generator) EmitTasks(b *strings.Builder, layer CandyModel, img *buildkit.ResolvedBox, ops []vmshared.Op, buildDir, contextRelPrefix string) (string, error) {
	initialUser := "0" // candy-boundary starting USER (root); every caller starts at root
	if len(ops) == 0 && !g.candyHasImplicitBuild(layer, img) {
		return initialUser, nil
	}

	// Clone ops and append implicit build if needed.
	tasks := make([]vmshared.Op, 0, len(ops)+1)
	tasks = append(tasks, ops...)
	hasExplicitBuild := false
	for _, t := range ops {
		if t.Build != "" {
			hasExplicitBuild = true
			break
		}
	}
	if !hasExplicitBuild && g.candyHasImplicitBuild(layer, img) {
		tasks = append(tasks, vmshared.Op{Build: "all", RunAs: "${USER}"})
	}

	// Track known mkdirs to suppress parent-dir auto-insertion for author-declared paths.
	declaredDirs := make(map[string]bool)
	for _, t := range tasks {
		if t.Mkdir != "" {
			for p := TaskSubstPath(t.Mkdir, img); p != "" && p != "/" && p != "."; p = path.Dir(p) {
				declaredDirs[p] = true
			}
		}
	}

	runningUser := initialUser
	i := 0
	for i < len(tasks) {
		t := tasks[i]
		verb, err := t.Kind()
		if err != nil {
			fmt.Fprintf(b, "# skipping task %d: %v\n", i, err)
			i++
			continue
		}

		// Resolve USER for this task. Build tasks default to ${USER}.
		userField := t.RunAs
		if verb == "build" && userField == "" {
			userField = "${USER}"
		}
		directive, _ := ResolveUserSpec(userField, img)
		if directive != runningUser {
			fmt.Fprintf(b, "USER %s\n", directive)
			runningUser = directive
		}

		if t.Comment != "" {
			b.WriteString("# " + t.Comment + "\n")
		}

		switch verb {
		case "mkdir":
			batch := []vmshared.Op{t}
			for i+1 < len(tasks) && TaskCoalescesWith(t, tasks[i+1], verb) {
				batch = append(batch, tasks[i+1])
				i++
			}
			EmitMkdirBatch(b, batch, img)

		case "copy":
			parent := ParentDirForDest(TaskSubstPath(t.To, img))
			if parent != "" && !declaredDirs[parent] && parent != img.Home && parent != "/" {
				EmitMkdirBatch(b, []vmshared.Op{{Mkdir: parent, RunAs: t.RunAs}}, img)
				declaredDirs[parent] = true
			}
			EmitCopy(b, t, layer.GetName(), img)

		case "write":
			parent := ParentDirForDest(TaskSubstPath(t.Write, img))
			if parent != "" && !declaredDirs[parent] && parent != img.Home && parent != "/" {
				EmitMkdirBatch(b, []vmshared.Op{{Mkdir: parent, RunAs: t.RunAs}}, img)
				declaredDirs[parent] = true
			}
			srcPath, err := StageInlineContent(buildDir, contextRelPrefix, layer.GetName(), t.Content)
			if err != nil {
				return runningUser, err
			}
			EmitWrite(b, t, srcPath, img)

		case "link":
			batch := []vmshared.Op{t}
			for i+1 < len(tasks) && TaskCoalescesWith(t, tasks[i+1], verb) {
				batch = append(batch, tasks[i+1])
				i++
			}
			EmitLinkBatch(b, batch, img)

		case "setcap":
			batch := []vmshared.Op{t}
			for i+1 < len(tasks) && TaskCoalescesWith(t, tasks[i+1], verb) {
				batch = append(batch, tasks[i+1])
				i++
			}
			EmitSetcapBatch(b, batch, img)

		case "download":
			if err := EmitDownload(b, t, img); err != nil {
				return runningUser, err
			}

		case "build":
			b.WriteString("# build: " + t.Build + " (handled by builder stage)\n")

		case "plugin":
			// `plugin: command` is the ONE install-task plugin verb whose act IS the
			// full EmitCmd path (an install-task RUN); rehydrate its command onto an Op
			// and emit via the SAME EmitCmd the literal command verb uses.
			if t.Plugin == "command" {
				cmdStr, _ := t.PluginInput["command"].(string)
				EmitCmd(b, vmshared.Op{Command: cmdStr, RunAs: t.RunAs, Cache: t.Cache, Env: t.Env},
					layer.GetName(), img, runningUser == "0" || runningUser == "root")
				break
			}
			// Every OTHER plugin verb dispatches through the core Provider registry
			// (the EmitPluginOp seam): a ProvisionActor act-shell is emitted via EmitCmd;
			// any other provider's OpEmit fragment is written verbatim.
			out, isScript, perr := g.EmitPluginOp(&t, img)
			if perr != nil {
				return runningUser, perr
			}
			if isScript {
				EmitCmd(b, vmshared.Op{Command: out, RunAs: t.RunAs}, layer.GetName(), img, runningUser == "0" || runningUser == "root")
			} else {
				b.WriteString(out)
				if !strings.HasSuffix(out, "\n") {
					b.WriteString("\n")
				}
			}

		default:
			fmt.Fprintf(b, "# unknown verb %q — skipping\n", verb)
		}
		i++
	}
	return runningUser, nil
}
