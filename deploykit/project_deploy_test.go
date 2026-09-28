package deploykit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestProjectDeployConfig_NamespaceQualified is the regression for the qualified
// projection: a namespaced deploy must appear under its `ns.name` key so a
// merged-root consumer (`charly deploy add charly.check-vm`) resolves it. On the
// former root-scope `uf.Deploy` projection the key is absent and the walk defaults
// the target to "pod".
func TestProjectDeployConfig_NamespaceQualified(t *testing.T) {
	disp := true
	ns := &spec.UnifiedFile{Deploy: map[string]spec.DeployNode{
		"check-vm": {Target: "vm", From: "base-vm", Disposable: &disp},
	}}
	uf := &spec.UnifiedFile{
		Deploy:     map[string]spec.DeployNode{"local-pod": {Target: "pod", Image: "x"}},
		Namespaces: map[string]*spec.UnifiedFile{"charly": ns},
	}
	dc := ProjectDeployConfig(uf)
	if dc == nil {
		t.Fatal("ProjectDeployConfig returned nil")
	}
	if _, ok := dc.Deploy["local-pod"]; !ok {
		t.Fatal("local deploy missing")
	}
	if _, ok := dc.Deploy["charly.check-vm"]; !ok {
		t.Fatalf("namespaced deploy missing; keys = %v (must project the qualified set)", keysOf(dc.Deploy))
	}
}

func keysOf(m map[string]DeployNode) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	return k
}
