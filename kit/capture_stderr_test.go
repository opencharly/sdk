package kit

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and returns whatever was written.
// A shared kit test helper (used by http_fetch_race_test.go + agent_forward_test.go to assert
// loud-warning behavior). Given its own file (#55 U5) when tunnel_metadata_test.go — the dead
// tunnel-config duplicate it had been co-located with — was retired; the helper is generic and
// outlives that file.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	return buf.String()
}
