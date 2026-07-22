// settings.cue — wire types for the externalized `charly settings` command plugin
// (candy/plugin-settings; SDD conversion, per the standing operator directive: a
// hand-written wire struct not yet CUE-sourced is conversion-in-progress, never a
// sanctioned exception). NOT authoring kinds (never in #Node/#Op) — pure generated
// host<->plugin wire structs. The command LOGIC (the get/set/list/reset/path
// subcommand grammar + output) lives in the plugin; the config subsystem
// (read/write ~/.config/charly/config.yml + the credential store + engine/runtime
// resolution) stays in core and is reached via the generic "settings" HostBuild
// kind: the plugin sends the requested op and gets back the resolved value /
// entries to present. Plain structs — gengotypes generates them faithfully, no
// disjunction/inexpressibility escape needed.

// #SettingsRequest is the "settings" HostBuild kind request: one config-subsystem
// op. Op ∈ {get, set, list, reset, path}. Key/Value carry the op's arguments
// (get/reset: Key; set: Key+Value; list/path: neither; reset with empty Key
// resets all).
#SettingsRequest: {
	op!:    string @go(Op)
	key?:   string @go(Key)
	value?: string @go(Value)
}

// #SettingsEntry is one resolved config key (the `charly settings list` row).
#SettingsEntry: {
	key!:    string @go(Key)
	value!:  string @go(Value)
	source!: string @go(Source) // "env" | "config" | "default"
}

// #SettingsReply is the "settings" HostBuild kind reply: Value for get/path,
// Entries for list; set/reset return neither. Error is a human-facing message on
// failure (e.g. an unknown config key).
#SettingsReply: {
	value?:   string @go(Value)
	entries?: [...#SettingsEntry] @go(Entries)
	error?:   string @go(Error)
}
