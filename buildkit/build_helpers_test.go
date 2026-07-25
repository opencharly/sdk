package buildkit

import (
	"reflect"
	"testing"
)

// TestNormalizeBoxArgs asserts the `all` sentinel collapses to nil ONLY when it is the sole
// argument — the canonical "every enabled box" shape shared by `charly box build` and `charly
// box generate` (relocated from charly/box_selection_test.go with the BUILD-cone cutover).
func TestNormalizeBoxArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil stays nil", nil, nil},
		{"empty stays empty", []string{}, []string{}},
		{"lone all → nil", []string{"all"}, nil},
		{"lone ALL (case-insensitive) → nil", []string{"ALL"}, nil},
		{"lone All → nil", []string{"All"}, nil},
		{"single named box passes through", []string{"fedora"}, []string{"fedora"}},
		{"all alongside another name is literal", []string{"all", "fedora"}, []string{"all", "fedora"}},
		{"two named boxes pass through", []string{"fedora", "arch"}, []string{"fedora", "arch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeBoxArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeBoxArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestResolvePodmanJobs verifies the config-driven --jobs capping logic (relocated from
// charly/build_jobs_test.go). The cap is sourced from defaults.podman_jobs_cap (passed as
// jobsCap); a jobsCap of 0 falls back to PodmanJobsCapFallback. The helper must:
//   - honor an explicit override (>0) verbatim, ignoring cap + ncpu
//   - when no override: return min(NumCPU(), cap)
//   - treat jobsCap < 1 as PodmanJobsCapFallback
func TestResolvePodmanJobs(t *testing.T) {
	origNumCPU := NumCPU
	defer func() { NumCPU = origNumCPU }()

	cases := []struct {
		name     string
		override int
		jobsCap  int
		ncpu     int
		want     int
	}{
		{"override wins over large ncpu + cap", 8, 4, 16, 8},
		{"override wins over small ncpu", 1, 8, 16, 1},
		{"override wins regardless of cap", 12, 8, 16, 12},
		{"no override, configured cap 8, ncpu above cap", 0, 8, 16, 8},
		{"no override, configured cap 8, ncpu below cap returns ncpu", 0, 8, 4, 4},
		{"no override, configured cap 2 below ncpu", 0, 2, 16, 2},
		{"no override, cap 0 falls back to PodmanJobsCapFallback", 0, 0, 16, PodmanJobsCapFallback},
		{"no override, cap negative falls back", 0, -1, 16, PodmanJobsCapFallback},
		{"no override, cap 8 but ncpu 1", 0, 8, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			NumCPU = func() int { return tc.ncpu }
			got := ResolvePodmanJobs(tc.override, tc.jobsCap)
			if got != tc.want {
				t.Errorf("ResolvePodmanJobs(%d, %d) with ncpu=%d = %d, want %d",
					tc.override, tc.jobsCap, tc.ncpu, got, tc.want)
			}
		})
	}
}
