package sdk

import (
	"testing"

	pb "github.com/opencharly/spec/proto"
	"google.golang.org/protobuf/proto"
)

// BuildCapabilities with no requires must be byte-identical to the pre-change shape:
// the wire Capabilities.requires field stays empty, so a plugin with no dependencies
// is unaffected (the NewMeta/BuildCapabilities call sites are 144 across the corpus).
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
