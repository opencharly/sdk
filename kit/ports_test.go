package kit

import "testing"

// TestParsePublishedPort covers ParsePublishedPort, the shared host "ip:port"
// normalizer behind the port-protocol verbs' venue resolution (P12a: relocated
// from charly/check_venue_test.go's parsePublishedPort coverage).
func TestParsePublishedPort(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{"standard localhost binding", "127.0.0.1:5900\n", "127.0.0.1:5900", false},
		{"all interfaces binding", "0.0.0.0:5900\n", "127.0.0.1:5900", false},
		{"random high port", "0.0.0.0:49900\n", "127.0.0.1:49900", false},
		{"ipv6 binding", "[::]:5900\n", "127.0.0.1:5900", false},
		{"multiple lines", "0.0.0.0:5900\n[::]:5900\n", "127.0.0.1:5900", false},
		{"no trailing newline", "127.0.0.1:5900", "127.0.0.1:5900", false},
		{"empty output", "", "", true},
		{"only whitespace", "  \n", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePublishedPort(tt.output, 5900)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePublishedPort() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParsePublishedPort() = %q, want %q", got, tt.want)
			}
		})
	}
}
