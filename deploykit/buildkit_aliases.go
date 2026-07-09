package deploykit

// buildkit_aliases.go — resolved-image types the deploy-plan compiler reads, homed
// in sdk/buildkit (P3). deploykit→buildkit is acyclic.

import "github.com/opencharly/sdk/buildkit"

type ResolvedBox = buildkit.ResolvedBox
