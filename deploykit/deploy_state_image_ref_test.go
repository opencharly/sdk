package deploykit

import (
	"strings"
	"testing"
)

// TestRejectImageRefAsDeployName_NamesTheWorkingVerb pins the REMEDIATION, not just the refusal.
//
// Sending the operator to `charly deploy add <name> <ref>` would REJECT a
// registry image ref ("not found as a box or candy" — measured on plugin-fleet#51). An error that
// names a verb which cannot work costs the operator a second failure, so the remediation is
// asserted here by its own test: a user-visible sentence is the artifact.
func TestRejectImageRefAsDeployName_NamesTheWorkingVerb(t *testing.T) {
	const ref = "ghcr.io/opencharly/openwebui:2026.317.2254"
	err := RejectImageRefAsDeployName(ref)
	if err == nil {
		t.Fatal("a tagged registry image ref used as a deploy name must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "charly deploy from-box "+ref) {
		t.Errorf("remediation does not name the verb that works (deploy from-box <ref>):\n%s", msg)
	}
	if strings.Contains(msg, "charly deploy add") {
		t.Errorf("remediation still names `deploy add`, which rejects a registry image ref:\n%s", msg)
	}
}

// TestRejectImageRefAsDeployName_LeavesNamesAlone is the guard on the other side: a plain name,
// a bare name with a tag, and a github repo ref (which pins with @version) are NOT image refs.
func TestRejectImageRefAsDeployName_LeavesNamesAlone(t *testing.T) {
	for _, ok := range []string{"fedora-coder", "openwebui", "fedora:43", "github.com/opencharly/pod-comfyui@v2026.319.0023"} {
		if err := RejectImageRefAsDeployName(ok); err != nil {
			t.Errorf("%q must not be rejected as a deploy name: %v", ok, err)
		}
	}
}
