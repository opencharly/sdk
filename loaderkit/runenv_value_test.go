package loaderkit

import (
	"context"
	"testing"

	"github.com/opencharly/spec/proc"
	"github.com/opencharly/spec/spec"
)

// TestRunEnvValue_CtxWinsAndFallsBack pins the repo-override read: a ctx RunEnv value wins, and an
// absent RunEnv falls back to os.Getenv inside spec.RunEnvGet (the legacy host CLI — byte-identical
// to the former plain os.Getenv).
func TestRunEnvValue_CtxWinsAndFallsBack(t *testing.T) {
	t.Setenv(proc.RepoOverrideEnv, "o/r=/legacy")
	if got := runEnvValue(context.Background(), proc.RepoOverrideEnv); got != "o/r=/legacy" {
		t.Fatalf("no-RunEnv = %q, want o/r=/legacy (os.Getenv fallback)", got)
	}
	ctx := spec.WithRunEnv(context.Background(), spec.RunEnv{proc.RepoOverrideEnv: "o/r=/bed"})
	if got := runEnvValue(ctx, proc.RepoOverrideEnv); got != "o/r=/bed" {
		t.Fatalf("RunEnv = %q, want o/r=/bed (ctx must win)", got)
	}
	if got := runEnvValue(context.Background(), "CHARLY_RUNENV_ABSENT_XYZ"); got != "" {
		t.Fatalf("absent = %q, want empty", got)
	}
}
