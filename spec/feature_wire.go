package spec

// feature_wire.go — wire types for the externalized `charly feature` command plugin
// (candy/plugin-feature). The command LOGIC (the list/pending/validate grammar + output formatting)
// lives in the plugin; the genuine core subsystem it can't hold — the unified LOADER (LoadConfig /
// ScanCandy — the kernel), the Step plan model, and validatePlanSteps (shared with `charly box
// validate`, R3) — stays core and is reached via the generic "feature" HostBuild kind: the host loads
// the project, enumerates every entity's plan into plain data, runs the shared validation, and returns
// it for the plugin to present.

// FeatureRequest is the "feature" HostBuild kind request. Filter (empty | a kind "candy"/"box" | an
// entity id "candy:redis") narrows the enumeration.
type FeatureRequest struct {
	Filter string `json:"filter,omitempty"`
}

// FeatureStep is one enumerated plan step flattened to plain data (no core Step model in the plugin).
type FeatureStep struct {
	Index   int    `json:"index"`
	Keyword string `json:"keyword"`
	Text    string `json:"text,omitempty"`
	IsAgent bool   `json:"is_agent,omitempty"`
	IsCheck bool   `json:"is_check,omitempty"`
}

// FeatureEntity is one enumerated kind: entity + its plan data. Summary/Steps/ValidationErrors are
// populated only for entities WITH content (a non-empty description or plan) — an empty candy layer is
// still listed (as "(no description)") but not summarized or validated, matching the former engine.
type FeatureEntity struct {
	Kind             string        `json:"kind"`
	Name             string        `json:"name"`
	Description      string        `json:"description,omitempty"`
	Summary          string        `json:"summary,omitempty"`
	Steps            []FeatureStep `json:"steps,omitempty"`
	ValidationErrors []string      `json:"validation_errors,omitempty"`
}

// FeatureReply is the "feature" HostBuild kind reply — the enumerated entities the plugin formats into
// the list/pending/validate output. Error is a human-facing message on a load failure.
type FeatureReply struct {
	Entities []FeatureEntity `json:"entities,omitempty"`
	Error    string          `json:"error,omitempty"`
}
