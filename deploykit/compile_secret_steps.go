package deploykit

// compile_secret_steps.go — the §8 secret DELIVERY compiler: a candy's `use: secret:` entries
// become FILE steps, so the value reaches its consumer as a file in the venue and never as text.
//
// Two rows hold this:
//
//	DESIGN.md:2511 (D-SEC-1) — "Secrets come only from the configured backend; they never enter
//	  the store, artifacts, labels, IR, state or output."
//	DESIGN.md:2983 — "there is no `get`: a value reaches a consumer as a `secret:` need, never as
//	  text."
//
// So the delivery is FILE→FILE and never value→env: `#SecretUse` (schema/secret.cue) carries
// `src`, a host path, and `dst`, a path in the venue, and nothing that could hold bytes. The step
// emitted here carries those two paths and a mode; `kit.walkFile` reads `src` on the OPERATOR host
// at realize and `DeployExecutor.PutFile` writes the bytes into the venue. The value therefore
// never enters the plan, the ledger, a log line or an artifact — D-SEC-1 holds by construction
// rather than by care.
//
// ONE emitter serves every substrate. `PutFile` is one method of `DeployExecutor`
// (kit/walk.go), and every deploy target is out-of-process over that same walk, so there is no
// per-substrate code and no per-substrate shape. This is the FIRST producer of a FileStep in this
// tree: before it, `spec.FileStep` was constructed only by the wire decoder (spec/install_step_view.go),
// so `kit.walkFile` had no compiled-plan caller (measured: `git -C sdk grep -c 'FileStep{' origin/main -- '*.go'` = 0,
// and the same grep over charly hits only *_test.go files).
//
// `optional: true` is DESIGN's own tolerance — DESIGN.md:735's example reads "the key is readable
// when the host had one", and its check tolerates absence. It is decided HERE, on the operator
// host, because FileStep carries no optional flag and walkFile fails hard on an unreadable source;
// a REQUIRED entry whose source is unreadable is an error, so a typo cannot silently deliver
// nothing. THE RESIDUAL RACE IS REAL AND SMALL: the source is stat'ed at compile and read at
// realize, so an optional entry whose host file disappears in between fails the deploy at realize
// rather than being skipped. Closing it needs an `optional` field on the File step (filed as a
// follow-up); deciding it here is honest and covers the case DESIGN names.

import (
	"fmt"
	"os"
	"sort"

	"github.com/opencharly/spec/spec"
)

// secretUseCarrier is the one accessor a CandyModel must expose for its authored `use:` block.
// It is not a widening of `spec.CandyReader`: `deploykit.specCandyAdapter` is the ONLY
// implementation of that interface in production (charly/layers.go wraps every candy through
// `deploykit.NewSpecCandyModel`), and the adapter is exactly the type that holds the raw
// `spec.CandyModel` — so the accessor is one method on one type, and a CandyModel that does not
// carry the authored model simply has no `use:` entries to compile.
type secretUseCarrier interface {
	CandyUse() *spec.CandyUse
}

// CompileSecretSteps lowers a candy's `use: secret:` entries into FILE steps, in the order of the
// entry NAMES so a plan is reproducible from one compile to the next.
func CompileSecretSteps(layer CandyModel) ([]InstallStep, error) {
	carrier, ok := layer.(secretUseCarrier)
	if !ok {
		return nil, nil
	}
	use := carrier.CandyUse()
	if use == nil || len(use.Secret) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(use.Secret))
	for name := range use.Secret {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []InstallStep
	for _, name := range names {
		entry := use.Secret[name]
		src, dst := string(entry.Src), string(entry.Dst)
		if src == "" || dst == "" {
			return nil, fmt.Errorf("candy %q: secret use %q must carry both src (the host file the value is read from) and dst (where it lands in the venue)", layer.GetName(), name)
		}
		if _, err := os.Stat(src); err != nil {
			if entry.Optional {
				continue // DESIGN.md:735 — "when the host had one"; absence is not a failure
			}
			return nil, fmt.Errorf("candy %q: secret use %q: the host source %q is unreadable and the entry is not `optional`: %w", layer.GetName(), name, src, err)
		}
		out = append(out, &FileStep{
			Source:    src,
			Dest:      dst,
			Mode:      0o600, // a secret delivered world-readable is not delivered
			CandyName: layer.GetName(),
		})
	}
	return out, nil
}
