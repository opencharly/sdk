package workflowkit

import (
	"testing"

	"github.com/opencharly/spec/spec"
)

func TestResolveArgs(t *testing.T) {
	task := spec.Task{
		Params: map[string]spec.TaskParamSpec{
			"who":  {Default: "world"},
			"why":  {Required: true},
			"bare": {},
		},
	}

	got, err := ResolveArgs(task, map[string]string{"why": "because", "extra": "passthrough"})
	if err != nil {
		t.Fatalf("ResolveArgs: %v", err)
	}
	if got["who"] != "world" {
		t.Errorf("default not applied: %v", got)
	}
	if got["why"] != "because" {
		t.Errorf("override not applied: %v", got)
	}
	if got["extra"] != "passthrough" {
		t.Errorf("undeclared arg not passed through: %v", got)
	}
	if _, ok := got["bare"]; ok {
		t.Errorf("an optional param with no default must not appear: %v", got)
	}

	if _, err := ResolveArgs(task, nil); err == nil {
		t.Error("ResolveArgs accepted a missing required param")
	}
}

func TestResolveArgsOverridesDefault(t *testing.T) {
	task := spec.Task{Params: map[string]spec.TaskParamSpec{"who": {Default: "world"}}}
	got, err := ResolveArgs(task, map[string]string{"who": "you"})
	if err != nil {
		t.Fatal(err)
	}
	if got["who"] != "you" {
		t.Errorf("CLI override lost to the default: %v", got)
	}
}

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

func TestMergedEnvPrecedence(t *testing.T) {
	task := spec.Task{
		Vars: map[string]string{"K": "var", "V": "var"},
		Env:  map[string]string{"K": "env"},
	}
	got := MergedEnv(task, map[string]string{"K": "arg"})
	if got["K"] != "arg" {
		t.Errorf("args must win: %v", got)
	}
	if got["V"] != "var" {
		t.Errorf("vars lost: %v", got)
	}
}
