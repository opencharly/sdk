package kit

import (
	"fmt"
	"io"
	"net"
	"time"
)

// vnc_bridge.go — P12a follow-up: UnixToTCPBridge relocated from
// charly/vnc_helpers.go. Pure host-side networking (no core state) — shared by
// the VM-VNC endpoint resolution (charly's resolveVerbGraphics) AND the libvirt
// SSH path (charly/ssh.go), both of which stay core (they need live venue /
// executor state) and call kit.UnixToTCPBridge.

// UnixToTCPBridge starts a TCP listener on 127.0.0.1:0 that pipes each accepted
// connection to the named UNIX socket. The returned listener owns a goroutine
// that exits when the listener is closed. Used wherever an RFB client (or any
// TCP-only peer) must reach a UNIX-socket-only service.
func UnixToTCPBridge(socketPath string) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bridge listen: %w", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close() //nolint:errcheck
				u, err := net.DialTimeout("unix", socketPath, 5*time.Second)
				if err != nil {
					return
				}
				defer u.Close() //nolint:errcheck
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(u, conn); done <- struct{}{} }()
				go func() { _, _ = io.Copy(conn, u); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return ln, nil
}
