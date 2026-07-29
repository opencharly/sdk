package vmshared

// ssh_target.go — the SSH target vocabulary (SSHTarget + ParseSSHTarget +
// String + CurrentUsername) is a stdlib-only FABRIC primitive and now lives in
// the spec contract module, github.com/opencharly/spec/spec (ssh_target.go,
// #55 step1). vmshared keeps this type alias so its own libvirt_uri.go (and any
// consumer that still names vmshared.SSHTarget) resolves to the single source;
// ParseSSHTarget/CurrentUsername callers repoint to spec.ParseSSHTarget /
// spec.CurrentUsername directly (no re-export — hard cutover).

import "github.com/opencharly/spec/spec"

// SSHTarget aliases spec.SSHTarget — the parsed "[user@]host[:port]" form.
type SSHTarget = spec.SSHTarget
