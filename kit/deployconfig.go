package kit

import "github.com/opencharly/spec/spec"

// deployconfig.go — re-export of the per-host deploy-overlay path resolver, RELOCATED to
// spec/spec/deployconfig.go (#55 value extraction). A pure host-path resolver over the
// deploy-config E-envelope, so it homes in spec; kit re-exports it here so existing
// kit.DeployConfigEnv / kit.DefaultDeployConfigPath call sites (plugins + sdk) are untouched.
// New consumers should reference spec.DeployConfigEnv / spec.DefaultDeployConfigPath.
const DeployConfigEnv = spec.DeployConfigEnv

var DefaultDeployConfigPath = spec.DefaultDeployConfigPath
