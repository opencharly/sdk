package loaderkit

// host_config_section.go — the PER-HOST SECTION reader (opencharly/sdk#565).
//
// # Why this exists
//
// spec#384 declared `clean:` as a typed per-host section (schema/clean.cue `#CleanSection`), and
// NO plugin-reachable loader returns it:
//
//   - `LoadDeployConfigViaExecutor` ends with `deploykit.ProjectDeployConfig`, which builds
//     `DeployConfig{Provides, Deploy, Sidecar}` — every unnamed per-host section is dropped on the
//     way out (sdk/deploykit/project_deploy.go);
//   - `LoadHostDeployConfigViaExecutor` delegates to it, so it inherits the drop;
//   - even the UNIFIED document loses the section, because the materialize's per-document merge
//     (`spec/merge.MergeUnified`, loaderkit/materialize_project.go) carries the document fields it
//     knows and not the per-host sections. The end-to-end measurement — three shapes all reporting
//     the plugin's own fallback — is in opencharly/sdk#565.
//
// This reader answers the section question directly and narrowly: one per-host document, one
// decode, the section (or nil). It is deliberately NOT a widened deploy view: teaching
// `ProjectDeployConfig` to carry per-host state would make a constructor named for the PROJECT
// deploy view a lie. opencharly/spec#383 records the per-section *permission* gap one layer up;
// this is its data half.
//
// # The contract the consumer's refusal rule depends on
//
//   - no per-host document            → (nil, nil): nothing authored, the consumer falls back (the
//     32 GiB ceiling lives in the plugin, never in the contract);
//   - a document with no `clean:`     → (nil, nil): "unset" is a legitimate answer;
//   - UNREADABLE (I/O, YAML, the bootstrap leg, an unresolvable config path) → (nil, error).
//
// The last line is the one that matters: a config that cannot be READ must never be mistaken for
// one that says NOTHING, or the consumer's "an invalid ceiling is refused" rule collapses into
// "silently use the default" — the failure mode a missing machine-scoped knob hides behind.
//
// # The decoding path is the loader's, not a second one (R2)
//
// `readConfigStack` is the very reader `LoadUnified` uses (system layer → in-dir, raw document
// merge, retired-directive stripping), the bootstrap phase runs in the same order `LoadUnified`
// runs it, and the decode target is the same `spec.UnifiedFile` that
// `loaderkit.MaterializeLoadedProject` decodes each document's reserved directives into. What is
// NOT reused is the node-form walk + merge + deploy projection — which is precisely the chain that
// loses the section.

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/spec"
	"gopkg.in/yaml.v3"
)

// LoadHostCleanSectionViaExecutor reads the per-host document's `clean:` section — the LOCAL,
// machine-scoped retention knobs of spec#384 — PLUGIN-SIDE, without going through
// deploykit.ProjectDeployConfig. It is the section-scoped counterpart of
// LoadHostDeployConfigViaExecutor, and the contract described in the file header applies: absent
// is (nil, nil), unreadable is an error.
//
// dir is the per-host config DIRECTORY (the directory holding charly.yml, i.e.
// filepath.Dir(kit.DefaultDeployConfigPath(ctx)) — the same value hostDeployConfigDir derives). An
// empty dir means "the standard per-host location"; a path that cannot be resolved is an ERROR,
// not an absent section, because the consumer must not read "I could not find the file" as "no
// ceiling was authored".
func LoadHostCleanSectionViaExecutor(ctx context.Context, ex *sdk.Executor, dir string) (*spec.CleanSection, error) {
	if ex == nil {
		return nil, fmt.Errorf("load host clean section via executor: no host reverse channel (command not compiled-in?)")
	}
	if dir == "" {
		dir = hostDeployConfigDir(ctx)
		if dir == "" {
			return nil, fmt.Errorf("load host clean section: the per-host config directory does not resolve (no user config dir for this run env)")
		}
	}
	// The loader's own document read: the config stack (system → in-dir, raw merge).
	rootData, ok, err := readConfigStack(dir)
	if err != nil {
		return nil, fmt.Errorf("load host clean section: %w", err)
	}
	if !ok {
		// No per-host document at all — nothing authored. The consumer falls back.
		return nil, nil
	}
	// The bootstrap phase, in the SAME order LoadUnified runs it (before the document is read as a
	// document): a bootstrap plugin's rewrite must reach this reader too, or the section it sees
	// could differ from the one the loader would.
	seams := LoadSeamsFromExecutor(&executorLoaderExecutor{ctx: ctx, ex: ex})
	if seams.RunBootstrapPhase != nil {
		bootstrapped, berr := seams.RunBootstrapPhase(rootData)
		if berr != nil {
			return nil, fmt.Errorf("load host clean section: bootstrap phase: %w", berr)
		}
		rootData = bootstrapped
	}
	var uf spec.UnifiedFile
	if uerr := yaml.Unmarshal(rootData, &uf); uerr != nil {
		return nil, fmt.Errorf("load host clean section: decode %s: %w", filepath.Join(dir, spec.UnifiedFileName), uerr)
	}
	return uf.Clean, nil
}
