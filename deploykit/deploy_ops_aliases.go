package deploykit

// deploy_ops_aliases.go — thin re-export aliases for the pure deploy-tree / deploy-path /
// candy-stage / preempt-resolve / task-var value HELPERS relocated to the spec contract module
// (#55 import-purity, deploykit D2-clean). spec now OWNS these stdlib-only value helpers; deploykit's
// own callers, its tests, and the deploy candies keep referencing deploykit.X unchanged, while
// charly repoints to deploy.X directly. Mirrors deploy_key_aliases.go (Cone V) — one pattern (R3).

import "github.com/opencharly/spec/deploy"

// Pure deploy-path helpers (stdlib-only, spec-owned).
var (
	// ResolveNodePath resolves a dotted deployment path against a root map.
	ResolveNodePath = deploy.ResolveNodePath
	// SplitDottedPath splits a dotted deployment path into segments (nil on any empty segment).
	SplitDottedPath = deploy.SplitDottedPath
	// PathLeaf returns the last (tolerant) segment of a dotted deployment path.
	PathLeaf = deploy.PathLeaf
	// ClassifyNodeTarget picks the pod|vm|kubernetes|local|android target discriminator for a node.
	ClassifyNodeTarget = deploy.ClassifyNodeTarget
	// BedCheckLiveRefs returns the ordered `charly check live` targets for a bed.
	BedCheckLiveRefs = deploy.BedCheckLiveRefs
	// DescriptionInfo returns the trimmed first line of a candy/box description.
	DescriptionInfo = deploy.DescriptionInfo
	// MergeDeployNode overlays src's authored + structural-tree fields onto dst.
	MergeDeployNode = deploy.MergeDeployNode
	// HostRooted reports whether a node's stamped descent is a host-rooted (local/SSH-shell) venue.
	HostRooted = deploy.HostRooted
	// DeployNestedLocalChildren applies a parent venue's nested target:local children in place.
	DeployNestedLocalChildren = deploy.DeployNestedLocalChildren
)

// Pure candy-stage helpers (spec-owned).
var (
	// CandyMapKey returns a candy's map key (full @github ref for remote, else bare Name).
	CandyMapKey = deploy.CandyMapKey
	// CandyStageDirName returns the content-addressed staging dir name (name.version).
	CandyStageDirName = deploy.CandyStageDirName
)

// Pure preempt-resolve helpers (spec-owned).
var (
	// HolderAddrFor derives the resource-arbiter holder address for a deploy-tree node.
	HolderAddrFor = deploy.HolderAddrFor
	// FindVMClaimant returns the first node claiming a VM entity via requires_exclusive.
	FindVMClaimant = deploy.FindVMClaimant
)

// Pure task-var + artifact helpers (spec-owned).
var (
	// TaskAutoExports are the generator-reserved auto-exported variable names.
	TaskAutoExports = deploy.TaskAutoExports
	// TaskKnownNames returns the ${NAME} references that resolve cleanly for a candy.
	TaskKnownNames = deploy.TaskKnownNames
	// CandyArtifactRegisters returns the DISTINCT `register:` hints across every candy's artifacts.
	CandyArtifactRegisters = deploy.CandyArtifactRegisters
)
