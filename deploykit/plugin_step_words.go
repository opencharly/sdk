package deploykit

import "github.com/opencharly/sdk/spec"

// plugin_step_words.go — the shared word vocabulary for the 12 compiler-emitted InstallStep kinds
// whose pod-overlay build-emit externalized to the compiled-in class:step plugin
// candy/plugin-installstep. Portable (sdk/deploykit), so BOTH the host (charly/provider_step.go's
// checkStepProviderBijection) and the plugin itself (candy/plugin-installstep's "oci-dispatch" word,
// K5-A item 2) consult the SAME map — R3: this used to be a charly-private copy the host's
// oci_step_emit.go dispatch consulted; relocating oci_step_emit.go's dispatch logic into the plugin
// (which needs the identical kind→word lookup to decide who serves a given step) made the map a
// genuine cross-boundary shared constant, not a host-only detail.

// PluginEmitStepWords maps a builtin InstallStep kind to the lowercase-hyphenated class:step plugin
// word that serves its pod-overlay OpEmit (candy/plugin-installstep). These kinds have NO in-proc
// StepProvider — the OCI dispatch routes them here, serializing the step VIEW as the OpEmit payload.
// Their DEPLOY leg is unchanged (sdk/kit.WalkPlans renders them from the same view; reboot's is the
// host-side guest reboot over RunHostStep). apk-install's and reboot's plugin declares Emits=false
// (no build fragment); every other word Emits=true.
var PluginEmitStepWords = map[spec.StepKind]string{
	StepKindFile:            "file",
	StepKindShellHook:       "shell-hook",
	StepKindShellSnippet:    "shell-snippet",
	StepKindServicePackaged: "service-packaged",
	StepKindServiceCustom:   "service-custom",
	StepKindRepoChange:      "repo-change",
	StepKindApkInstall:      "apk-install",
	StepKindReboot:          "reboot",
	StepKindSystemPackages:  "system-packages",
	StepKindBuilder:         "builder",
	StepKindLocalPkgInstall: "local-pkg-install",
	StepKindOp:              "op",
}
