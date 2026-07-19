package sdk

import (
	"testing"

	pb "github.com/opencharly/sdk/proto"
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
