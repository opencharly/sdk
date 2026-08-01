package vmshared

import "github.com/opencharly/spec/spec"

// libvirt_uri.go — re-export FORWARDERS for the libvirt-URI parse, RELOCATED to
// spec/spec/libvirt_uri.go (#55 vmshared Bucket B). charly core reaches spec.* directly
// (import-purity); the plugin/test callers that hold the vmshared.* names keep them via these
// forwarders. It is PURE string parsing over the spec SSHTarget value type; the live go-libvirt
// transport that consumes the parsed target stays in the VM behavior layer (plugin-vm / vmshared).

// LibvirtURI re-exports spec.LibvirtURI (its IsLocal method rides the alias). See spec/spec/libvirt_uri.go.
type LibvirtURI = spec.LibvirtURI

// ParseLibvirtURI re-exports spec.ParseLibvirtURI. See spec/spec/libvirt_uri.go.
var ParseLibvirtURI = spec.ParseLibvirtURI
