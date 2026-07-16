package kit

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestUnixToTCPBridge proves the bridge pipes bytes written on the TCP side
// through to a UNIX-socket listener and back (P12a: no equivalent test existed
// in charly/vnc_helpers.go — this is new coverage for the relocated function).
func TestUnixToTCPBridge(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "bridge.sock")
	uln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("unix listen: %v", err)
	}
	defer uln.Close() //nolint:errcheck

	echoDone := make(chan struct{})
	go func() {
		defer close(echoDone)
		conn, err := uln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		buf := make([]byte, 5)
		if _, err := conn.Read(buf); err != nil {
			return
		}
		_, _ = conn.Write(buf)
	}()

	tln, err := UnixToTCPBridge(sockPath)
	if err != nil {
		t.Fatalf("UnixToTCPBridge: %v", err)
	}
	defer tln.Close() //nolint:errcheck

	conn, err := net.DialTimeout("tcp", tln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := make([]byte, 5)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(out); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("bridge echo = %q, want %q", out, "hello")
	}
	<-echoDone
}
