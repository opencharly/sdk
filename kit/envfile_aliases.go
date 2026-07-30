package kit

// envfile_aliases.go — kit re-exports the .env / process-env resolution helpers,
// relocated to the fabric slice github.com/opencharly/spec/hostenv (#55, the
// existing host-environment slice already homing ExpandHostHome + the runtime
// config, which envfile's ResolveEnvVars depends on). These aliases keep every
// existing kit.X caller (charly main, candy/plugin-deploy-pod, candy/plugin-settings)
// compiling unchanged (R3, single source).

import "github.com/opencharly/spec/hostenv"

var (
	LoadProcessDotenv = hostenv.LoadProcessDotenv
	ParseEnvFile      = hostenv.ParseEnvFile
	ParseEnvBytes     = hostenv.ParseEnvBytes
	LoadWorkspaceEnv  = hostenv.LoadWorkspaceEnv
	ResolveEnvVars    = hostenv.ResolveEnvVars
	EnrichNoProxy     = hostenv.EnrichNoProxy
	DotenvLoaded      = hostenv.DotenvLoaded
)
