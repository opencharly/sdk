package kit

import (
	"os"
	"strings"
)

// checkvars_charlybin.go — CHARLY_BIN stamping for a runtime check-var resolver
// (CHECK-cone move, extracted from charly's checkrun.go). Shared by BOTH the check
// runtime (a live baked-plan run) and the deploy runtime (the local `target: local`
// deploy's `--verify` plan run) — an R10 plan step referencing ${CHARLY_BIN} must
// drive the ACTIVE binary rather than whatever a stale PATH happens to select,
// regardless of which cone is running the plan. os.Executable() is portable to any
// process, including a compiled-in plugin candy (same binary, same process).

// CurrentCharlyExecutable is the executable that owns the current check/deploy-verify
// run. Keeping it a package var (not a direct os.Executable() call) lets a caller's test
// prove the resolver contract uses the ACTIVE binary rather than a stale PATH selection.
var CurrentCharlyExecutable = os.Executable

// StampCharlyBin records the active charly executable path into a runtime check-var
// resolver's Env as CHARLY_BIN, so host-side plan re-entry (a step referencing
// ${CHARLY_BIN}) drives the active binary instead of a stale PATH selection. CHARLY_BIN
// is deliberately never synthesized from PATH: an unavailable executable leaves the
// variable unresolved instead of silently selecting an unrelated installed charly.
// nil-safe; idempotent.
func StampCharlyBin(res *CheckVarResolver) *CheckVarResolver {
	if res == nil {
		return res
	}
	if res.Env == nil {
		res.Env = map[string]string{}
	}
	if path, err := CurrentCharlyExecutable(); err == nil && strings.TrimSpace(path) != "" {
		res.Env["CHARLY_BIN"] = path
	}
	return res
}

// NewRuntimeCheckVarResolver constructs a runtime check-var resolver (HasRuntime true)
// from an env map, stamping CHARLY_BIN via StampCharlyBin.
func NewRuntimeCheckVarResolver(env map[string]string) *CheckVarResolver {
	if env == nil {
		env = map[string]string{}
	}
	return StampCharlyBin(&CheckVarResolver{Env: env, HasRuntime: true})
}
