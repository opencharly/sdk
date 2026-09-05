package sdk

import (
	"context"
	"testing"

	"github.com/opencharly/spec/spec"
)

// TestCheckContextInvokeProvider pins the one-dial contract: a compiled-in/out-of-process
// check context MUST implement the InvokeProvider surface the session/service verbs submit
// through (the compile-time assertion fails if the interface method is ever removed - the
// B12 guard the validator asks for). The behavioral path is executed live by the wave's
// spice session E2E probe (plan-step spice session start through cc.InvokeProvider).
var _ spec.CheckContext = &sdkCheckContext{}

func TestCheckContextInvokeProvider(t *testing.T) {
	ctx := context.Background()
	testCtx := &sdkCheckContext{}
	// A nil exec must fail cleanly (the wire dispatch path is exercised - it reaches
	// exec.InvokeProvider; with no executor this errors rather than panicking or skipping).
	if _, err := testCtx.InvokeProvider(ctx, "verb", "session", "start", nil, nil); err == nil {
		t.Fatal("InvokeProvider with nil exec: want an error, got nil")
	}
}
