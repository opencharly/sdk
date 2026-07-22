package kit

import (
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
