package vmshared

import "github.com/opencharly/spec/hostenv"

// hostdistro.go — re-export FORWARDERS for the host distro + glibc detection surface, RELOCATED to
// spec/hostenv (#55 vmshared Bucket C). charly core reaches spec/hostenv.* directly (import-purity);
// the plugin/test callers that hold the vmshared.* names (candy/plugin-fleet, candy/plugin-vm) keep
// them via these forwarders. Pure host inspection (os-release / glibc) with zero registry coupling.

// HostDistro re-exports hostenv.HostDistro (its PopulateTags/PrimaryTag/FormatHint methods ride the
// alias). See spec/hostenv/hostdistro.go.
type HostDistro = hostenv.HostDistro

// DetectHostDistro re-exports hostenv.DetectHostDistro. See spec/hostenv/hostdistro.go.
var DetectHostDistro = hostenv.DetectHostDistro

// DetectHostGlibc re-exports hostenv.DetectHostGlibc. See spec/hostenv/hostdistro.go.
var DetectHostGlibc = hostenv.DetectHostGlibc

// CompareGlibc re-exports hostenv.CompareGlibc. See spec/hostenv/hostdistro.go.
var CompareGlibc = hostenv.CompareGlibc
