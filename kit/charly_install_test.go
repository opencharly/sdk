package kit

import "testing"

// TestHostCharlyIsNewer covers the single CalVer arbiter shared by EnsureCharlyInVenue (walk-time,
// generic) + EnsureCharlyInGuest (vm PrepareVenue, host-surface) — moved here from charly core with
// the delivery logic (R3): the venue's PATH charly is kept when at least as new; only an absent/older
// one is overwritten, and an unprovable host version never clobbers.
func TestHostCharlyIsNewer(t *testing.T) {
	cases := []struct {
		name, host, venue string
		want              bool
	}{
		{"host strictly newer", "2026.200.1200", "2026.100.0900", true},
		{"venue newer — keep it", "2026.100.0900", "2026.200.1200", false},
		{"equal — never downgrade", "2026.150.1000", "2026.150.1000", false},
		{"venue absent — host wins", "2026.150.1000", "", true},
		{"venue unparseable — host wins", "2026.150.1000", "unknown", true},
		{"host unparseable — cannot prove, keep venue", "unknown", "2026.150.1000", false},
	}
	for _, c := range cases {
		if got := HostCharlyIsNewer(c.host, c.venue); got != c.want {
			t.Errorf("%s: HostCharlyIsNewer(%q,%q)=%v want %v", c.name, c.host, c.venue, got, c.want)
		}
	}
}
