package sdk

import (
	"testing"

	pb "github.com/opencharly/spec/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCommandModelWireFieldAllocationAndRoundTrip(t *testing.T) {
	field := (&pb.ProvidedCapability{}).ProtoReflect().Descriptor().Fields().ByName("command_model_json")
	if field == nil {
		t.Fatal("ProvidedCapability.command_model_json is missing from the generated descriptor")
	}
	if got, want := field.Number(), protoreflect.FieldNumber(13); got != want {
		t.Fatalf("command_model_json field number = %d, want %d", got, want)
	}

	model := []byte(`{"name":"agent","help":"remote agent"}`)
	wire, err := proto.Marshal(&pb.ProvidedCapability{Class: "command", Word: "agent", CommandModelJson: model})
	if err != nil {
		t.Fatal(err)
	}
	var capability pb.ProvidedCapability
	if err := proto.Unmarshal(wire, &capability); err != nil {
		t.Fatal(err)
	}
	if got := capability.GetCommandModelJson(); string(got) != string(model) {
		t.Fatalf("command model round trip = %q, want %q", got, model)
	}
}

// TestBuildCapabilitiesDropsProtocolVersion gates the removal of the redundant
// charly protocol_version wire field (opencharly/spec#138). The emitted
// Capabilities must NOT carry protocol_version on the wire: before this change
// BuildCapabilities set it from the sdk.ProtocolVersion const, so this assertion
// fails without the change. proto3 omits zero-valued scalars, so a set field 2
// appears on the wire and a dropped one does not.
//
// The check is by NAME (descriptor) and by wire field NUMBER, so it is
// pin-agnostic: against a spec that still declares the field the descriptor names
// it, and once the pin adopts the field-removed spec the wire walk is what
// remains. Either way the emitted message carries no field 2.
func TestBuildCapabilitiesDropsProtocolVersion(t *testing.T) {
	caps, err := BuildCapabilities("2026.261.1747", nil, nil, "")
	if err != nil {
		t.Fatalf("BuildCapabilities: %v", err)
	}
	wire, err := proto.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}

	const protocolVersionField = protowire.Number(2)
	for len(wire) > 0 {
		num, _, n := protowire.ConsumeField(wire)
		if n < 0 {
			t.Fatalf("Capabilities wire is malformed: %v", protowire.ParseError(n))
		}
		if num == protocolVersionField {
			t.Fatalf("Capabilities carries wire field 2 (protocol_version), want it dropped")
		}
		wire = wire[n:]
	}
}

// TestBuildCapabilitiesCarriesInteractive locks the ProvidedCapability.Interactive
// pass-through: a class=command capability declaring Interactive=true must carry it
// onto the emitted proto capability. FAILS without the sdk's `Interactive:
// c.Interactive` line in BuildCapabilities (spec's capability.Interactive field is
// present, but the sdk mapping is what this test exercises).
func TestBuildCapabilitiesCarriesInteractive(t *testing.T) {
	caps, err := BuildCapabilities("2026.261.1747", []ProvidedCapability{
		{Class: "command", Word: "shell", Interactive: true},
		{Class: "command", Word: "check", Interactive: false},
	}, nil, "")
	if err != nil {
		t.Fatalf("BuildCapabilities: %v", err)
	}
	byWord := map[string]*pb.ProvidedCapability{}
	for _, c := range caps.GetProvided() {
		byWord[c.GetWord()] = c
	}
	if !byWord["shell"].GetInteractive() {
		t.Fatal("command:shell Interactive=true not carried onto the proto capability")
	}
	if byWord["check"].GetInteractive() {
		t.Fatal("command:check Interactive=false must not be set")
	}
}
