package deploykit

import (
	"reflect"
	"testing"

	"github.com/opencharly/sdk/spec"
)

func TestSecurityArgsPrivileged(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{Privileged: true})
	want := []string{"--privileged"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(privileged) = %v, want %v", args, want)
	}
}

func TestSecurityArgsCapabilities(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{
		CapAdd:      []string{"SYS_ADMIN", "MKNOD"},
		Devices:     []string{"/dev/fuse"},
		SecurityOpt: []string{"label=disable"},
	})
	want := []string{
		"--cap-add", "SYS_ADMIN",
		"--cap-add", "MKNOD",
		"--device", "/dev/fuse",
		"--security-opt", "label=disable",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(caps) = %v, want %v", args, want)
	}
}

func TestSecurityArgsEmpty(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{})
	if len(args) != 0 {
		t.Errorf("SecurityArgs(empty) = %v, want empty", args)
	}
}

func TestSecurityArgsPrivilegedOverridesCaps(t *testing.T) {
	// When privileged is true, only --privileged is emitted (caps are redundant)
	args := SecurityArgs(spec.SecurityConfig{
		Privileged: true,
		CapAdd:     []string{"SYS_ADMIN"},
	})
	want := []string{"--privileged"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(privileged+caps) = %v, want %v", args, want)
	}
}

func TestSecurityArgsShmSize(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{ShmSize: "1g"})
	want := []string{"--shm-size", "1g"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(shm_size) = %v, want %v", args, want)
	}
}

func TestSecurityArgsShmSizeWithPrivileged(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{Privileged: true, ShmSize: "512m"})
	want := []string{"--privileged", "--shm-size", "512m"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(privileged+shm) = %v, want %v", args, want)
	}
}

// TestSecurityArgsShmSizeDroppedWhenIpcHost covers the 2026-04-27 fix
// for the shm_size + ipc=host conflict: when IpcMode is "host", the
// kernel-level /dev/shm is shared with the host and podman REJECTS
// `--shm-size` with a runtime error. The fix drops the flag.
func TestSecurityArgsShmSizeDroppedWhenIpcHost(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{ShmSize: "1g", IpcMode: "host"})
	for _, a := range args {
		if a == "--shm-size" {
			t.Errorf("expected no `--shm-size` flag when IpcMode=host; got %v", args)
		}
	}
	// `--ipc host` MUST be present.
	foundIpc := false
	for i, a := range args {
		if a == "--ipc" && i+1 < len(args) && args[i+1] == "host" {
			foundIpc = true
		}
	}
	if !foundIpc {
		t.Errorf("expected `--ipc host` flag; got %v", args)
	}
}

// TestSecurityArgsShmSizeRetainedWhenIpcPrivate verifies the gate
// only fires for IpcMode=host — other values (private, shareable,
// empty) keep the flag.
func TestSecurityArgsShmSizeRetainedWhenIpcPrivate(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{ShmSize: "1g", IpcMode: "private"})
	foundShm := false
	for i, a := range args {
		if a == "--shm-size" && i+1 < len(args) && args[i+1] == "1g" {
			foundShm = true
		}
	}
	if !foundShm {
		t.Errorf("expected `--shm-size 1g` flag with IpcMode=private; got %v", args)
	}
}

func TestSecurityArgsMemoryCaps(t *testing.T) {
	args := SecurityArgs(spec.SecurityConfig{
		MemoryMax:     "6g",
		MemoryHigh:    "5g",
		MemorySwapMax: "2g",
		Cpus:          "4",
	})
	want := []string{
		"--memory", "6g",
		"--memory-reservation", "5g",
		"--memory-swap", "2g",
		"--cpus", "4",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(caps) = %v, want %v", args, want)
	}
}

func TestSecurityArgsMemoryCapsWithPrivileged(t *testing.T) {
	// Privileged containers still need resource caps — they can run anything
	// kernel-level but don't get a free pass on memory/CPU.
	args := SecurityArgs(spec.SecurityConfig{
		Privileged: true,
		ShmSize:    "1g",
		MemoryMax:  "6g",
		Cpus:       "2.5",
	})
	want := []string{
		"--privileged",
		"--shm-size", "1g",
		"--memory", "6g",
		"--cpus", "2.5",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("SecurityArgs(privileged+caps) = %v, want %v", args, want)
	}
}
