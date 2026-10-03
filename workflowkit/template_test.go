package workflowkit

import (
	"encoding/json"
	"testing"
)

func TestExpandVars(t *testing.T) {
	t.Setenv("WORKFLOWKIT_TEST_UNSET", "")
	env := map[string]string{"NAME": "from-env", "WHO": "it's ok"}
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"${NAME}", "from-env"},
		{"$NAME", "from-env"},
		{"hi ${NAME}", "hi from-env"},
		{`echo "P=${NAME}"`, `echo "P=from-env"`},
		{"${WHO}", "it's ok"},
		{"${MISSING_VAR_XYZ}", ""},
	}
	for _, tc := range cases {
		if got := ExpandVars(tc.in, env); got != tc.want {
			t.Errorf("ExpandVars(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLobsterArgsEnv(t *testing.T) {
	env := LobsterArgsEnv(map[string]string{"who": "world", "my-arg": "x"})
	if env["LOBSTER_ARG_WHO"] != "world" {
		t.Errorf("LOBSTER_ARG_WHO = %q", env["LOBSTER_ARG_WHO"])
	}
	if env["LOBSTER_ARG_MY_ARG"] != "x" {
		t.Errorf("LOBSTER_ARG_MY_ARG = %q", env["LOBSTER_ARG_MY_ARG"])
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(env["LOBSTER_ARGS_JSON"]), &got); err != nil {
		t.Fatalf("LOBSTER_ARGS_JSON: %v", err)
	}
	if got["who"] != "world" || got["my-arg"] != "x" {
		t.Errorf("LOBSTER_ARGS_JSON = %v", got)
	}
}

func TestLobsterArgName(t *testing.T) {
	for in, want := range map[string]string{
		"who":     "WHO",
		"my-arg":  "MY_ARG",
		"a.b":     "A_B",
		"already": "ALREADY",
		"n1":      "N1",
	} {
		if got := LobsterArgName(in); got != want {
			t.Errorf("LobsterArgName(%q) = %q, want %q", in, got, want)
		}
	}
}
