package sdk

// step_contract_test.go — every StepContract field a plugin DECLARES must reach the host over the
// wire, because the host is what acts on it. `PluginPaths` (opencharly/charly#1131) is the flag
// that makes the host inject the connected plugin set into the step's input; a field dropped in
// this conversion is a capability the plugin declares and the host never sees.

import (
	"testing"

	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
	"google.golang.org/protobuf/proto"
)

func TestBuildCapabilitiesCarriesTheStepContractFlagOnTheWire(t *testing.T) {
	provided := []ProvidedCapability{{
		Class: "step",
		Word:  "plugin-tree",
		StepContract: &spec.StepContract{
			Scope:       spec.ScopeSystem,
			Venue:       spec.VenueHostNative,
			Gate:        spec.GateNone,
			Emits:       false,
			PluginPaths: true,
		},
	}}
	caps, err := BuildCapabilitiesWithRequires("2026.176.0001", provided, nil, testSchemaFS(), "schema")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := proto.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	var got pb.Capabilities
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	var contract *pb.StepContract
	for _, c := range got.GetProvided() {
		if c.GetClass() == "step" && c.GetWord() == "plugin-tree" {
			contract = c.GetStepContract()
		}
	}
	if contract == nil {
		t.Fatal("the class:step capability reached the wire without its StepContract")
	}
	if !contract.GetPluginPaths() {
		t.Fatal("PluginPaths did not survive the capability build: the host would never inject the plugin set")
	}
	if contract.GetScope() != spec.ScopeSystem.String() || contract.GetVenue() != int32(spec.VenueHostNative) {
		t.Errorf("the contract's other fields drifted: scope=%q venue=%d", contract.GetScope(), contract.GetVenue())
	}
}
