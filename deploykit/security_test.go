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

// The tests below migrated from charly/security_test.go with the W9 CollectSecurity split:
// IpcModeBlocksShmSize/the byte-size merge helpers now live in this package exclusively
// (parseShmBytes/maxShmSize/minCap/minCpus back MergeCandySecurity above); the append-unique
// helper's own test now lives with its implementation in kit/append_unique_test.go (this
// package aliases it via kit_aliases.go).

// TestIpcModeBlocksShmSize is the helper-level check for the gate.
// Only "host" should trigger the drop.
func TestIpcModeBlocksShmSize(t *testing.T) {
	cases := []struct {
		ipc  string
		want bool
	}{
		{"host", true},
		{"private", false},
		{"shareable", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IpcModeBlocksShmSize(tc.ipc); got != tc.want {
			t.Errorf("IpcModeBlocksShmSize(%q) = %v, want %v", tc.ipc, got, tc.want)
		}
	}
}

func TestMaxShmSize(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"", "1g", "1g"},
		{"1g", "", "1g"},
		{"256m", "1g", "1g"},
		{"2g", "1g", "2g"},
		{"512m", "512m", "512m"},
	}
	for _, tt := range tests {
		got := maxShmSize(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("maxShmSize(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseShmBytes(t *testing.T) {
	tests := []struct {
		s    string
		want int64
	}{
		{"1g", 1024 * 1024 * 1024},
		{"256m", 256 * 1024 * 1024},
		{"64k", 64 * 1024},
		{"1024", 1024},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseShmBytes(tt.s)
		if got != tt.want {
			t.Errorf("parseShmBytes(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

func TestMinCap(t *testing.T) {
	// Smallest-wins: opposite of maxShmSize. Tighter cap is safer.
	tests := []struct {
		a, b, want string
	}{
		{"", "1g", "1g"},
		{"1g", "", "1g"},
		{"256m", "1g", "256m"},
		{"2g", "1g", "1g"},
		{"512m", "512m", "512m"},
		{"1024m", "1g", "1024m"}, // equal sizes — first wins
	}
	for _, tt := range tests {
		got := minCap(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("minCap(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMinCpus(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"", "2", "2"},
		{"2", "", "2"},
		{"1.5", "4", "1.5"},
		{"8", "2.5", "2.5"},
		{"2", "2", "2"},
		{"bogus", "2", "2"}, // unparseable → other wins
		{"2", "bogus", "2"},
	}
	for _, tt := range tests {
		got := minCpus(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("minCpus(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

// candyWithSecurity builds a CandyModel (via the existing NewSpecCandyModel envelope adapter)
// carrying only a Security config — the minimal fixture MergeCandySecurity needs.
func candyWithSecurity(sec *SecurityConfig) CandyModel {
	return NewSpecCandyModel(spec.CandyModel{Security: sec}, spec.CandyView{})
}

func TestMergeCandySecurityMergesCapsSmallest(t *testing.T) {
	candies := []CandyModel{
		candyWithSecurity(&SecurityConfig{MemoryMax: "8g", MemoryHigh: "7g", Cpus: "8"}),
		candyWithSecurity(&SecurityConfig{MemoryMax: "4g", MemoryHigh: "3g", Cpus: "2"}),
	}
	sec := MergeCandySecurity(candies, nil)
	if sec.MemoryMax != "4g" {
		t.Errorf("MemoryMax = %q, want 4g (smallest wins)", sec.MemoryMax)
	}
	if sec.MemoryHigh != "3g" {
		t.Errorf("MemoryHigh = %q, want 3g", sec.MemoryHigh)
	}
	if sec.Cpus != "2" {
		t.Errorf("Cpus = %q, want 2", sec.Cpus)
	}
}

func TestMergeCandySecurityImageOverridesCaps(t *testing.T) {
	candies := []CandyModel{
		candyWithSecurity(&SecurityConfig{MemoryMax: "6g", ShmSize: "1g"}),
	}
	sec := MergeCandySecurity(candies, &SecurityConfig{MemoryMax: "16g"})
	if sec.MemoryMax != "16g" {
		t.Errorf("MemoryMax = %q, want 16g (box override)", sec.MemoryMax)
	}
	if sec.ShmSize != "1g" {
		t.Errorf("ShmSize = %q, want 1g (candy default preserved)", sec.ShmSize)
	}
}

func TestMergeCandySecurityNilCandySkipped(t *testing.T) {
	candies := []CandyModel{nil, candyWithSecurity(&SecurityConfig{Privileged: true})}
	sec := MergeCandySecurity(candies, nil)
	if !sec.Privileged {
		t.Error("expected Privileged=true from the non-nil candy")
	}
}
