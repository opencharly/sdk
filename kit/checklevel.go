package kit

// checklevel.go — re-export of the per-box acceptance-depth ladder, RELOCATED to the spec
// contract module github.com/opencharly/spec/spec/checklevel.go (#55 CHECK-ENGINE cone Option
// A — a pure string-ladder classifier candy/plugin-check's bed session (bed_session.go, #55 W3
// B2-full) reaches importing zero kit). kit re-exports the symbols here so every existing kit.CheckLevel*
// / kit.CheckLevelReaches / kit.ResolveCheckLevel / kit.IsValidCheckLevel / kit.DefaultCheckLevel
// call site (charly core + plugins + sdk) is untouched. New consumers should import spec.* directly.

import "github.com/opencharly/spec/spec"

const (
	// CheckLevelNone — skip acceptance entirely.
	CheckLevelNone = spec.CheckLevelNone
	// CheckLevelBuild — build-context ops only (charly check box).
	CheckLevelBuild = spec.CheckLevelBuild
	// CheckLevelNoAgent — build + deploy + runtime act/assert, NO do: instruct (default).
	CheckLevelNoAgent = spec.CheckLevelNoAgent
	// CheckLevelAgent — also run do: instruct steps through the agent grader.
	CheckLevelAgent = spec.CheckLevelAgent
)

// DefaultCheckLevel is the rung applied when a box declares no check_level.
const DefaultCheckLevel = spec.DefaultCheckLevel

// ResolveCheckLevel normalizes an authored check_level to a canonical rung, applying the
// default for the empty value.
var ResolveCheckLevel = spec.ResolveCheckLevel

// IsValidCheckLevel reports whether level is one of the four canonical rungs.
var IsValidCheckLevel = spec.IsValidCheckLevel

// CheckLevelReaches reports whether a box resolved to `have` runs at least as deep as `want`.
var CheckLevelReaches = spec.CheckLevelReaches
