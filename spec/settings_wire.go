package spec

// settings_wire.go — wire types for the externalized `charly settings` command plugin
// (candy/plugin-settings). The command LOGIC (the get/set/list/reset/path subcommand grammar + output)
// lives in the plugin; the config subsystem (read/write ~/.config/charly/config.yml + the credential
// store + engine/runtime resolution) stays in core and is reached via the generic "settings" HostBuild
// kind: the plugin sends the requested op and gets back the resolved value / entries to present.

// SettingsRequest is the "settings" HostBuild kind request: one config-subsystem op. Op ∈
// {get, set, list, reset, path}. Key/Value carry the op's arguments (get/reset: Key; set: Key+Value;
// list/path: neither; reset with empty Key resets all).
type SettingsRequest struct {
	Op    string `json:"op"`
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// SettingsEntry is one resolved config key (the `charly settings list` row).
type SettingsEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"` // "env" | "config" | "default"
}

// SettingsReply is the "settings" HostBuild kind reply: Value for get/path, Entries for list; set/reset
// return neither. Error is a human-facing message on failure (e.g. an unknown config key).
type SettingsReply struct {
	Value   string          `json:"value,omitempty"`
	Entries []SettingsEntry `json:"entries,omitempty"`
	Error   string          `json:"error,omitempty"`
}
