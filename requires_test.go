package sdk

import (
	"context"
	"testing"

	pb "github.com/opencharly/spec/proto"
	"google.golang.org/protobuf/proto"
)

// BuildCapabilities with no requires must be byte-identical to the pre-change shape:
// the wire Capabilities.requires field stays empty, so a plugin with no dependencies
// is unaffected (the 145 sdk.NewMeta( call sites across the corpus are unchanged).
func TestBuildCapabilitiesNoRequiresIsEmpty(t *testing.T) {
	caps, err := BuildCapabilities("2026.176.0001", []ProvidedCapability{{Class: "verb", Word: "x"}}, nil, "schema")
	if err != nil {
		t.Fatal(err)
	}
	if got := caps.GetRequires(); len(got) != 0 {
		t.Fatalf("requires = %v, want empty for a nil-requires plugin", got)
	}
}

// A declared Requirement travels onto the wire Capabilities.requires field, carrying
// class/word/source/optional — the host resolves each before treating the plugin as
// loaded. Marshalled + round-tripped, so the field is proven on the wire, not merely
// in-memory.
func TestBuildCapabilitiesRequiresRoundTrip(t *testing.T) {
	caps, err := BuildCapabilitiesWithRequires("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x"}},
		[]Requirement{
			{Class: "verb", Word: "enc"},
			{Class: "kind", Word: "sidecar", Source: "github.com/opencharly/plugin-sidecar/candy/plugin-sidecar", Optional: true},
		}, nil, "schema")
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
	reqs := got.GetRequires()
	if len(reqs) != 2 {
		t.Fatalf("requires length = %d, want 2", len(reqs))
	}
	if reqs[0].GetClass() != "verb" || reqs[0].GetWord() != "enc" || reqs[0].GetSource() != "" || reqs[0].GetOptional() {
		t.Errorf("requirement[0] = %+v, want {verb enc \"\" false}", reqs[0])
	}
	if reqs[1].GetClass() != "kind" || reqs[1].GetWord() != "sidecar" ||
		reqs[1].GetSource() != "github.com/opencharly/plugin-sidecar/candy/plugin-sidecar" || !reqs[1].GetOptional() {
		t.Errorf("requirement[1] = %+v, want {kind sidecar <ref> true}", reqs[1])
	}
}

// NewMetaWithRequires must wire its declared requirements through fixedMeta.Describe —
// the authoring entry point a plugin actually uses. This test fails if the
// fixedMeta.requires field or the Describe wiring were dropped (the BuildCapabilities
// tests above would still pass, so this exercises the branch they do not).
func TestNewMetaWithRequiresDescribeCarriesRequires(t *testing.T) {
	srv := NewMetaWithRequires("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x"}},
		[]Requirement{{Class: "verb", Word: "enc"}},
		nil)
	caps, err := srv.Describe(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	reqs := caps.GetRequires()
	if len(reqs) != 1 {
		t.Fatalf("Describe requires length = %d, want 1 (fixedMeta.requires not wired?)", len(reqs))
	}
	if reqs[0].GetClass() != "verb" || reqs[0].GetWord() != "enc" {
		t.Errorf("requirement = %+v, want {verb enc}", reqs[0])
	}
}

// NewMeta (no requires) must Describe with an empty requires — the unchanged path the
// existing 145 sdk.NewMeta( call sites rely on.
func TestNewMetaDescribeHasNoRequires(t *testing.T) {
	srv := NewMeta("2026.176.0001", []ProvidedCapability{{Class: "verb", Word: "x"}}, nil)
	caps, err := srv.Describe(context.Background(), &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if got := caps.GetRequires(); len(got) != 0 {
		t.Fatalf("Describe requires = %v, want empty", got)
	}
}

// A command capability's command_parent is part of its IDENTITY and must travel on
// the wire ProvidedCapability, so an out-of-process plugin nests exactly like a
// compiled-in one. This fails without the field (the pre-change ProvidedCapability had
// no command_parent and BuildCapabilities dropped it).
func TestBuildCapabilitiesCommandParentRoundTrip(t *testing.T) {
	caps, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{
			{Class: "command", Word: "generate", CommandParent: "box"},
			{Class: "command", Word: "feature"},
		}, nil, "schema")
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
	provided := got.GetProvided()
	if len(provided) != 2 {
		t.Fatalf("provided length = %d, want 2", len(provided))
	}
	if provided[0].GetCommandParent() != "box" {
		t.Errorf("provided[0].CommandParent = %q, want %q", provided[0].GetCommandParent(), "box")
	}
	if provided[1].GetCommandParent() != "" {
		t.Errorf("provided[1].CommandParent = %q, want empty (top-level)", provided[1].GetCommandParent())
	}
}

// A command_parent outside class=command is a declaration error, caught at serve time
// so the host never sees an off-grammar identity (only commands nest).
func TestBuildCapabilitiesRejectsCommandParentOffCommand(t *testing.T) {
	_, err := BuildCapabilities("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x", CommandParent: "box"}}, nil, "schema")
	if err == nil {
		t.Fatal("BuildCapabilities accepted a command parent on a non-command capability")
	}
}

// A requirement may name a NESTED command peer distinctly from its top-level twin; the
// parent rides the wire PluginRequirement.command_parent field.
func TestBuildCapabilitiesRequirementCommandParentRoundTrip(t *testing.T) {
	caps, err := BuildCapabilitiesWithRequires("2026.176.0001",
		[]ProvidedCapability{{Class: "verb", Word: "x"}},
		[]Requirement{
			{Class: "command", Word: "feature"},
			{Class: "command", Word: "feature", CommandParent: "box"},
		}, nil, "schema")
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
	reqs := got.GetRequires()
	if len(reqs) != 2 {
		t.Fatalf("requires length = %d, want 2", len(reqs))
	}
	if reqs[0].GetCommandParent() != "" {
		t.Errorf("requires[0].CommandParent = %q, want empty (top-level)", reqs[0].GetCommandParent())
	}
	if reqs[1].GetCommandParent() != "box" {
		t.Errorf("requires[1].CommandParent = %q, want %q", reqs[1].GetCommandParent(), "box")
	}
}
