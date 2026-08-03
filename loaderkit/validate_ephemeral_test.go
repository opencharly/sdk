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
