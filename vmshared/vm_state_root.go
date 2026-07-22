package vmshared

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// vm_state_root.go — the ONE root-path resolver for per-VM host state
// (~/.local/share/charly/vm/<domain>/ — ssh keys, known_hosts, seed ISO, the per-domain disk
// overlay, snapshots/, instance.yml). Bed-robustness batch item 6 (the "global
// ~/.local/share/charly/vm non-worktree-scoping footgun"): a libvirt DOMAIN name is derived
// PURELY from the deploy name (VmDomainIdentity), with no project/worktree component, so two
// concurrent checkouts (separate `git worktree`s, the project's own standing multi-worktree /
// multi-teammate development model) that happen to run the SAME bed/deploy name — the common
// case, since bed names like "check-charly-vm" are fixed identifiers shared across every
// worktree of the same repo — collide on the SAME host state directory (and, more severely, the
// SAME libvirt domain). VmStateRoot is the single override point: set CHARLY_VM_STATE_DIR to a
// worktree-distinct path (e.g. in a per-worktree wrapper/.envrc) and every VM state file for
// that invocation lands under it instead of the shared default — the SAME env-var-override
// pattern already established for CHARLY_REPO_CACHE / CHARLY_REPO_OVERRIDE / CHARLY_PROJECT_DIR.
// Default behavior (no env var set) is UNCHANGED — this is purely additive, zero-risk for the
// common single-worktree case. Every VM state-path call site (charly core, candy/plugin-vm,
// candy/plugin-deploy-vm, this package's own vm_snapshot.go) routes through this ONE function
// (R3 — the literal `filepath.Join(home, ".local", "share", "charly", "vm")` was previously
// duplicated across 7+ files) so the override applies uniformly, never partially.
const VmStateDirEnv = "CHARLY_VM_STATE_DIR"

// VmStateRoot resolves the root directory for per-VM host state. Honors CHARLY_VM_STATE_DIR
// when set (trimmed, must be non-empty, must be absolute — a relative override would resolve
// against whatever cwd happens to be active at each call site, defeating the whole point of a
// stable per-worktree pin); otherwise falls back to the default
// ~/.local/share/charly/vm.
func VmStateRoot() (string, error) {
	if raw := strings.TrimSpace(os.Getenv(VmStateDirEnv)); raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("%s=%q must be an absolute path", VmStateDirEnv, raw)
		}
		return raw, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "charly", "vm"), nil
}
