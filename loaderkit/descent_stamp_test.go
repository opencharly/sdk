package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestStampDeployDescents_RecursesNamespaces is the roster regression: StampDeployDescents must
// stamp every imported NAMESPACE's deploy nodes, not just the root map. A namespaced bed
// (`charly.check-agentteams-vm`) resolves from its OWNING namespace's UnifiedFile, so an unstamped
// namespace node reaches the bed runner with Descent==nil — deploy.IsVmVenue returns false and the
// `vm:` root is misclassified as a pod, failing `deploy add` with an empty image. Measured live:
// the same bed was IsVM=true run locally and IsVM=false run namespaced from the umbrella root.
func TestStampDeployDescents_RecursesNamespaces(t *testing.T) {
	threaded := spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{
		"pod": {Venue: "container", ImageBacked: true},
		"vm":  {Venue: "ssh"},
	}}
	ns := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
		"ns-vm": {Target: "vm", From: "base-vm"},
	}}
	root := &spec.UnifiedFile{
		Deploy:     map[string]spec.DeployNode{"root-pod": {Target: "pod", Image: "x"}},
		Namespaces: map[string]*spec.UnifiedFile{"charly": ns},
	}

	StampDeployDescents(root, threaded)

	if root.Deploy["root-pod"].Descent == nil || root.Deploy["root-pod"].Descent.Venue != "container" {
		t.Fatalf("root pod not stamped: %+v", root.Deploy["root-pod"].Descent)
	}
	got := ns.Deploy["ns-vm"]
	if got.Descent == nil {
		t.Fatal("namespaced bed node has Descent==nil — StampDeployDescents did not recurse into Namespaces")
	}
	if got.Descent.Venue != "ssh" {
		t.Fatalf("namespaced vm node venue = %q, want ssh (IsVmVenue must be true)", got.Descent.Venue)
	}
}

// TestStampDeployDescents_MutualCycleTerminates: a mutual import (main↔sub) must not loop forever;
// a shared namespace mounted at multiple paths is still stamped at each path.
func TestStampDeployDescents_MutualCycleTerminates(t *testing.T) {
	threaded := spec.Threaded{DeployTraits: map[string]*spec.DeployTraits{"vm": {Venue: "ssh"}}}
	main := &spec.UnifiedFile{}
	sub := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{"sub-vm": {Target: "vm", From: "b"}}}
	main.Deploy = map[string]spec.DeployNode{"main-vm": {Target: "vm", From: "b"}}
	main.Namespaces = map[string]*spec.UnifiedFile{"sub": sub}
	sub.Namespaces = map[string]*spec.UnifiedFile{"up": main} // mutual cycle

	StampDeployDescents(main, threaded) // must return

	if main.Deploy["main-vm"].Descent == nil || sub.Deploy["sub-vm"].Descent == nil {
		t.Fatal("both nodes must be stamped despite the cycle")
	}
}
