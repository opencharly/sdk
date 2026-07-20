package deploykit

import "testing"

// ephemeral_id_test.go — relocated 1:1 from charly/ephemeral_classification_test.go
// (P13-KERNEL fold-in) alongside the RenderNamingPattern/NewEphemeralID move, with
// IDENTICAL assertions — a byte-diff gate proving the logic is unchanged pre/post move.

// TestRenderNamingPattern covers the template rendering for instance
// names.
func TestRenderNamingPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		source  string
		id      string
		want    string
		wantErr bool
	}{
		{name: "default pattern", pattern: "{{.Source}}-eph-{{.UUID6}}", source: "arch-test", id: "abcdef", want: "arch-test-eph-abcdef"},
		{name: "literal", pattern: "fixed", source: "x", id: "y", want: "fixed"},
		{name: "bad template", pattern: "{{.Bad", source: "x", id: "y", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderNamingPattern(tt.pattern, tt.source, tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNewEphemeralID verifies the ID is six hex characters.
func TestNewEphemeralID(t *testing.T) {
	id, err := NewEphemeralID()
	if err != nil {
		t.Fatalf("NewEphemeralID error: %v", err)
	}
	if len(id) != 6 {
		t.Errorf("ID length = %d, want 6 (got %q)", len(id), id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("non-hex character in ID %q: %c", id, c)
		}
	}
	// Two successive IDs should differ overwhelmingly often.
	id2, _ := NewEphemeralID()
	if id == id2 {
		t.Errorf("two consecutive IDs match (statistically vanishing probability): %q == %q", id, id2)
	}
}
