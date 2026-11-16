package auditkit_test

// The three D-AUDIT rows that shape the code, and the test for each:
//
//	D-AUDIT-1  every mutation and every authority decision appends EXACTLY ONE record —
//	           a second append for the same act FAILS LOUDLY (ErrDuplicateAct).
//	D-AUDIT-4  append-only: the exported surface has NO update, delete or truncate, proven by
//	           enumerating the package's exported funcs and asserting the set.
//	D-AUDIT-3  a record never holds a secret value — and with the merged contract that is
//	           STRUCTURAL: spec.Redacted is a bare marker, so the test proves the TYPE cannot hold
//	           a value rather than proving a validator refuses one.
//
// D-STORE-8 is the fourth constraint: every test uses an INJECTED root (t.TempDir) and writes
// nothing outside it, except the one case that asserts OpenDefault's resolution, which sets
// XDG_STATE_HOME to a temporary directory rather than reading the operator's real one.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/opencharly/sdk/auditkit"
	"github.com/opencharly/spec/spec"
)

// event builds a valid record: the four fields the contract marks `!` that this writer can set
// (`at`, `event`, `outcome`, `identity`) — `target`, `action`, `namespace`, `revision` and `detail`
// are optional, so nothing else is required for a record to be valid.
func event(kind string) spec.AuditEvent {
	return spec.AuditEvent{
		At:       spec.Timestamp("2026-11-17T00:00:00Z"),
		Event:    kind,
		Outcome:  "ok",
		Identity: spec.Ref("charly"),
	}
}

// D-AUDIT-1: exactly one record per act.
func TestAppendWritesExactlyOneRecordPerAct(t *testing.T) {
	a, err := auditkit.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := a.Append("deploy-1", event("mutate")); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	// The SAME act again must fail loudly, not duplicate and not overwrite — and the key is the ACT,
	// not the payload, so a retry that brings a new timestamp is refused just the same.
	retry := event("mutate")
	retry.At = spec.Timestamp("2026-11-17T00:00:01Z")
	err = a.Append("deploy-1", retry)
	if !errors.Is(err, auditkit.ErrDuplicateAct) {
		t.Fatalf("second Append for the same act = %v, want ErrDuplicateAct", err)
	}

	got, err := a.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("records after a duplicated act = %d, want exactly 1", len(got))
	}
	if got[0].At != spec.Timestamp("2026-11-17T00:00:00Z") {
		t.Fatalf("the stored record was overwritten by the retry: at = %q", got[0].At)
	}
}

// D-AUDIT-1 covers "every authority decision", including a DENIED one: a refused act is recorded,
// with its reason in a detail (the contract keeps the message there, not in `outcome`).
func TestAppendRecordsARefusalWithItsReason(t *testing.T) {
	a, err := auditkit.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	refused := event("authorize")
	refused.Outcome = "refused"
	refused.Detail = []spec.AuditDetail{{Code: "reason", Text: spec.Text("gpu lease held by another session")}}
	if err := a.Append("authorize-1", refused); err != nil {
		t.Fatalf("Append(a refused act): %v", err)
	}
	got, err := a.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Outcome != "refused" {
		t.Fatalf("refused act not recorded verbatim: %+v", got)
	}
	if len(got[0].Detail) != 1 || got[0].Detail[0].Text != spec.Text("gpu lease held by another session") {
		t.Fatalf("the refusal's reason did not survive: %+v", got[0].Detail)
	}
}

// The read path returns records in APPEND order, which is what makes the file an audit rather than
// a set.
func TestListReturnsRecordsInAppendOrder(t *testing.T) {
	a, err := auditkit.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i, kind := range []string{"mutate", "authorize", "teardown"} {
		if err := a.Append("act-"+string(rune('a'+i)), event(kind)); err != nil {
			t.Fatalf("Append(%q): %v", kind, err)
		}
	}
	got, err := a.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e.Event)
	}
	if want := []string{"mutate", "authorize", "teardown"}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("append order = %v, want %v", kinds, want)
	}
}

// D-AUDIT-3, re-expressed for the merged contract: the marker is the whole mechanism, so the test
// proves (a) the TYPE has no field a value could occupy, and (b) a detail's value is the marker when
// present — there is no path by which a value reaches a record.
func TestRedactedIsAnEmptyMarker(t *testing.T) {
	// (a) STRUCTURAL: spec.Redacted must be exactly one bool field. If the contract ever grows a
	// value-bearing field, this fails and D-AUDIT-3 needs re-examining — which is the point.
	rt := reflect.TypeOf(spec.Redacted{})
	if rt.Kind() != reflect.Struct || rt.NumField() != 1 {
		t.Fatalf("spec.Redacted = %v with %d field(s), want a 1-field struct (the bare marker)", rt, rt.NumField())
	}
	f := rt.Field(0)
	if f.Name != "Redacted" || f.Type.Kind() != reflect.Bool {
		t.Fatalf("spec.Redacted's field = %s %v, want the boolean marker `Redacted bool` — a non-bool field could hold a value", f.Name, f.Type)
	}

	// (b) BEHAVIOURAL: a detail that says a value was there carries the marker on disk, and the
	// marker is the ONLY content of `value`.
	dir := t.TempDir()
	a, err := auditkit.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	withMarker := event("mutate")
	withMarker.Detail = []spec.AuditDetail{{
		Code:  "field",
		Text:  spec.Text("api_token"),
		Value: &spec.Redacted{Redacted: true},
	}}
	if err := a.Append("mutate-1", withMarker); err != nil {
		t.Fatalf("Append: %v", err)
	}
	raw, err := os.ReadFile(singleRecord(t, dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"redacted": true`) {
		t.Fatalf("the marker is not on disk:\n%s", raw)
	}
	var decoded spec.AuditDetail
	if err := json.Unmarshal(rawDetail(t, raw), &decoded); err != nil {
		t.Fatalf("decode the detail: %v", err)
	}
	if decoded.Value == nil || !decoded.Value.Redacted {
		t.Fatalf("the marker did not round-trip: %+v", decoded.Value)
	}

	// (c) THE OPTIONAL MARK IS REALLY OPTIONAL. `spec v0.2026320.2307` generates `Value *Redacted`,
	// so a detail that references NO value carries no `value` key at all. Before that tag the field
	// was a value and this key appeared as {"redacted":false} — an absent reference asserting itself.
	bareDir := t.TempDir()
	bare, err := auditkit.Open(bareDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	noValue := event("mutate")
	noValue.Detail = []spec.AuditDetail{{Code: "reason", Text: spec.Text("no secret referenced")}}
	if err := bare.Append("mutate-bare", noValue); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rawBare, err := os.ReadFile(singleRecord(t, bareDir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(rawBare), `"value"`) {
		t.Fatalf("a detail with no marker still serializes a value key:\n%s", rawBare)
	}
}

// D-AUDIT-2 plus the required marks: a record names WHO acted, and the five `!` fields cannot be
// empty — the half a Go caller can defeat, because the value never passes through the CUE pass.
func TestRequiredFieldsAreRefusedWhenEmpty(t *testing.T) {
	a, err := auditkit.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for name, mutate := range map[string]func(*spec.AuditEvent){
		"identity": func(e *spec.AuditEvent) { e.Identity = "" },
		"at":       func(e *spec.AuditEvent) { e.At = "" },
		"event":    func(e *spec.AuditEvent) { e.Event = "" },
		"outcome":  func(e *spec.AuditEvent) { e.Outcome = "" },
		"detail":   func(e *spec.AuditEvent) { e.Detail = []spec.AuditDetail{{Code: "reason"}} },
	} {
		e := event("mutate")
		mutate(&e)
		if err := a.Append("act-"+name, e); !errors.Is(err, auditkit.ErrInvalidRecord) {
			t.Fatalf("%s empty: err = %v, want ErrInvalidRecord", name, err)
		}
	}
	if err := a.Append("  ", event("mutate")); !errors.Is(err, auditkit.ErrInvalidRecord) {
		t.Fatalf("blank act: err = %v, want ErrInvalidRecord", err)
	}
}

// The record lives under the STATE root (D-DEP-5), resolved by the ONE resolver — not built by hand
// here.
func TestOpenDefaultResolvesTheStateRoot(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	a, err := auditkit.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if want := filepath.Join(state, "charly", "audit"); a.Dir() != want {
		t.Fatalf("OpenDefault dir = %q, want %q (the state root, D-DEP-5)", a.Dir(), want)
	}
}

// The bytes on disk ARE the contract type: an audit a reader cannot decode against spec is a private
// format, and a private format drifts.
func TestOnDiskRecordDecodesAsTheContractType(t *testing.T) {
	dir := t.TempDir()
	a, err := auditkit.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := event("mutate")
	want.Action = spec.Word("build")
	want.Target = spec.Ref("box/cachyos")
	want.Revision = spec.Digest("sha256:" + strings.Repeat("a", 64))
	want.Detail = []spec.AuditDetail{{
		Code:  "reason",
		Text:  spec.Text("ok"),
		Value: &spec.Redacted{Redacted: true},
	}}
	if err := a.Append("mutate-1", want); err != nil {
		t.Fatalf("Append: %v", err)
	}
	raw, err := os.ReadFile(singleRecord(t, dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got spec.AuditEvent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("the record is not JSON decodable as spec.AuditEvent: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip =\n%+v\nwant\n%+v", got, want)
	}
}

// D-AUDIT-4: append-only is a property of the SURFACE, so it is proven against the surface — every
// exported function and method of the package, read from its own source.
func TestExportedSurfaceHasNoMutator(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgDir := filepath.Dir(thisFile)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", pkgDir, err)
	}
	top, methods := map[string]bool{}, map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, isFn := decl.(*ast.FuncDecl)
				if !isFn || fn.Name == nil || !fn.Name.IsExported() {
					continue
				}
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					rt := fn.Recv.List[0].Type
					if star, isStar := rt.(*ast.StarExpr); isStar {
						rt = star.X
					}
					if id, isID := rt.(*ast.Ident); isID {
						methods[id.Name+"."+fn.Name.Name] = true
					}
					continue
				}
				top[fn.Name.Name] = true
			}
		}
	}

	// (a) The set is ASSERTED, not merely scanned: a symbol added later fails this test until its
	// author states what it is and why it cannot amend a record.
	wantTop := []string{"Open", "OpenDefault"}
	wantMethods := []string{"Appender.Append", "Appender.Dir", "Appender.List"}
	gotTop, gotMethods := keys(top), keys(methods)
	if !reflect.DeepEqual(gotTop, wantTop) {
		t.Fatalf("exported funcs = %v, want %v — a new exported function on an append-only package is a review obligation (D-AUDIT-4)", gotTop, wantTop)
	}
	if !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("exported methods = %v, want %v — a new exported method on an append-only package is a review obligation (D-AUDIT-4)", gotMethods, wantMethods)
	}

	// (b) The scan itself, with its own failure message: no exported name may carry a mutating verb,
	// even one the allowlist would have to grow to admit.
	forbidden := regexp.MustCompile(`(?i)(update|delete|remove|truncate|purge|replace|rewrite|amend|overwrite|erase|drop|clear|reset|compact|rotate|set|put)`)
	for _, name := range append(append([]string{}, gotTop...), gotMethods...) {
		if forbidden.MatchString(name) {
			t.Fatalf("exported %q carries a mutating verb — the record is append-only (D-AUDIT-4)", name)
		}
	}
}

// singleRecord returns the one *.json record in dir.
func singleRecord(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) != 1 {
		t.Fatalf("record files = %d, want 1", len(files))
	}
	return files[0]
}

// rawDetail extracts the first object of the record's `detail` array, so the marker can be decoded
// on its own.
func rawDetail(t *testing.T, record []byte) []byte {
	t.Helper()
	var envelope struct {
		Detail []json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(record, &envelope); err != nil {
		t.Fatalf("decode the record: %v", err)
	}
	if len(envelope.Detail) == 0 {
		t.Fatalf("the record carries no detail: %s", record)
	}
	return envelope.Detail[0]
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
