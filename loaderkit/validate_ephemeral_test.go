package loaderkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestValidateVmNamingGuard (relocated from charly/ephemeral_classification_test.go,
// #55 K3 Cone 4): verifies the `-eph-` infix is reserved for ephemeral-instance
// naming and rejected everywhere else. ValidateVmNamingGuard accumulates into
// spec.Diagnostics (RULING 2) rather than the core spec.ValidationError.
func TestValidateVmNamingGuard(t *testing.T) {
	tests := []struct {
		name        string
		shouldError bool
	}{
		{name: "arch", shouldError: false},
		{name: "arch-test", shouldError: false},
		{name: "fedora-coder", shouldError: false},
		{name: "arch-eph-abc", shouldError: true},
		{name: "test-eph-deadbeef", shouldError: true},
		{name: "-eph-", shouldError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := &spec.Diagnostics{}
			ValidateVmNamingGuard(tt.name, errs)
			has := len(errs.Items) > 0
			if has != tt.shouldError {
				t.Errorf("ValidateVmNamingGuard(%q) errors=%v, want %v", tt.name, has, tt.shouldError)
			}
		})
	}
}

// TestFillEphemeralDefaults covers F5.2: the ephemeral→disposable:true promotion is a
// LOAD/finalize defaults fill (fillEphemeralDefaults), NOT a validator side effect —
// ValidateEphemeralUnified must be read-only (it must not mutate uf.Deploy).
func TestFillEphemeralDefaults_PromotesAndValidatorReadOnly(t *testing.T) {
	uf := &spec.UnifiedFile{
		Deploy: map[string]spec.DeployNode{
			"eph":  {Ephemeral: &spec.EphemeralLifetime{TTL: "30m"}},
			"bare": {Target: "pod"},
		},
	}
	// ValidateEphemeralUnified must NOT promote (read-only validator).
	if err := ValidateEphemeralUnified(uf, spec.Threaded{}); err != nil {
		t.Fatalf("ValidateEphemeralUnified: %v", err)
	}
	if d := uf.Deploy["eph"]; d.Disposable != nil {
		t.Fatalf("validator mutated its subject: Disposable = %v, want nil (read-only)", *d.Disposable)
	}
	// fillEphemeralDefaults promotes at finalize time.
	fillEphemeralDefaults(uf)
	d := uf.Deploy["eph"]
	if d.Disposable == nil || !*d.Disposable {
		t.Fatalf("fillEphemeralDefaults did not promote ephemeral→disposable: got %v", d.Disposable)
	}
	if d := uf.Deploy["bare"]; d.Disposable != nil {
		t.Fatalf("non-ephemeral deploy must stay untouched: Disposable = %v", *d.Disposable)
	}
	// Idempotent: a second fill changes nothing.
	before := *uf.Deploy["eph"].Disposable
	fillEphemeralDefaults(uf)
	if after := *uf.Deploy["eph"].Disposable; after != before {
		t.Fatalf("fillEphemeralDefaults not idempotent: %v → %v", before, after)
	}
}
