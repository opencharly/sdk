package loaderkit

import "github.com/opencharly/spec/spec"

// repo_identity.go — the repo-identity helpers (NormalizeRepoSpec / RepoIdentity / RootRepoIdentity
// + the git-URL normalizers) are DEFINED in the dedicated spec module (spec/spec/repo_identity.go,
// #55 2b Class A) so charly core reaches them without importing loaderkit. These package-level
// forwarders keep loaderkit's own callers + the candy consumers (candy/plugin-loader's WalkSeams
// defaulting, candy/plugin-build's superproject resolve) terse — same pattern as the ResolveOpts /
// LoaderExecutor relocations.
var (
	NormalizeRepoSpec     = spec.NormalizeRepoSpec
	RepoIdentity          = spec.RepoIdentity
	RootRepoIdentity      = spec.RootRepoIdentity
	GitRemoteIdentity     = spec.GitRemoteIdentity
	NormalizeRepoIdentity = spec.NormalizeRepoIdentity
	NormalizeGitRemoteURL = spec.NormalizeGitRemoteURL
)
