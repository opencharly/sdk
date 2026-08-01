package kit

// check_step_descriptors.go — re-export of the TYPED-STEP state-provision contract cluster
// for host-coupled check-verb candies, RELOCATED to the spec/checkstep fabric slice
// github.com/opencharly/spec/checkstep/checkstep.go (#55 CHECK-ENGINE cone Option A). The
// step-descriptor types (StepKindName / ServicePackagedDesc / SystemPackagesDesc /
// StepDescriptor) + the OPTIONAL roles a kit candy implements alongside CheckVerbProvider
// (StepProvider, ProvisionActor) + the ResolvePackageName cross-distro resolver live in
// spec/checkstep (its own package — the const IDENTIFIERS StepKindServicePackaged /
// StepKindSystemPackages clash in spec/spec with the internal InstallPlan IR StepKind enum,
// see spec/checkstep/checkstep.go's header), so charly core's in-proc kitVerbAdapter
// (check_kit_adapter.go) references them importing only spec. kit re-exports each here so
// every candy call site (the ~85 plugin candies implementing ProvisionActor / StepProvider, and
// the host step materializer) compiles UNCHANGED. New consumers should import spec/checkstep
// directly.

import "github.com/opencharly/spec/checkstep"

// StepKindName names the TYPED install-plan step a step-providing verb lowers into. Aliased
// to checkstep.StepKindName (the body lives there).
type StepKindName = checkstep.StepKindName

const (
	// StepKindServicePackaged — the `service` verb (enable a packaged unit; load-bearing reversals).
	StepKindServicePackaged = checkstep.StepKindServicePackaged
	// StepKindSystemPackages — the `package` verb (install system packages).
	StepKindSystemPackages = checkstep.StepKindSystemPackages
)

// ServicePackagedDesc is the candy-decodable construction input for a service-packaged step.
// Aliased to checkstep.ServicePackagedDesc.
type ServicePackagedDesc = checkstep.ServicePackagedDesc

// SystemPackagesDesc is the candy-decodable construction input for a system-packages step.
// Aliased to checkstep.SystemPackagesDesc.
type SystemPackagesDesc = checkstep.SystemPackagesDesc

// StepDescriptor is the candy-decodable construction input for a TYPED install-plan step.
// Aliased to checkstep.StepDescriptor.
type StepDescriptor = checkstep.StepDescriptor

// StepProvider is the OPTIONAL third role of a host-coupled verb candy: a verb whose build/deploy
// ACT lowers into a TYPED install-plan step. Aliased to checkstep.StepProvider.
type StepProvider = checkstep.StepProvider

// ProvisionActor is the OPTIONAL second role of a host-coupled verb candy: the do:act renderer
// for a state-provision verb. Aliased to checkstep.ProvisionActor.
type ProvisionActor = checkstep.ProvisionActor

// ResolvePackageName picks the correct package name for the running image's distro. Re-exported
// from checkstep.ResolvePackageName (the body lives there); the single cross-distro name resolver
// shared by the `package` candy's check + act AND the host's step materializer (R3).
var ResolvePackageName = checkstep.ResolvePackageName
