package loaderkit

import (
	"strings"
	"testing"

	"github.com/opencharly/spec/spec"
)

// The DECLARED-but-UNCONNECTED structural-kind exemption for the targetless-bed member
// requirement (validate_check_beds.go). After the group:-kind removal, a targetless bed node can
// only be an external STRUCTURAL plugin kind; when that kind is declared in the schema vocabulary
// (Threaded.StructuralKinds) but its plugin did not connect (no registered input schema — absent
// from Threaded.StructuralDeclaredFields), its OpLoad never ran and the bed cannot be
// member-scanned, so the member requirement must not fire. A CONNECTED structural kind (schema
// registered) whose bed is targetless+memberless is a real defect and still fails.

func structKindBed(name string) map[string]spec.FleetNode {
	disp := true
	return map[string]spec.FleetNode{
		name: {Target: "", Disposable: &disp},
	}
}

// TestValidateCheckBedsExemptsUnconnectedStructuralKind pins the exemption: a bed whose kind is
// DECLARED (StructuralKinds) but whose plugin is NOT connected (no registered schema — the
// documented no-declared-schema signal) folds targetless+memberless and must VALIDATE.
func TestValidateCheckBedsExemptsUnconnectedStructuralKind(t *testing.T) {
	threaded := spec.Threaded{
		StructuralKinds: map[string]bool{"examplestructkind": true},
		// StructuralDeclaredFields deliberately ABSENT for the word: the plugin's schema was
		// never registered because the provider never connected.
	}
	uf := &spec.UnifiedFile{Fleet: structKindBed("check-structkind")}
	if err := ValidateCheckBeds(uf, threaded); err != nil {
		t.Fatalf("a declared-but-unconnected structural kind bed must be exempt from the member requirement: %v", err)
	}
}

// TestValidateCheckBedsFailsConnectedStructuralKind pins the defect arm: a CONNECTED structural
// kind (its input schema registered — the presence its OpLoad dispatch hard-requires) whose bed
// is STILL targetless+memberless is a real fold defect and must keep failing.
func TestValidateCheckBedsFailsConnectedStructuralKind(t *testing.T) {
	threaded := spec.Threaded{
		StructuralKinds:          map[string]bool{"examplestructkind": true},
		StructuralDeclaredFields: map[string]map[string]bool{"examplestructkind": {"marker": true}},
	}
	uf := &spec.UnifiedFile{Fleet: structKindBed("check-structkind")}
	err := ValidateCheckBeds(uf, threaded)
	if err == nil {
		t.Fatal("a CONNECTED structural kind folding a targetless memberless bed is a defect — must fail")
	}
	if !strings.Contains(err.Error(), "a group bed must declare member subdeployments") {
		t.Fatalf("the failure must stay the member-requirement error, got: %v", err)
	}
}

// TestValidateCheckBedsStillFailsPlainTargetlessBed pins the unchanged baseline: with NO declared
// structural kind in the vocabulary, a targetless memberless bed remains a hard error (the
// exemption must not widen to unstructured shapes).
func TestValidateCheckBedsStillFailsPlainTargetlessBed(t *testing.T) {
	threaded := spec.Threaded{}
	uf := &spec.UnifiedFile{Fleet: structKindBed("check-orphan")}
	if err := ValidateCheckBeds(uf, threaded); err == nil {
		t.Fatal("a targetless memberless bed with no declared structural kind must still fail")
	}
}
