package kit

import "github.com/opencharly/spec/checkhost"

// vnc_bridge.go — UnixToTCPBridge RELOCATED to the spec fabric slice
// github.com/opencharly/spec/checkhost (#55 CHECK-ENGINE cone Option A — pure host-side networking,
// a check host-vantage primitive). kit re-exports so every existing kit.UnixToTCPBridge call site
// (charly's ssh.go / the VM-VNC endpoint resolution) is untouched.
var UnixToTCPBridge = checkhost.UnixToTCPBridge
