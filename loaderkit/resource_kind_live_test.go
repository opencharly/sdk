// resource_kind_live_test.go — the resource-kind membership the parse asks must come from the
// CONTRACT at consult time, not from a snapshot the loader took at package init.
//
// Before this test the loader derived its own copy once, at init:
//
//	resourceKindSet = sliceSet(spec.ResourceKinds)
//
// so the parse and `spec.ResourceKindSet()` were two independent derivations of one list that
// could only ever agree BY LUCK — and a stale copy is not a theoretical hazard: the generated
// vocabulary moves whenever `#ResourceKind` moves, and a binary whose init ran before a
// vocabulary change keeps classifying from the old set.
//
// The observable is divergence, not compilation: the test makes the schema's own list move and
// asks the parse. A loader holding its own copy answers from the snapshot and fails; a loader
// asking the contract answers from the vocabulary.
package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestMemberDiscTracksTheVocabulary(t *testing.T) {
	const probe = "resourcekindtest"
	orig := spec.ResourceKinds
	spec.ResourceKinds = append(append([]string{}, orig...), probe)
	t.Cleanup(func() { spec.ResourceKinds = orig })

	if !memberDisc(probe, spec.Threaded{}) {
		t.Fatal("memberDisc does not see a word the composed schema now declares: the loader answers " +
			"from a snapshot taken at package init, so its verdict can disagree with spec.ResourceKindSet()")
	}
	// The control: a word nobody declares must stay OUT, or the test above would pass on an
	// implementation that simply said yes to everything.
	if memberDisc("definitely-not-a-kind-word", spec.Threaded{}) {
		t.Fatal("memberDisc accepts a word no vocabulary declares")
	}
}
