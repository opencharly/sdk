package kit

// check_step_descriptors.go — re-export of the TYPED-STEP state-provision contract cluster
// for host-coupled check-verb candies, RELOCATED to the spec/checkstep fabric slice
// github.com/opencharly/spec/checkstep/checkstep.go (#55 CHECK-ENGINE cone Option A). The
// OPTIONAL roles a kit candy implements alongside CheckVerbProvider (StepProvider,
// ProvisionActor) + the ResolvePackageName cross-distro resolver live in spec/checkstep (its
// own package), so charly core's in-proc kitVerbAdapter (check_kit_adapter.go) references them
// importing only spec. kit re-exports each here so a candy that NAMES the roles compiles
// against the same identifier. New consumers should import spec/checkstep directly.
//
// C7: StepProvider is the CANDY-owned typed-step lowering — StepKind() returns the internal
// InstallPlan IR spec.StepKind directly, and MaterializeStep builds the real spec.InstallStep
// from the op's plugin_input + the four host-resolved ctx scalars. There is no candy-local
// kind-name or neutral descriptor for core to map, so core keeps no per-kind switch.

import "github.com/opencharly/spec/checkstep"

// StepProvider is the OPTIONAL third role of a host-coupled verb candy: a verb whose build/deploy
// ACT lowers into a TYPED install-plan step (the candy owns both the kind mapping and the
// materialization). Aliased to checkstep.StepProvider.
type StepProvider = checkstep.StepProvider

// ProvisionActor is the OPTIONAL second role of a host-coupled verb candy: the do:act renderer
// for a state-provision verb. Aliased to checkstep.ProvisionActor.
type ProvisionActor = checkstep.ProvisionActor

// ResolvePackageName picks the correct package name for the running image's distro. Re-exported
// from checkstep.ResolvePackageName (the body lives there); the single cross-distro name resolver
// shared by the `package` candy's check + act + step materializer (R3).
var ResolvePackageName = checkstep.ResolvePackageName
