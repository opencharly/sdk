package kit

import "github.com/opencharly/spec/hostenv"

// runtime_config.go — re-export of the user-level runtime configuration subsystem, RELOCATED to
// spec/hostenv/runtime_config.go (#55 host-env fabric extraction). Resolving engine / run-mode /
// storage paths from env > config-file > defaults is a HOST-ENVIRONMENT primitive, homed in
// spec/hostenv. kit re-exports the types + funcs so the ~23 kit.ResolveRuntime / kit.RuntimeConfig
// / kit.ResolvedRuntime / … call sites (charly core + plugins) are untouched.
//
// NOTE: the two test-injection SEAM vars — RuntimeConfigPath and SystemdUserRuntimeDir — are
// DELIBERATELY NOT re-exported. A live var that callers REASSIGN cannot be `var X = hostenv.X`
// aliased (that copies the value, so a reassignment through kit would never reach hostenv's own
// reader). Every reader/writer references hostenv.RuntimeConfigPath / hostenv.SystemdUserRuntimeDir
// directly (charly host_build_hostprobe.go, plugin-settings, and the test-injection sites).
type (
	RuntimeConfig   = hostenv.RuntimeConfig
	RuntimeVmConfig = hostenv.RuntimeVmConfig
	EngineConfig    = hostenv.EngineConfig
	ResolvedRuntime = hostenv.ResolvedRuntime
)

var (
	LoadRuntimeConfig           = hostenv.LoadRuntimeConfig
	SaveRuntimeConfig           = hostenv.SaveRuntimeConfig
	ResolveRuntime              = hostenv.ResolveRuntime
	ResolveValue                = hostenv.ResolveValue
	ValidateEngine              = hostenv.ValidateEngine
	ValidateRunMode             = hostenv.ValidateRunMode
	DetectRunMode               = hostenv.DetectRunMode
	SystemdUserAvailable        = hostenv.SystemdUserAvailable
	ValidateBindAddress         = hostenv.ValidateBindAddress
	ResolveEncryptedStoragePath = hostenv.ResolveEncryptedStoragePath
	ResolveVolumesPath          = hostenv.ResolveVolumesPath
	ExpandHostHome              = hostenv.ExpandHostHome
	ResolveAutoEnable           = hostenv.ResolveAutoEnable
)
