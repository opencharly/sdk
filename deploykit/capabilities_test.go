package deploykit

import "testing"

// TestCapabilityLabelCompleteness verifies every exported field on spec.BoxMetadata has a
// CapabilityLabelMap entry. Adding a new capability field without a label mapping is a
// build break — enforces the invariant "every capability lives in an OCI label" so that
// `charly bundle from-box` can reconstruct the full contract from a pushed image.
func TestCapabilityLabelCompleteness(t *testing.T) {
	if err := CheckCapabilityLabelCompleteness(); err != nil {
		t.Fatal(err)
	}
}
