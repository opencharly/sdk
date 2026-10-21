package workflowkit

import (
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
