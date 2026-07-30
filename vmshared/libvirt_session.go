package vmshared

import "github.com/opencharly/spec/hostenv"

// libvirt_session.go — re-export FORWARDER for StartLibvirtUserSession, RELOCATED to spec/hostenv
// (#55 vmshared Bucket C). charly core reaches spec/hostenv.StartLibvirtUserSession directly
// (import-purity); candy/plugin-vm keeps the vmshared.* name via this forwarder AND stubs THIS var in
// its test (vm_backend_resolve_test.go) — self-consistent, since plugin-vm both stubs and calls the
// vmshared forwarder. A pure best-effort host action (systemctl --user / virsh spawn), zero coupling.

// StartLibvirtUserSession re-exports hostenv.StartLibvirtUserSession. Stubbable per-package (a caller
// that stubs THIS var must also call THIS var — plugin-vm does). See spec/hostenv/libvirt_session.go.
var StartLibvirtUserSession = hostenv.StartLibvirtUserSession
