// feature.cue — wire types for the externalized `charly feature` command plugin
// (candy/plugin-feature; SDD conversion, per the standing operator directive: a
// hand-written wire struct not yet CUE-sourced is conversion-in-progress, never a
// sanctioned exception). NOT authoring kinds (never in #Node/#Op) — pure generated
// host<->plugin wire structs. The command LOGIC (the list/pending/validate
// grammar + output formatting, INCLUDING the plan-to-summary transform:
// keyword/text/agent/check flattening + validatePlanSteps) lives in the plugin
// (kit.KeywordOf / kit.ValidatePlanSteps / deploykit.DescriptionInfo are
// sdk-portable — the plugin calls them directly); the genuine core subsystem it
// can't hold — the unified LOADER (LoadConfig / ScanCandy — the kernel) — stays
// core and is reached via the generic "feature" HostBuild kind: the host loads
// the project and enumerates every entity's RAW description + plan (spec.Step,
// no transform), and the plugin computes summary/steps/validation itself. Plain
// structs — gengotypes generates them faithfully, no disjunction needed.

// #FeatureRequest is the "feature" HostBuild kind request. Filter (empty | a
// kind "candy"/"box" | an entity id "candy:redis") narrows the enumeration.
#FeatureRequest: {
	filter?: string @go(Filter)
}

// #FeatureEntity is one enumerated kind: entity + its RAW plan data (Step is
// already a plain CUE-sourced wire type, so no separate flattened form is
// needed on the wire). An entity with neither a description nor a plan is
// still listed (as "(no description)") but the plugin skips
// summarizing/validating it, matching the former engine.
#FeatureEntity: {
	kind!: string @go(Kind)
	name!: string @go(Name)
	description?: string @go(Description)
	plan?: [...#Step] @go(Plan)
}

// #FeatureReply is the "feature" HostBuild kind reply — the enumerated entities
// the plugin transforms (summary/steps/validation) and formats into the
// list/pending/validate output. Error is a human-facing message on a load
// failure.
#FeatureReply: {
	entities?: [...#FeatureEntity] @go(Entities)
	error?:    string @go(Error)
}
