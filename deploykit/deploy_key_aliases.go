package deploykit

// deploy_key_aliases.go — thin re-export aliases for the deploy CONFIG/KEY value-vocabulary
// relocated to the spec contract module (#55 import-purity, Cone V). spec now OWNS these pure
// helpers + wire structs; deploykit's own callers, its tests, and the deploy candies keep
// referencing deploykit.X unchanged. The behaviour that CONSUMES them (SaveDeployState, the
// OCI step-emit dispatch) stays in deploykit — only the value vocabulary moved.

import "github.com/opencharly/spec/spec"

// Pure deploy-key / argv / name helpers (stdlib-only, spec-owned).
var (
	// DeployKey builds the charly.yml deployment map key from a box name + optional instance.
	DeployKey = spec.DeployKey
	// ParseDeployKey is the inverse of DeployKey.
	ParseDeployKey = spec.ParseDeployKey
	// FleetDelArgv is the single `charly fleet del <name>` argv builder.
	FleetDelArgv = spec.FleetDelArgv
	// DeriveDeploymentName is the shared default-name derivation for a source-less from-box deploy.
	DeriveDeploymentName = spec.DeriveDeploymentName
)

// Deploy-state / OCI-emit wire structs (CUE-sourced in spec; both marshalled host↔plugin).
type (
	// SaveDeployStateInput holds the parameters SaveDeployState (in this package) persists.
	SaveDeployStateInput = spec.SaveDeployStateInput
	// OCIEmitStepParams is the per-step pod-overlay "oci-emit-step" HostBuild payload.
	OCIEmitStepParams = spec.OCIEmitStepParams
)
