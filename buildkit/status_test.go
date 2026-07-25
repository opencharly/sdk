package buildkit

import (
	"testing"

	"github.com/opencharly/sdk/spec"
)

// status_test.go — coverage for the candy/box maturity-rung helpers relocated from
// charly/generate.go (status.go). Each case pins the REAL status vocabulary
// (working/testing/broken) and its severity ORDERING (working < testing < broken); any of
// these functions returning a wrong severity or defaulting the wrong way would flip a
// candy-chain worst-status computation silently, so the assertions check exact values, not
// just "no panic".

func TestResolveStatus(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to testing", "", StatusTesting},
		{"working passes through", StatusWorking, StatusWorking},
		{"testing passes through", StatusTesting, StatusTesting},
		{"broken passes through", StatusBroken, StatusBroken},
		{"unknown word passes through verbatim", "bogus", "bogus"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveStatus(tc.in); got != tc.want {
				t.Errorf("ResolveStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCandyStatus(t *testing.T) {
	t.Run("nil CandyReader defaults to testing", func(t *testing.T) {
		if got := CandyStatus(nil); got != StatusTesting {
			t.Errorf("CandyStatus(nil) = %q, want %q", got, StatusTesting)
		}
	})

	cases := []struct {
		name   string
		status string
		want   string
	}{
		{"unset status defaults to testing", "", StatusTesting},
		{"working status honored", StatusWorking, StatusWorking},
		{"broken status honored", StatusBroken, StatusBroken},
		{"testing status honored", StatusTesting, StatusTesting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reader spec.CandyReader = &fakeCandyReader{status: tc.status}
			if got := CandyStatus(reader); got != tc.want {
				t.Errorf("CandyStatus(status=%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestStatusSeverity(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"working is least severe", StatusWorking, 0},
		{"testing is mid severity", StatusTesting, 1},
		{"broken is most severe", StatusBroken, 2},
		{"empty defaults to testing severity", "", 1},
		{"unknown word treated as testing severity", "made-up", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StatusSeverity(tc.in); got != tc.want {
				t.Errorf("StatusSeverity(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
	// Pin the ORDERING invariant WorstStatus relies on: broken > testing > working.
	if StatusSeverity(StatusBroken) <= StatusSeverity(StatusTesting) ||
		StatusSeverity(StatusTesting) <= StatusSeverity(StatusWorking) {
		t.Fatal("StatusSeverity ordering invariant broken > testing > working does not hold")
	}
}

func TestWorstStatus(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want string
	}{
		{"broken beats working", StatusBroken, StatusWorking, StatusBroken},
		{"broken beats working (args swapped)", StatusWorking, StatusBroken, StatusBroken},
		{"testing beats working", StatusTesting, StatusWorking, StatusTesting},
		{"working stays working when both working", StatusWorking, StatusWorking, StatusWorking},
		{"broken beats testing", StatusBroken, StatusTesting, StatusBroken},
		{"equal severities keep the first arg (ResolveStatus'd)", StatusTesting, StatusTesting, StatusTesting},
		{"empty a defaults to testing, still beaten by broken b", "", StatusBroken, StatusBroken},
		{"empty b defaults to testing, beats working a", StatusWorking, "", StatusTesting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorstStatus(tc.a, tc.b); got != tc.want {
				t.Errorf("WorstStatus(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
