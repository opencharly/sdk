package sdk

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
)

// verb.go carries the shared helpers every OUT-OF-PROCESS check-verb provider uses: the
// {status,message} reply builder and the required-modifier check. They are the byte-identical
// boilerplate the EXEC-based verb plugins (dbus/record/wl) formerly each carried, hoisted here
// so the transport-invisible verb-serving surface has ONE home (R3).

// resultWire is the {status,message} wire form every out-of-process check verb returns (the
// host's pluginCheckResult). status ∈ "pass" | "fail" | "skip".
type resultWire struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ResultJSON builds the InvokeReply an out-of-process check verb's Invoke returns — the SAME
// {status,message} shape every verb plugin (and ServeCheckVerb) emits (R3).
func ResultJSON(status, msg string) (*pb.InvokeReply, error) {
	j, err := json.Marshal(resultWire{Status: status, Message: msg})
	if err != nil {
		return nil, err
	}
	return &pb.InvokeReply{ResultJson: j}, nil
}

// OpModifierZero reports whether the modifier named `name` — its yaml WIRE KEY on spec.Op —
// is absent (zero) on op. It is the ONE generic replacement for the ~10 copy-pasted per-plugin
// modifierZero funcs (R3): reflection over spec.Op's fields keyed by their yaml tag, so a
// modifier name ("tab", "key", "x2", …) maps to the field carrying that tag (op.Tab, op.KeyName,
// op.X2, …) and IsZero handles every field type uniformly (string "" / int 0). An unknown name
// is treated as absent. The tag→name equivalence is asserted by TestOpModifierZeroMatchesFields.
func OpModifierZero(op *spec.Op, name string) bool {
	if op == nil {
		return true
	}
	v := reflect.ValueOf(*op)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("yaml")
		if idx := strings.IndexByte(key, ','); idx >= 0 {
			key = key[:idx]
		}
		if key == name {
			return v.Field(i).IsZero()
		}
	}
	return true
}

// RequireModifiers verifies every modifier a method requires is present on op, using the
// generic OpModifierZero — so a plugin keeps ONLY its requiredModifiers map (genuinely per-verb
// data) and drops both its copy-pasted modifierZero func and the CheckRequiredModifiers wrapper
// call (R3). It is CheckRequiredModifiers bound to OpModifierZero.
func RequireModifiers(method string, op *spec.Op, required map[string][]string) error {
	return CheckRequiredModifiers(method, op, required, OpModifierZero)
}

// CheckRequiredModifiers verifies every modifier a method requires is present on op, returning
// a "missing required modifier(s): …" error naming the absent ones. required maps a method
// name to its required modifier field names, and isZero reports whether a named modifier is
// absent (zero) on op. RequireModifiers binds isZero to the generic OpModifierZero; this lower
// form stays for a verb whose zero-semantics genuinely differ from plain reflection (R3).
func CheckRequiredModifiers(method string, op *spec.Op, required map[string][]string, isZero func(op *spec.Op, name string) bool) error {
	var missing []string
	for _, f := range required[method] {
		if isZero(op, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required modifier(s): %s", strings.Join(missing, ", "))
}
