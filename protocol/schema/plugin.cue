// Authoritative CUE source for Charly's gRPC transport contract.
// proto/plugin.proto and its language bindings are generated artifacts.
package protocol

protocol: {
	"syntax":     "proto3"
	"package":    "charlyplugin"
	"go_package": "github.com/opencharly/sdk/proto"
	"doc": """
		The Charly plugin gRPC contract. One Provider service mirrors the unified
		provider interface; typed protobuf messages carry transport, streaming,
		and handshake control while CUE-generated JSON payloads carry domain data.
		"""
	"messages": [
		{
			"name": "Empty"
		},
		{
			"name": "Capabilities"
			"fields": [
				{
					"name":   "calver"
					"type":   "string"
					"number": 1
					"doc":    "the plugin's CalVer (CalVer is the version authority)"
				},
				{
					"name":   "protocol_version"
					"type":   "uint32"
					"number": 2
					"doc":    "thin secondary gate; never duplicates CalVer"
				},
				{
					"name":     "provided"
					"type":     "ProvidedCapability"
					"number":   3
					"doc":      "the unit's capabilities + the def validating each input"
					"repeated": true
				},
				{
					"name":   "schema_cue"
					"type":   "string"
					"number": 4
					"doc":    "the unit's package-less, SELF-CONTAINED .cue source text"
				},
			]
		},
		{
			"name": "ProvidedCapability"
			"doc":  "ProvidedCapability — one served capability plus the CUE def that validates its"
			"fields": [
				{
					"name":   "class"
					"type":   "string"
					"number": 1
					"doc":    "\"verb\" / \"kind\" / \"deploy\" / \"step\" / \"builder\""
				},
				{
					"name":   "word"
					"type":   "string"
					"number": 2
					"doc":    "the reserved word, e.g. \"externalprobe\""
				},
				{
					"name":   "input_def"
					"type":   "string"
					"number": 3
					"doc":    "the CUE def for this word's plugin_input, e.g. \"#ExternalprobeInput\""
				},
				{
					"name":   "step_contract"
					"type":   "StepContract"
					"number": 4
					"doc":    "set ONLY for class=\"step\" (F3): the plugin-declared install-step contract"
				},
				{
					"name":   "structural"
					"type":   "bool"
					"number": 5
					"doc":    "set ONLY for class=\"kind\" (F5): the kind decodes a STRUCTURAL entity (a spec.Deploy member tree -\u003e uf.Bundle) rather than a FLAT body (-\u003e uf.PluginKinds)"
				},
				{
					"name":   "lifecycle"
					"type":   "bool"
					"number": 6
					"doc":    "set ONLY for class=\"deploy\" (F6): the substrate brings its OWN host-side venue lifecycle (PrepareVenue/Start/Stop/...) served over the lifecycle Ops; the host registers a wire-backed substrateLifecycle for it"
				},
				{
					"name":   "preresolve"
					"type":   "bool"
					"number": 7
					"doc":    "set ONLY for class=\"deploy\" (F6): the substrate declares a host-side PRERESOLVE step (OpPreresolve) the host runs before apply, shipping the opaque result in DeployVenue.Substrate"
				},
				{
					"name":   "validates"
					"type":   "bool"
					"number": 8
					"doc":    "set ONLY for class=\"kind\" (F7/C8): the kind serves a deep OpValidate check (returns Diagnostics) BEYOND the static CUE input-def gate; the host dispatches OpValidate at load"
				},
				{
					"name":   "phase"
					"type":   "string"
					"number": 9
					"doc":    "F9: the plugin lifecycle PHASE (sdk.Phase*; \"\" =\u003e runtime default) — the ordered point at which the kernel loads/invokes the plugin; \"bootstrap\" runs BEFORE config validation/migration"
				},
				{
					"name":   "primary"
					"type":   "string"
					"number": 10
					"doc":    "set ONLY for class=\"verb\": the input field the scalar sugar shorthand targets (`file: /x` -\u003e plugin_input: {\u003cprimary\u003e: \"/x\"}); \"\" =\u003e map input only"
				},
				{
					"name":   "deploy_traits"
					"type":   "DeployTraits"
					"number": 11
					"doc":    "set ONLY for class=\"kind\" (P9): a SUBSTRATE kind's DECLARED deploy behaviour traits, the SINGLE plugin-declared source kit.StampDescent stamps onto node.Descent — the consult sites read the traits off node.Descent instead of switching on the substrate kind word"
				},
				{
					"name":   "command_model_json"
					"type":   "bytes"
					"number": 12
					"doc":    "CUE #CLIModel JSON for class=command; lets CLI and MCP reflect plugin-owned leaves without importing plugin code"
				},
			]
		},
		{
			"name": "DeployTraits"
			"doc":  "DeployTraits — a SUBSTRATE kind's DECLARED deploy behaviour (P9), advertised per substrate"
			"fields": [
				{
					"name":   "venue"
					"type":   "string"
					"number": 1
					"doc":    "\"container\" | \"ssh\" | \"shell\" | \"parent\" | \"none\": how commands physically execute in this substrate's venue"
				},
				{
					"name":   "image_backed"
					"type":   "bool"
					"number": 2
					"doc":    "the substrate runs a baked OCI image (pod)"
				},
				{
					"name":   "image_context"
					"type":   "bool"
					"number": 3
					"doc":    "the substrate composes over an image build context (pod overlay, k8s manifests)"
				},
				{
					"name":   "machine_venue"
					"type":   "bool"
					"number": 4
					"doc":    "the substrate is a full machine with a system init (host/vm/local) — services render as systemd units, not a container init"
				},
				{
					"name":   "exclusive_venue"
					"type":   "bool"
					"number": 5
					"doc":    "the substrate holds an exclusive host-resource lease boundary (vm)"
				},
				{
					"name":   "leaf_only"
					"type":   "bool"
					"number": 6
					"doc":    "the substrate is a deploy-chain LEAF — it cannot be descended into (k8s)"
				},
			]
		},
		{
			"name": "StepContract"
			"doc":  "StepContract — a class=\"step\" plugin's DECLARED install-step contract (F3): where the"
			"fields": [
				{
					"name":   "scope"
					"type":   "string"
					"number": 1
					"doc":    "\"system\" | \"user\" | \"user-profile\""
				},
				{
					"name":   "venue"
					"type":   "int32"
					"number": 2
					"doc":    "Venue enum: 0=host-native, 1=container-builder, 2=skip"
				},
				{
					"name":   "gate"
					"type":   "string"
					"number": 3
					"doc":    "\"\" | \"allow-repo-changes\" | \"allow-root-tasks\" | \"with-services\""
				},
				{
					"name":   "emits"
					"type":   "bool"
					"number": 4
					"doc":    "F-STEP-EMIT: the step produces a build-context Containerfile FRAGMENT (Invoke(OpEmit)); the pod-overlay OCITarget bakes it. false =\u003e deploy-only step (no build fragment; OCITarget skips it, like apk on an image build)"
				},
			]
		},
		{
			"name": "InvokeRequest"
			"fields": [
				{
					"name":   "reserved"
					"type":   "string"
					"number": 1
					"doc":    "the reserved word, e.g. \"exampleprobe\",\"cdp\",\"local\""
				},
				{
					"name":   "op"
					"type":   "string"
					"number": 2
					"doc":    "operation selector for the word's class"
				},
				{
					"name":   "params_json"
					"type":   "bytes"
					"number": 3
					"doc":    "the CUE-generated params (Op for verbs/steps; entity for kinds)"
				},
				{
					"name":   "env_json"
					"type":   "bytes"
					"number": 4
					"doc":    "snapshotCheckEnv / venue descriptor (serializable invocation ctx)"
				},
				{
					"name":   "class"
					"type":   "string"
					"number": 5
					"doc":    "the ProviderClass (\"verb\"/\"kind\"/...) — words aren't unique across classes"
				},
				{
					"name":   "executor_broker_id"
					"type":   "uint32"
					"number": 6
					"doc":    "E3b: the go-plugin broker id the host serves ExecutorService on for a deploy/step/builder op; 0 = none (verb/kind ops need no executor)"
				},
			]
		},
		{
			"name": "InvokeReply"
			"fields": [
				{
					"name":   "result_json"
					"type":   "bytes"
					"number": 1
				},
			]
		},
		{
			"name": "Frame"
			"doc":  "CheckResult / InstallPlan / Diagnostics, JSON"
			"fields": [
				{
					"name":   "result_json"
					"type":   "bytes"
					"number": 1
				},
			]
		},
		{
			"name": "ChannelFrame"
			"doc":  "ChannelFrame is the generic, bidirectional provider process channel. Domain payloads remain CUE-generated JSON; this envelope owns ordering, flow control, terminal data, and lifecycle signals without naming a runtime or transport."
			"fields": [
				{
					"name":   "request_id"
					"type":   "string"
					"number": 1
					"doc":    "stable idempotency/correlation key for the channel"
				},
				{
					"name":   "sequence"
					"type":   "uint64"
					"number": 2
					"doc":    "monotonic sender sequence; zero is reserved for an unsequenced open"
				},
				{
					"name":   "ack_sequence"
					"type":   "uint64"
					"number": 3
					"doc":    "highest contiguous peer sequence consumed"
				},
				{
					"name":   "kind"
					"type":   "string"
					"number": 4
					"doc":    "open|stdin|stdout|stderr|terminal|status|resize|signal|ack|cancel|exit|error|resync"
				},
				{
					"name":   "class"
					"type":   "string"
					"number": 5
					"doc":    "provider class, set on open"
				},
				{
					"name":   "reserved"
					"type":   "string"
					"number": 6
					"doc":    "provider word, set on open"
				},
				{
					"name":   "op"
					"type":   "string"
					"number": 7
					"doc":    "provider operation, set on open"
				},
				{
					"name":   "payload_json"
					"type":   "bytes"
					"number": 8
					"doc":    "CUE-generated domain payload or structured status"
				},
				{
					"name":   "data"
					"type":   "bytes"
					"number": 9
					"doc":    "binary-safe process or terminal bytes; never shell-interpreted"
				},
				{
					"name":   "name"
					"type":   "string"
					"number": 10
					"doc":    "canonical key, signal, stream, or status name"
				},
				{
					"name":   "cols"
					"type":   "uint32"
					"number": 11
				},
				{
					"name":   "rows"
					"type":   "uint32"
					"number": 12
				},
				{
					"name":   "exit_code"
					"type":   "int32"
					"number": 13
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 14
				},
				{
					"name":   "target_json"
					"type":   "bytes"
					"number": 15
					"doc":    "CUE-generated generic target chain, set on open"
				},
				{
					"name":   "replay_from"
					"type":   "uint64"
					"number": 16
					"doc":    "first sequence requested during attach/resynchronization"
				},
			]
		},
		{
			"name": "InvokeProviderRequest"
			"doc":  "InvokeProviderRequest mirrors InvokeRequest minus the broker id (the host already holds the"
			"fields": [
				{
					"name":   "class"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "reserved"
					"type":   "string"
					"number": 2
				},
				{
					"name":   "op"
					"type":   "string"
					"number": 3
				},
				{
					"name":   "params_json"
					"type":   "bytes"
					"number": 4
				},
				{
					"name":   "env_json"
					"type":   "bytes"
					"number": 5
				},
			]
		},
		{
			"name": "HostBuildRequest"
			"doc":  "HostBuildRequest names a registered host-builder `kind` (e.g. \"plugin-binary\", and — added by"
			"fields": [
				{
					"name":   "kind"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "spec_json"
					"type":   "bytes"
					"number": 2
				},
			]
		},
		{
			"name": "HostBuildReply"
			"fields": [
				{
					"name":   "result_json"
					"type":   "bytes"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "HostArbiterRequest"
			"doc":  "C9 resource-arbiter reverse channel (ExecutorService.HostArbiter). action names one of the"
			"fields": [
				{
					"name":   "action"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "params_json"
					"type":   "bytes"
					"number": 2
				},
			]
		},
		{
			"name": "HostArbiterReply"
			"fields": [
				{
					"name":   "result_json"
					"type":   "bytes"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "VenueReply"
			"fields": [
				{
					"name":   "venue"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "RunRequest"
			"fields": [
				{
					"name":   "script"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "opts_json"
					"type":   "bytes"
					"number": 2
				},
			]
		},
		{
			"name": "RunReply"
			"doc":  "opts_json = EmitOpts, JSON"
			"fields": [
				{
					"name":   "error"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "PutFileRequest"
			"doc":  "empty error = success"
			"fields": [
				{
					"name":   "path"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "content"
					"type":   "bytes"
					"number": 2
				},
				{
					"name":   "mode"
					"type":   "uint32"
					"number": 3
				},
				{
					"name":   "owner_root"
					"type":   "bool"
					"number": 4
				},
				{
					"name":   "opts_json"
					"type":   "bytes"
					"number": 5
				},
			]
		},
		{
			"name": "PutFileReply"
			"doc":  "content placed at path; mode = octal perms; opts_json = EmitOpts"
			"fields": [
				{
					"name":   "error"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "CaptureReply"
			"doc":  "empty error = success"
			"fields": [
				{
					"name":   "stdout"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "stderr"
					"type":   "string"
					"number": 2
				},
				{
					"name":   "exit_code"
					"type":   "int32"
					"number": 3
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 4
				},
			]
		},
		{
			"name": "LiveReply"
			"doc":  "error = execution failure, NOT a non-zero exit"
			"fields": [
				{
					"name":   "exit_code"
					"type":   "int32"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "GetFileRequest"
			"doc":  "F12 RunInteractive/RunStream: stdout/stderr/stdin went LIVE to the operator's terminal (host-held), so no buffers — only the session's exit code; error = execution/spawn failure, NOT a non-zero exit (CaptureReply's split, sans buffers)"
			"fields": [
				{
					"name":   "path"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "as_root"
					"type":   "bool"
					"number": 2
				},
				{
					"name":   "opts_json"
					"type":   "bytes"
					"number": 3
				},
			]
		},
		{
			"name": "GetFileReply"
			"fields": [
				{
					"name":   "content"
					"type":   "bytes"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "HostStepRequest"
			"doc":  "step_json = ONE spec.InstallStepView (a HOST-ENGINE step kind — Builder, LocalPkgInstall,"
			"fields": [
				{
					"name":   "step_json"
					"type":   "bytes"
					"number": 1
				},
				{
					"name":   "opts_json"
					"type":   "bytes"
					"number": 2
				},
			]
		},
		{
			"name": "HostStepReply"
			"fields": [
				{
					"name":   "reverse_ops_json"
					"type":   "bytes"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "HTTPDoRequest"
			"doc":  "HTTPDoRequest carries the FULL request + per-request policy the host needs to build the"
			"fields": [
				{
					"name":   "method"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "url"
					"type":   "string"
					"number": 2
				},
				{
					"name":   "body"
					"type":   "bytes"
					"number": 3
				},
				{
					"name":         "headers"
					"type":         "string"
					"number":       4
					"map_key_type": "string"
				},
				{
					"name":   "timeout"
					"type":   "string"
					"number": 5
				},
				{
					"name":   "allow_insecure"
					"type":   "bool"
					"number": 6
				},
				{
					"name":   "no_follow_redirects"
					"type":   "bool"
					"number": 7
				},
				{
					"name":   "ca_pem"
					"type":   "bytes"
					"number": 8
				},
			]
		},
		{
			"name": "HTTPDoReply"
			"doc":  "HTTPDoReply: status + body + response headers, or a transport-level error (the RPC itself"
			"fields": [
				{
					"name":   "status"
					"type":   "int32"
					"number": 1
				},
				{
					"name":   "body"
					"type":   "bytes"
					"number": 2
				},
				{
					"name":   "header_blob"
					"type":   "string"
					"number": 3
					"doc":    "pre-formatted \"Key: value\\n\" blob (multi-value preserved), matcher-ready"
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 4
				},
			]
		},
		{
			"name": "AddBackgroundRequest"
			"fields": [
				{
					"name":   "pid"
					"type":   "int32"
					"number": 1
				},
			]
		},
		{
			"name": "ResolveEndpointRequest"
			"doc":  "ResolveEndpointRequest: the in-venue TCP port to resolve to a host-reachable address."
			"fields": [
				{
					"name":   "port"
					"type":   "int32"
					"number": 1
				},
			]
		},
		{
			"name": "ResolveEndpointReply"
			"doc":  "ResolveEndpointReply: the host-reachable \"host:port\" addr, or a resolution error."
			"fields": [
				{
					"name":   "addr"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "ResolveGraphicsEndpointRequest"
			"doc":  "ResolveGraphicsEndpointRequest: the graphics kind to resolve (\"vnc\" | \"spice\")."
			"fields": [
				{
					"name":   "kind"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "ResolveGraphicsEndpointReply"
			"doc":  "ResolveGraphicsEndpointReply: the dialable endpoint. Exactly one of addr / socket is set;"
			"fields": [
				{
					"name":   "addr"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "socket"
					"type":   "string"
					"number": 2
				},
				{
					"name":   "password"
					"type":   "string"
					"number": 3
				},
				{
					"name":   "skip"
					"type":   "bool"
					"number": 4
				},
				{
					"name":   "skip_message"
					"type":   "string"
					"number": 5
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 6
				},
			]
		},
		{
			"name": "ResolveClusterContextRequest"
			"doc":  "ResolveClusterContextRequest: the charly k8s cluster-profile name to map to a context."
			"fields": [
				{
					"name":   "cluster"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "ResolveClusterContextReply"
			"doc":  "ResolveClusterContextReply: the resolved kubeconfig context (\"\" = no matching profile)."
			"fields": [
				{
					"name":   "context"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
		{
			"name": "ResolveImageLabelRequest"
			"doc":  "ResolveImageLabelRequest: the OCI label name to read (e.g. \"ai.opencharly.mcp_provide\")."
			"fields": [
				{
					"name":   "label"
					"type":   "string"
					"number": 1
				},
			]
		},
		{
			"name": "ResolveImageLabelReply"
			"doc":  "ResolveImageLabelReply: the raw label value (\"\" = label absent on the image)."
			"fields": [
				{
					"name":   "value"
					"type":   "string"
					"number": 1
				},
				{
					"name":   "error"
					"type":   "string"
					"number": 2
				},
			]
		},
	]
	"services": [
		{
			"name": "PluginMeta"
			"doc":  "PluginMeta — read once on connect."
			"methods": [
				{
					"name":     "Describe"
					"request":  "Empty"
					"response": "Capabilities"
				},
			]
		},
		{
			"name": "Provider"
			"doc":  "Provider — the ONE uniform capability service. `reserved` is the reserved word"
			"methods": [
				{
					"name":     "Invoke"
					"request":  "InvokeRequest"
					"response": "InvokeReply"
					"doc":      "single-shot (the common path)"
				},
				{
					"name":             "InvokeStream"
					"request":          "InvokeRequest"
					"response":         "Frame"
					"doc":              "streaming (record/logcat/execute)"
					"server_streaming": true
				},
				{
					"name":             "Channel"
					"request":          "ChannelFrame"
					"response":         "ChannelFrame"
					"doc":              "generic bidirectional provider/process stream with explicit ordering, acknowledgements, cancellation, and resynchronization"
					"client_streaming": true
					"server_streaming": true
				},
			]
		},
		{
			"name": "ExecutorService"
			"doc":  "ExecutorService — the HOST-SERVED reverse channel (E3b). A provider's live"
			"methods": [
				{
					"name":     "Venue"
					"request":  "Empty"
					"response": "VenueReply"
				},
				{
					"name":     "RunSystem"
					"request":  "RunRequest"
					"response": "RunReply"
				},
				{
					"name":     "RunUser"
					"request":  "RunRequest"
					"response": "RunReply"
				},
				{
					"name":     "PutFile"
					"request":  "PutFileRequest"
					"response": "PutFileReply"
					"doc":      "place file content on the venue (binary-safe; owner_root → root:root via sudo)"
				},
				{
					"name":     "RunCapture"
					"request":  "RunRequest"
					"response": "CaptureReply"
					"doc":      "capture stdout/stderr/exit (no root escalation — callers add sudo)"
				},
				{
					"name":     "RunInteractive"
					"request":  "RunRequest"
					"response": "LiveReply"
					"doc":      "F12: run script on the venue wired to the operator's LIVE TTY (the host reverse-server runs IN the charly process, so it inherits os.Stdin/os.Stdout/os.Stderr = the operator's terminal — stdio NEVER crosses the wire, only script→exit; the child podman exec -it / ssh -t owns the PTY + resize + Ctrl-C). Blocks until the session ends; UNARY (host holds the TTY — the hostBuildCli doctrine). Consumers: charly shell (-it), charly cmd (-i)"
				},
				{
					"name":     "RunStream"
					"request":  "RunRequest"
					"response": "LiveReply"
					"doc":      "F12: run script on the venue streaming stdout/stderr LIVE to the operator (inherited os.Stdout/os.Stderr; no stdin). Blocks until exit. UNARY (host holds the terminal). Consumer: charly logs --follow"
				},
				{
					"name":     "GetFile"
					"request":  "GetFileRequest"
					"response": "GetFileReply"
					"doc":      "read a venue file (e.g. a record/screenshot artifact pulled back to the host)"
				},
				{
					"name":     "RunHostStep"
					"request":  "HostStepRequest"
					"response": "HostStepReply"
					"doc":      "run a HOST-ENGINE step (Builder/LocalPkgInstall/SystemPackages/act-OpStep/ExternalPlugin) on the host engine + apply onto the venue"
				},
				{
					"name":     "InvokeProvider"
					"request":  "InvokeProviderRequest"
					"response": "InvokeReply"
					"doc":      "F10 plugin↔plugin: the host resolves another provider by (class,word) + Invokes it on the calling plugin's behalf (threading the SAME venue executor) — the generalization of the RunHostStep ExternalPlugin arm to ANY class/op"
				},
				{
					"name":     "HostBuild"
					"request":  "HostBuildRequest"
					"response": "HostBuildReply"
					"doc":      "F10 host-build: the calling plugin requests a HOST-side build (the build engine stays in core) — the host runs the registered host-builder for `kind` and returns its result"
				},
				{
					"name":     "HostArbiter"
					"request":  "HostArbiterRequest"
					"response": "HostArbiterReply"
					"doc":      "C9 resource-arbiter seams: the COMPILED-IN candy/plugin-preempt (verb:arbiter) calls back mid-logic for its host dependencies (config gather/resources, VM/pod lifecycle running/stop/start, the GPU driver flip switchMode/ensureCDI). action-multiplexed (the GpuProbeInput pattern) — the host runs the seam's in-core default impl and replies"
				},
			]
		},
		{
			"name": "CheckContextService"
			"doc":  "CheckContextService is the host-served reverse channel (F2) for the HOST-COUPLED"
			"methods": [
				{
					"name":     "HTTPDo"
					"request":  "HTTPDoRequest"
					"response": "HTTPDoReply"
				},
				{
					"name":     "AddBackground"
					"request":  "AddBackgroundRequest"
					"response": "Empty"
				},
				{
					"name":     "ResolveEndpoint"
					"request":  "ResolveEndpointRequest"
					"response": "ResolveEndpointReply"
					"doc":      "ResolveEndpoint: resolve the check target's venue (container / VM / ssh / local) and"
				},
				{
					"name":     "ResolveGraphicsEndpoint"
					"request":  "ResolveGraphicsEndpointRequest"
					"response": "ResolveGraphicsEndpointReply"
					"doc":      "ResolveGraphicsEndpoint: resolve a VM's \u003cgraphics type='\u003ckind\u003e'\u003e listener (kind = \"vnc\""
				},
				{
					"name":     "ResolveClusterContext"
					"request":  "ResolveClusterContextRequest"
					"response": "ResolveClusterContextReply"
					"doc":      "ResolveClusterContext: map a charly k8s cluster-profile NAME to its kubeconfig context by"
				},
				{
					"name":     "ResolveImageLabel"
					"request":  "ResolveImageLabelRequest"
					"response": "ResolveImageLabelReply"
					"doc":      "ResolveImageLabel: read one OCI label value off the deployment-under-test's image — the"
				},
			]
		},
	]
}
