package deploykit

// state_aliases.go — deploykit bindings onto the spec/vmshared config types the
// deploy STATE-MODEL helpers (deploy_state.go) reference.

import (
	"github.com/opencharly/sdk/spec"
	"github.com/opencharly/sdk/vmshared"
)

type (
	PreemptibleConfig = spec.PreemptibleConfig
	InstallOptsConfig = vmshared.InstallOptsConfig
)

const (
	PreemptStopShutdown  = spec.PreemptStopShutdown
	PreemptRestoreAlways = spec.PreemptRestoreAlways
)
