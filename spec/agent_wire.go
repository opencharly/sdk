package spec

// agent_wire.go — the OpResolve envelope for the agent de-type (Cutover E).
// candy/plugin-agent resolves an authored Agent (an AI-CLI grader-catalog entry)
// into an AgentExecSpec: a GENERIC "launch + version-capture + iterate-poll a
// long-running CLI" descriptor the kernel's check/iterate harness consumes WITHOUT
// importing the concrete `agent` kind. The kernel stores agent bodies opaquely and
// the agent plugin owns the authored schema + default application.

// AgentResolveInput is candy/plugin-agent's OpResolve input: the opaque agent
// bodies (the name-keyed AI-CLI catalog) + the selected agent name ("" picks the
// sole entry, or errors when several are configured).
type AgentResolveInput struct {
	Agents map[string]RawBody `json:"agents,omitempty"`
	Name   string             `json:"name,omitempty"`
}

// AgentResolveReply is the OpResolve reply: the resolved exec spec + the chosen
// agent's catalog name.
type AgentResolveReply struct {
	Spec *AgentExecSpec `json:"spec,omitempty"`
	Name string         `json:"name,omitempty"`
}

// AgentExecSpec is a fully-resolved long-running-CLI invocation descriptor — the
// resolve-to-envelope form of an agent, with Go-level defaults applied by the
// plugin (Timeout, PromptVia). Kind-agnostic: it describes HOW to launch a CLI,
// capture its version, and poll it in a plateau-bounded loop — not "agent-kind"
// knowledge.
type AgentExecSpec struct {
	Command                      []string          `json:"command,omitempty"`
	PromptVia                    string            `json:"prompt_via,omitempty"`
	VersionCommand               []string          `json:"version_command,omitempty"`
	Timeout                      Duration          `json:"timeout,omitempty"`
	Env                          StrMap            `json:"env,omitempty"`
	WorkingDir                   string            `json:"working_dir,omitempty"`
	Credential                   []CredentialMount `json:"credential,omitempty"`
	ProgressCheckInterval        Duration          `json:"progress_check_interval,omitempty"`
	ProgressNoImprovementTimeout Duration          `json:"progress_no_improvement_timeout,omitempty"`
	OutputFormat                 string            `json:"output_format,omitempty"`
}
