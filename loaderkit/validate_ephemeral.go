package loaderkit

import (
	"fmt"
	"strings"
	"time"

	"github.com/opencharly/spec/spec"
)

// validate_ephemeral.go — the LOAD-time ephemeral / vm-naming validators (K1-LOADER RELOCATION,
// moved from charly/validate_ephemeral.go). They read the registry-derived spec.Threaded.DeployTraits
// DATA snapshot instead of the live registry (the former deployTraitsFor), so they run identically
// host-side OR plugin-side (boundary law clause D). Error accumulation uses spec.Diagnostics
// (RULING 2, R3-reuse — the existing {Items:[{Severity,Message,Path}]} type, a strict superset of the
// former charly ValidationError's flat string list; every item here is Severity "error").

// addErr appends an error-severity diagnostic (the ValidationError.Add analogue).
func addErr(d *spec.Diagnostics, format string, args ...any) {
	d.Items = append(d.Items, spec.Diagnostic{Severity: "error", Message: fmt.Sprintf(format, args...)})
}

// diagErrors reports whether d carries any error-severity item.
func diagErrors(d *spec.Diagnostics) bool {
	for _, it := range d.Items {
		if it.Severity == "error" {
			return true
		}
	}
	return false
}

// diagJoin joins every error-severity message (the ValidationError.Error analogue).
func diagJoin(d *spec.Diagnostics) string {
	var msgs []string
	for _, it := range d.Items {
		if it.Severity == "error" {
			msgs = append(msgs, it.Message)
		}
	}
	return strings.Join(msgs, "\n  ")
}

// ValidateEphemeralOnNode applies all ephemeral-related invariants to a single DeployNode, reading
// the substrate's DECLARED #DeployTraits from the DATA snapshot t (never the registry). Errors
// accumulate into d. Byte-equivalent to the former charly ValidateEphemeralOnNode.
func ValidateEphemeralOnNode(name string, node *spec.DeployNode, t spec.Threaded, d *spec.Diagnostics) {
	if node == nil {
		return
	}
	traits := t.DeployTraits[node.Target]
	if !node.IsEphemeral() {
		if node.FromSnapshot != "" && (traits == nil || !traits.SupportsFromSnapshot) {
			addErr(d, "deployment %q: from_snapshot is only valid on a backing-chain substrate (vm) (got target=%q)", name, node.Target)
		}
		return
	}
	if traits != nil && !traits.SupportsEphemeral {
		if node.FromSnapshot != "" && !traits.SupportsFromSnapshot {
			addErr(d, "deployment %q: target=%s with from_snapshot is not supported (containers/namespaces have no backing chains; only vm restores from a snapshot)", name, node.Target)
		}
		addErr(d, "deployment %q: target=%s with ephemeral is not yet supported (the ephemeral lifecycle — TTL timer + charly.yml persistence — is wired for target=vm only; tracked for the other substrates in the bed-robustness batch)", name, node.Target)
	}
	if node.Ephemeral != nil && node.Ephemeral.TTL != "" {
		if dur, err := time.ParseDuration(node.Ephemeral.TTL); err != nil {
			addErr(d, "deployment %q: ephemeral.ttl %q is not a valid Go duration (e.g. 30m, 2h, 90s): %v", name, node.Ephemeral.TTL, err)
		} else if dur <= 0 {
			addErr(d, "deployment %q: ephemeral.ttl must be > 0 (got %s)", name, dur)
		}
	}
}

// ValidateVmNamingGuard enforces the reserved -eph- infix on user-authored entity names.
func ValidateVmNamingGuard(name string, d *spec.Diagnostics) {
	if strings.Contains(name, "-eph-") {
		addErr(d, "name %q contains reserved infix \"-eph-\"; this is reserved for ephemeral instance names — pick a different name", name)
	}
}

// fillEphemeralDefaults is the LOAD/FINALIZE defaults fill for the ephemeral →
// disposable promotion: an ephemeral deploy implies disposable:true (the
// destroy-and-rebuild authorization), even when the author wrote no `disposable:`
// key. It runs ONCE at load/finalize time (LoadUnified), NOT inside a validator —
// validators are read-only (F5.2: ValidateEphemeralUnified no longer mutates its
// subject). Registry-free, deterministic, idempotent.
func fillEphemeralDefaults(uf *spec.UnifiedFile) {
	if uf == nil {
		return
	}
	for name, node := range uf.Deploy {
		if node.IsEphemeral() && (node.Disposable == nil || !*node.Disposable) {
			tr := true
			node.Disposable = &tr
			uf.Deploy[name] = node
		}
	}
}

// ValidateEphemeralUnified is the LoadSeams.ValidateEphemeral entry point: it validates the
// ephemeral / vm-naming invariants across the spec.UnifiedFile's Deploy map, reading the
// DeployTraits DATA snapshot t. READ-ONLY — the disposable:true promotion happens at load/
// finalize time in fillEphemeralDefaults (F5.2), never in a validator. Moved verbatim
// (behaviour-preserving) from charly's validateEphemeralUnified.
func ValidateEphemeralUnified(uf *spec.UnifiedFile, t spec.Threaded) error {
	if uf == nil {
		return nil
	}
	var d spec.Diagnostics
	for name, node := range uf.Deploy {
		n := node
		ValidateEphemeralOnNode(name, &n, t, &d)
		ValidateVmNamingGuard(name, &d)
	}
	if diagErrors(&d) {
		return fmt.Errorf("ephemeral / naming validation:\n  %s", diagJoin(&d))
	}
	return nil
}
