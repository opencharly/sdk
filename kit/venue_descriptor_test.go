package kit

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestVenueFromDescriptor_Empty(t *testing.T) {
	exec, err := VenueFromDescriptor(spec.VenueDescriptor{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec != nil {
		t.Fatalf("want nil executor for an empty descriptor, got %#v", exec)
	}
}

func TestVenueFromDescriptor_Shell(t *testing.T) {
	exec, err := VenueFromDescriptor(spec.VenueDescriptor{Kind: "shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := exec.(ShellExecutor); !ok {
		t.Fatalf("want ShellExecutor, got %#v", exec)
	}
}

func TestVenueFromDescriptor_SSH(t *testing.T) {
	exec, err := VenueFromDescriptor(spec.VenueDescriptor{Kind: "ssh", User: "arch", Host: "charly-arch", Port: 2222})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sshExec, ok := exec.(*SSHExecutor)
	if !ok {
		t.Fatalf("want *SSHExecutor, got %#v", exec)
	}
	if sshExec.User != "arch" || sshExec.Host != "charly-arch" || sshExec.Port != 2222 {
		t.Fatalf("fields not threaded through: %#v", sshExec)
	}
}

func TestVenueFromDescriptor_UnknownKind(t *testing.T) {
	if _, err := VenueFromDescriptor(spec.VenueDescriptor{Kind: "carrier-pigeon"}); err == nil {
		t.Fatal("want an error for an unknown venue descriptor kind")
	}
}

// The DescriptorFromExecutor tests below mirror TestVenueFromDescriptor_*'s
// coverage exactly (pr-validator sdk#86 round-1 finding: the pure inverse
// function shipped with zero test coverage while its sibling got a 4-case
// suite in the same diff) plus an explicit round-trip property test proving
// VenueFromDescriptor(DescriptorFromExecutor(x)) reproduces x for both
// recognized kinds.

func TestDescriptorFromExecutor_Shell(t *testing.T) {
	d := DescriptorFromExecutor(ShellExecutor{})
	if d.Kind != "shell" {
		t.Fatalf("want Kind=%q, got %#v", "shell", d)
	}
	if !reflect.DeepEqual(d, spec.VenueDescriptor{Kind: "shell"}) {
		t.Fatalf("want a bare {Kind:\"shell\"} descriptor, got %#v", d)
	}
}

func TestDescriptorFromExecutor_SSH(t *testing.T) {
	exec := &SSHExecutor{
		User:           "arch",
		Host:           "charly-arch",
		Port:           2222,
		Args:           []string{"-o", "StrictHostKeyChecking=no"},
		ConnectTimeout: 30,
	}
	d := DescriptorFromExecutor(exec)
	if d.Kind != "ssh" {
		t.Fatalf("want Kind=%q, got %#v", "ssh", d)
	}
	if d.User != exec.User || d.Host != exec.Host || d.Port != exec.Port || d.ConnectTimeout != exec.ConnectTimeout {
		t.Fatalf("scalar fields not threaded through: %#v", d)
	}
	if len(d.Args) != len(exec.Args) {
		t.Fatalf("Args not threaded through: want %v, got %v", exec.Args, d.Args)
	}
	for i := range exec.Args {
		if d.Args[i] != exec.Args[i] {
			t.Fatalf("Args[%d] mismatch: want %q, got %q", i, exec.Args[i], d.Args[i])
		}
	}
}

// TestDescriptorFromExecutor_Unrecognized proves the default case: a
// concrete executor type DescriptorFromExecutor does not recognize (e.g. a
// composed *NestedExecutor, which cannot be flattened into one
// {kind,host,port} tuple) returns the zero VenueDescriptor{} — callers treat
// that identically to VenueFromDescriptor's own "" case (no venue; keep
// whatever executor is already in hand).
func TestDescriptorFromExecutor_Unrecognized(t *testing.T) {
	nested := &NestedExecutor{Parent: ShellExecutor{}, Jump: NestedJump{Kind: JumpPodmanExec, Target: "child"}}
	d := DescriptorFromExecutor(nested)
	if !reflect.DeepEqual(d, spec.VenueDescriptor{}) {
		t.Fatalf("want the zero VenueDescriptor for an unrecognized executor type, got %#v", d)
	}
}

func TestVenueDescriptorRoundTrip_Shell(t *testing.T) {
	d := DescriptorFromExecutor(ShellExecutor{})
	exec, err := VenueFromDescriptor(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := exec.(ShellExecutor); !ok {
		t.Fatalf("round trip did not reproduce a ShellExecutor: %#v", exec)
	}
}

func TestVenueDescriptorRoundTrip_SSH(t *testing.T) {
	original := &SSHExecutor{
		User:           "arch",
		Host:           "charly-arch",
		Port:           2222,
		Args:           []string{"-o", "StrictHostKeyChecking=no"},
		ConnectTimeout: 30,
	}
	d := DescriptorFromExecutor(original)
	exec, err := VenueFromDescriptor(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roundTripped, ok := exec.(*SSHExecutor)
	if !ok {
		t.Fatalf("round trip did not reproduce an *SSHExecutor: %#v", exec)
	}
	if roundTripped.User != original.User || roundTripped.Host != original.Host ||
		roundTripped.Port != original.Port || roundTripped.ConnectTimeout != original.ConnectTimeout {
		t.Fatalf("round trip lost scalar fields: want %#v, got %#v", original, roundTripped)
	}
	if len(roundTripped.Args) != len(original.Args) {
		t.Fatalf("round trip lost Args: want %v, got %v", original.Args, roundTripped.Args)
	}
	for i := range original.Args {
		if roundTripped.Args[i] != original.Args[i] {
			t.Fatalf("round trip Args[%d] mismatch: want %q, got %q", i, original.Args[i], roundTripped.Args[i])
		}
	}
}
