package deploykit

// plugin_step_words.go — thin re-export. PluginEmitStepWords RELOCATED to
// github.com/opencharly/spec/spec (install_step_vocab.go, #55 step-4), beside the fixed
// step-kind vocabulary (AllStepKinds) it belongs to. BOTH the host
// (charly/provider_step.go's checkStepProviderBijection) and the plugin
// (candy/plugin-installstep) consult the SAME map — now spec.PluginEmitStepWords (R3).
// deploykit's own code + out-of-tree plugins read the alias unchanged.

import "github.com/opencharly/spec/spec"

var PluginEmitStepWords = spec.PluginEmitStepWords
