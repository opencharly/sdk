package buildkit

import (
	"fmt"
	"time"
)

// calver.go — the canonical CalVer build-tag computation (K3 build-engine, U6). Relocated from charly
// core (charly/version.go) so BOTH charly core AND candy/plugin-build (running the build-engine RESOLVE
// plugin-side, which stamps the tag when the host leaves it empty) read the ONE source (R3). charly's
// package-main ComputeCalVer/ComputeCalVerAt now delegate here.

// ComputeCalVer returns the canonical build tag for the current UTC instant.
func ComputeCalVer() string {
	return ComputeCalVerAt(time.Now().UTC())
}

// ComputeCalVerAt formats t as the canonical CalVer: 4-digit year, 3-digit zero-padded day-of-year,
// 4-digit zero-padded HHMM. Every component is fixed-width, so a plain lexicographic sort of CalVer
// strings is chronological.
func ComputeCalVerAt(t time.Time) string {
	year := t.Year()
	dayOfYear := t.YearDay()
	hhmm := t.Hour()*100 + t.Minute()
	return fmt.Sprintf("%04d.%03d.%04d", year, dayOfYear, hhmm)
}
