package sdk

// checkverb_test.go — the OUT-OF-PROCESS check-verb REPLY wire (opencharly/charly#780).
//
// The producer half of the captured-value contract: a host-coupled probe's verdict carries
// spec.CheckVerbResult.CapturedValue (a JSON document, empty when it captured nothing), and the
// reply ServeCheckVerb returns must carry it to the host instead of dropping it. These tests
// drive the reply through the protobuf carrier the go-plugin gRPC transport actually uses and
// decode it with the contract module's OWN decoder (ops.ParseResultJSONFull), so the assertion is
// about the bytes that cross, not about a shape re-declared here (R3: one declaration per wire
// surface — the decoder lives in spec/ops).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/ops"
	pb "github.com/opencharly/spec/proto"
	"google.golang.org/protobuf/proto"
)

// throughTheWire carries a reply across the protobuf hop go-plugin's gRPC channel performs and
// returns what the consumer on the other side sees.
func throughTheWire(t *testing.T, reply *pb.InvokeReply) *pb.InvokeReply {
	t.Helper()
	raw, err := proto.Marshal(reply)
	if err != nil {
		t.Fatalf("wire: marshal: %v", err)
	}
	var back pb.InvokeReply
	if err := proto.Unmarshal(raw, &back); err != nil {
		t.Fatalf("wire: unmarshal: %v", err)
	}
	return &back
}

// TestCheckVerbReplyCarriesTheCapturedValue is the guard for the producer half: the structured
// value an out-of-process probe captured while making its judgement reaches the host, as a JSON
// document a consumer can address by dotted path.
func TestCheckVerbReplyCarriesTheCapturedValue(t *testing.T) {
	verdict := kit.Result{
		Status:        kit.StatusPass,
		Message:       "wl: screencap wrote 67 bytes to /tmp/screencap.png",
		CapturedValue: `{"bytes":67,"path":"/tmp/screencap.png"}`,
	}

	reply, err := checkVerbReply(verdict)
	if err != nil {
		t.Fatalf("checkVerbReply: %v", err)
	}
	status, message, captured, err := ops.ParseResultJSONFull(throughTheWire(t, reply))
	if err != nil {
		t.Fatalf("the wire did not decode: %v", err)
	}
	if status != "pass" || message != verdict.Message {
		t.Fatalf("wire verdict = (%q, %q), want (%q, %q)", status, message, "pass", verdict.Message)
	}
	if len(captured) == 0 {
		t.Fatalf("the capture was DROPPED on the out-of-process wire: the verdict carried %s, the wire carried %s",
			verdict.CapturedValue, reply.GetResultJson())
	}
	var probe struct {
		Bytes int    `json:"bytes"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(captured, &probe); err != nil {
		t.Fatalf("the capture did not cross as a JSON document: %v (%s)", err, captured)
	}
	if probe.Bytes != 67 || probe.Path != "/tmp/screencap.png" {
		t.Fatalf("the capture crossed corrupt: %+v", probe)
	}
}

// TestCheckVerbReplyEmptyCaptureIsByteIdenticalToThePlainWire pins the compatibility guarantee the
// field's omitempty makes: a verdict that captured nothing is byte-identical to the
// pre-extension {status,message} wire, so no existing producer's bytes changed.
func TestCheckVerbReplyEmptyCaptureIsByteIdenticalToThePlainWire(t *testing.T) {
	plain, err := ResultJSON("fail", "adb: screencap: dimensions 1080x2424 < required min 4000x4000")
	if err != nil {
		t.Fatalf("ResultJSON: %v", err)
	}
	withReply, err := checkVerbReply(kit.Result{
		Status:  kit.StatusFail,
		Message: "adb: screencap: dimensions 1080x2424 < required min 4000x4000",
	})
	if err != nil {
		t.Fatalf("checkVerbReply: %v", err)
	}
	if string(withReply.GetResultJson()) != string(plain.GetResultJson()) {
		t.Fatalf("a captureless verdict is not byte-identical:\n got %s\nwant %s",
			withReply.GetResultJson(), plain.GetResultJson())
	}
}

// TestCheckVerbReplyRejectsANonJSONCapture proves the contract is enforced at the producer: a
// verb that stashed prose (or partial JSON) instead of a JSON document is reported as a FAIL
// naming the cause, never emitted as a corrupt captured_value and never silently dropped (R1).
func TestCheckVerbReplyRejectsANonJSONCapture(t *testing.T) {
	reply, err := checkVerbReply(kit.Result{
		Status:        kit.StatusPass,
		Message:       "wl: screencap wrote 67 bytes",
		CapturedValue: "67 bytes written to /tmp/screencap.png",
	})
	if err != nil {
		t.Fatalf("checkVerbReply returned a Go error instead of a verdict-shaped fail: %v", err)
	}
	status, message, captured, err := ops.ParseResultJSONFull(reply)
	if err != nil {
		t.Fatalf("the rejection was not decodable: %v", err)
	}
	if status != "fail" {
		t.Fatalf("a non-JSON capture produced status %q, want %q (it must never pass a corrupt wire)", status, "fail")
	}
	if len(captured) != 0 {
		t.Fatalf("a non-JSON capture still crossed the wire as %s", captured)
	}
	if !strings.Contains(message, "captured value") {
		t.Fatalf("the rejection does not name the cause: %q", message)
	}
}

// TestCheckVerbReplyStatusMapping keeps the verdict's status on the wire for all three arms, so a
// reply that carries a capture can never lose the pass/fail/skip verdict it belongs to.
func TestCheckVerbReplyStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status kit.Status
		want   string
	}{
		{kit.StatusPass, "pass"},
		{kit.StatusFail, "fail"},
		{kit.StatusSkip, "skip"},
	} {
		reply, err := checkVerbReply(kit.Result{Status: tc.status, Message: "x", CapturedValue: `{"k":"v"}`})
		if err != nil {
			t.Fatalf("checkVerbReply(%v): %v", tc.status, err)
		}
		status, _, captured, err := ops.ParseResultJSONFull(throughTheWire(t, reply))
		if err != nil {
			t.Fatalf("decode(%v): %v", tc.status, err)
		}
		if status != tc.want || len(captured) == 0 {
			t.Fatalf("status %v -> wire (%q, captured=%s), want (%q, a capture)", tc.status, status, captured, tc.want)
		}
	}
}

// TestPreFixBuilderDropsTheCapture pins the MECHANISM this change removes, so a future revert of
// checkVerbReply fails visibly instead of quietly restoring the defect: the captureless builder —
// the producer's only return call before checkVerbReply, and still the right builder for a caller
// that captured nothing — keeps status+message and drops the capture.
func TestPreFixBuilderDropsTheCapture(t *testing.T) {
	reply, err := ResultJSON("pass", "wl: screencap wrote 67 bytes")
	if err != nil {
		t.Fatalf("ResultJSON: %v", err)
	}
	_, _, captured, err := ops.ParseResultJSONFull(throughTheWire(t, reply))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(captured) != 0 {
		t.Fatalf("the captureless builder carried a capture (%s) — this test no longer pins the defect", captured)
	}
}
