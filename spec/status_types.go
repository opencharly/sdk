package spec

// SubstrateKind identifies which deployment substrate a DeploymentStatus row came
// from — the discriminator that lets `charly status` present pod, VM, k8s, local,
// and android deployments side-by-side from one unified table. The command:status
// plugin (fan-out + render) and every substrate plugin's status-collect Op share
// this type; #DeploymentStatus.kind references it via @go(Kind,type=SubstrateKind).
//
// HAND-WRITTEN — a distinct named string type with the substrate consts, NOT emitted
// by `task cue:gen` (gengotypes references it from the generated DeploymentStatus.Kind
// field but does not define it; a CUE string enum would degrade to a plain string and
// lose the typed consts the collectors switch on).
type SubstrateKind string

const (
	SubstratePod     SubstrateKind = "pod"
	SubstrateVM      SubstrateKind = "vm"
	SubstrateK8s     SubstrateKind = "k8s"
	SubstrateLocal   SubstrateKind = "local"
	SubstrateAndroid SubstrateKind = "android"
)
