package workflowkit

import (
	"testing"
)

// args_test.go — the KIND-BLIND workdir resolution's coverage. The task-shaped helpers
// that used to be tested here (ResolveArgs, MergedEnv) moved HOME to `plugin-task` with
// their implementation (opencharly/spec#304 unit 2); their cases live beside it now.

func TestResolveTaskDir(t *testing.T) {
	env := map[string]string{"SUB": "sub/dir"}
	cases := []struct{ dir, want string }{
		{"", "/proj"},
		{"sub", "/proj/sub"},
		{"/abs", "/abs"},
		{"${SUB}", "/proj/sub/dir"},
	}
	for _, tc := range cases {
		if got := ResolveTaskDir("/proj", tc.dir, env); got != tc.want {
			t.Errorf("ResolveTaskDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}
