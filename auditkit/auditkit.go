// Package auditkit is the ONE append-only writer for the audit record DESIGN.md §20.1 declares.
//
// THE CONTRACT IS NOT DEFINED HERE. `spec.AuditEvent`, `spec.AuditDetail` and `spec.Redacted` are
// generated from `opencharly/spec`'s `schema/audit.cue`, and this package consumes them: it holds no
// mirror struct and restates no shape (D-SCH-1). What this package owns is the WRITE PATH — where a
// record goes, that exactly one exists per act, and that nothing can change one afterwards.
//
// THE ROWS THAT SHAPE THIS CODE (not decoration — each decides a line):
//
//   - D-AUDIT-1, "every mutation and every authority decision appends EXACTLY one record". The unit
//     is the ACT, and the caller names it: `Append(act, event)` writes once per act, and a SECOND
//     append for the same act fails loudly with ErrDuplicateAct — neither duplicated (which inflates
//     an audit) nor absorbed (which hides a double-apply). The contract expresses the same rule from
//     the other side: `#AuditAppendInput` is `{event!: #AuditEvent}`, one required, singular event,
//     so the wire has no form that carries zero or two.
//
//     THE ACT ID IS THE PROTOCOL'S `call_id`, not a name invented for this call: every `#Request`
//     carries a `#CallHeader` whose `call_id` is a 128-bit per-invocation id (`#CallId`,
//     `=~"^[0-9a-f]{32}$"`; DESIGN.cue:1144 and :1329-1334), and `audit_append` is a host method
//     (DESIGN.md §4.6), so the wiring is `Append(req.Header.CallId, req.AuditAppendInput.Event)`.
//     No field is needed on `#AuditAppendInput` for it — a second per-invocation identity beside the
//     one the header already mandates would be exactly the duplication that header prevents.
//
//   - D-AUDIT-4, "append-only". A record is ONE immutable file, created `O_CREATE|O_EXCL` and never
//     reopened for writing. The exported surface has no update, delete or truncate, and
//     `TestExportedSurfaceHasNoMutator` asserts that against the package's own source, because "no
//     mutator" is a property of the SURFACE and a comment cannot enforce one.
//
//   - D-AUDIT-3, "a record never holds a secret value". This is STRUCTURAL rather than checked:
//     `spec.Redacted` is `{redacted!: true}` — a bare marker with no field a value could occupy — and
//     a detail's `value` admits nothing but that marker. There is therefore no `ErrSecretValue` here
//     and no name-shape rule: a type with no value-bearing field cannot be the leak, and a string
//     check pretending to guard it would be the weaker mechanism.
//     `TestRedactedIsAnEmptyMarker` proves the absence by enumerating the type's own fields.
//
// WHAT THE WRITER DOES *NOT* VALIDATE, deliberately: the word sets (`event`, `outcome`, detail
// `code`) and the field shapes (`at`, `identity`, `action`, `revision`, `namespace`, the detail
// bound) are the SCHEMA's, and restating them in Go would be the duplication D-SCH-1 forbids — a
// second copy of a rule that drifts from `schema/audit.cue`. The writer refuses exactly what no CUE
// pass can see: the ZERO VALUE of a required field, which a Go caller can hand it because the value
// never passes through the loader.
//
// WHERE THE RECORDS LIVE (D-DEP-5): under the state root, resolved by the ONE resolver —
// storekit.Dir(storekit.RootState), which re-exports the contract's storeroot package — never by
// hand-building an XDG path here. `Open` takes the directory so a test injects its own temporary
// root (D-STORE-8); `OpenDefault` is the only place that resolves the real one.
//
// THE LOCK: D-AUDIT-1's "exactly one" is decided by a read-then-create, so two concurrent appends
// would race between the duplicate check and the create. The serializer is spec/lock — the
// contract's ONE advisory-lock primitive, the same one storekit uses for its index — and its file is
// not a record (List reads `*.json` and nothing else). The create still carries O_EXCL, so even a
// lost lock cannot overwrite a record; it can only lose a race it would have failed anyway.
//
// ORDER: a record's file name carries a zero-padded sequence, so List returns records in append
// order from a directory listing alone — no index file to fall out of step with the records.
//
// THE `value?` MARKER IS OPTIONAL ON THE WIRE, and that is the pinned contract's doing rather than this
// package's. `#AuditDetail.value?` projects as `*Redacted` at the spec version this module requires
// (`v0.2026320.2307`, which adopted opencharly/spec#345), so a detail carrying no marker writes NO
// `value` key at all — `TestRedactedIsAnEmptyMarker` case (c) asserts exactly that.
//
// The history is worth one line because it is why the pin is not incidental: before that fix the field
// projected as a VALUE, and `encoding/json`'s `omitempty` cannot omit a struct, so an absent marker
// serialized as `{"redacted":false}` — asserting "not redacted" on a record that had no marker. It was
// never a leak (`#Redacted` can only hold `true`); it was the `?` lost on the wire. The fix landed in
// the contract's generator, where it belonged, NOT as a second serialization here.
package auditkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencharly/sdk/storekit"
	"github.com/opencharly/spec/lock"
	"github.com/opencharly/spec/spec"
)

const (
	// recordDirMode keeps the audit tree private to the operator who ran the mutation.
	recordDirMode = 0o700
	// recordFileMode is the same privacy per record.
	recordFileMode = 0o600
	// lockWait bounds the wait for the append serializer. A wait this long means a writer died
	// holding the lock; failing loudly beats hanging a real mutation behind it.
	lockWait = 30 * time.Second
	// lockName is the serializer's file. It is not a record, and List never reads it.
	lockName = "append.lock"
	// seqDigits is the width of the sequence prefix that gives List its order.
	seqDigits = 8
)

// The two refusals. Each is a distinct wrapped error so a caller can act on the class rather than on
// a message.
var (
	// ErrDuplicateAct reports a second record for an act that already has one (D-AUDIT-1).
	ErrDuplicateAct = errors.New("audit: this act already has a record (D-AUDIT-1: exactly one per act)")
	// ErrInvalidRecord reports a record that cannot answer the question an audit exists for: an act
	// with no identity, no time, no event, no outcome, or a detail with no code or no text.
	ErrInvalidRecord = errors.New("audit: invalid record")
)

// Appender writes audit events into one directory and reads them back in append order.
//
// It is safe for concurrent use across processes: every append serializes on a file lock and creates
// its record with O_EXCL.
type Appender struct {
	dir string
}

// Open returns an Appender over dir, creating it when absent. Pass an injected directory in a test
// (D-STORE-8); use OpenDefault in production.
func Open(dir string) (*Appender, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("%w: the audit directory is empty", ErrInvalidRecord)
	}
	if err := os.MkdirAll(dir, recordDirMode); err != nil {
		return nil, fmt.Errorf("audit: create %s: %w", dir, err)
	}
	return &Appender{dir: dir}, nil
}

// OpenDefault returns an Appender over the audit directory under the STATE root (D-DEP-5), as the
// contract's ONE root resolver reports it.
func OpenDefault() (*Appender, error) {
	root, err := storekit.Dir(storekit.RootState)
	if err != nil {
		return nil, fmt.Errorf("audit: resolve the state root: %w", err)
	}
	return Open(filepath.Join(root, "audit"))
}

// Dir is the directory this Appender writes to.
func (a *Appender) Dir() string { return a.dir }

// Append writes EXACTLY ONE record for act.
//
// `act` is the act's identity — the protocol's `call_id` for a host method's invocation (see the
// package doc), i.e. a PER-INVOCATION id, never a name. The contract's event carries no id of its
// own, by design: an id would be a field a record could use to point at another record, which
// D-AUDIT-4 forbids. The dedupe key therefore lives in the file name, where it cannot be amended.
//
// Two calls with the same act are a programming error, not a no-op: the second returns
// ErrDuplicateAct, because an audit that silently absorbs a double-append cannot say how many times
// something happened.
//
// An act id must be PER INVOCATION — a command run's id, a request id — never a NAME. A deployment
// name identifies a deployment, not one deploy of it, so using one would make every deploy of that
// name a single act and the second would be refused: loudly and correctly, but for the wrong reason.
func (a *Appender) Append(act string, event spec.AuditEvent) error {
	if err := validate(act, event); err != nil {
		return err
	}
	release, err := lock.AcquireFileLockWithin(filepath.Join(a.dir, lockName), true, lockWait)
	if err != nil {
		return fmt.Errorf("audit: serialize the append: %w", err)
	}
	defer func() { _ = release() }()

	key := actKey(act)
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return fmt.Errorf("audit: read %s: %w", a.dir, err)
	}
	next := 1
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		if strings.HasSuffix(stem, "-"+key) {
			return fmt.Errorf("%w: act %q", ErrDuplicateAct, act)
		}
		if seq, ok := sequence(stem); ok && seq >= next {
			next = seq + 1
		}
	}

	blob, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("audit: encode event: %w", err)
	}
	blob = append(blob, '\n')
	path := filepath.Join(a.dir, fmt.Sprintf("%0*d-%s.json", seqDigits, next, key))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, recordFileMode)
	if err != nil {
		return fmt.Errorf("audit: create %s: %w", path, err)
	}
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()
		return fmt.Errorf("audit: write %s: %w", path, err)
	}
	// Sync before the record is visible as complete: a record that is present but empty on a crash
	// is worse than a record that is absent, because it reads as an act with no outcome.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("audit: sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("audit: close %s: %w", path, err)
	}
	return nil
}

// List returns every record in append order. It is a read path: it opens each record read-only and
// writes nothing.
func (a *Appender) List() ([]spec.AuditEvent, error) {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return nil, fmt.Errorf("audit: read %s: %w", a.dir, err)
	}
	// os.ReadDir sorts by file name, and the name's sequence prefix IS the append order.
	out := make([]spec.AuditEvent, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(a.dir, name))
		if err != nil {
			return nil, fmt.Errorf("audit: read %s: %w", name, err)
		}
		var event spec.AuditEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("audit: decode %s: %w", name, err)
		}
		out = append(out, event)
	}
	return out, nil
}

// validate refuses the records that cannot answer the question an audit exists for. It runs BEFORE
// the lock, so an invalid record costs no contention.
//
// It checks REQUIRED-NESS only — the half of the schema a Go caller can defeat, because the value
// never passes through the CUE pass that would enforce it. Membership in a word set and every field
// shape are the schema's (D-SCH-1: no Go validator restates a CUE rule).
func validate(act string, event spec.AuditEvent) error {
	if strings.TrimSpace(act) == "" {
		return fmt.Errorf("%w: the act's identity is empty (D-AUDIT-1 records one act)", ErrInvalidRecord)
	}
	if strings.TrimSpace(string(event.Identity)) == "" {
		return fmt.Errorf("%w: identity is required (D-AUDIT-2: a record names who acted)", ErrInvalidRecord)
	}
	if strings.TrimSpace(string(event.At)) == "" {
		return fmt.Errorf("%w: at is required (D-AUDIT-1 records an action and its outcome)", ErrInvalidRecord)
	}
	if strings.TrimSpace(event.Event) == "" {
		return fmt.Errorf("%w: event is required (D-AUDIT-1: what kind of act it was)", ErrInvalidRecord)
	}
	if strings.TrimSpace(event.Outcome) == "" {
		return fmt.Errorf("%w: outcome is required (D-AUDIT-1: how it ended, including a refusal)", ErrInvalidRecord)
	}
	for i, d := range event.Detail {
		if strings.TrimSpace(d.Code) == "" || strings.TrimSpace(string(d.Text)) == "" {
			return fmt.Errorf("%w: detail[%d] carries no code or no text", ErrInvalidRecord, i)
		}
	}
	return nil
}

// actKey is the stable, filesystem-safe identity of an act in a record's file name.
//
// 128 bits, not 64: the truncation is the only part of this key that could collide by ACCIDENT (the
// birthday bound for 8 bytes is about 4 billion records; for 16 it is beyond any audit). With it
// widened, "two acts, one key" can only mean the caller reused an act id — which is the intended
// semantics of D-AUDIT-1's idempotence, not a hash defect.
func actKey(act string) string {
	sum := sha256.Sum256([]byte(act))
	return hex.EncodeToString(sum[:16])
}

// sequence reads the zero-padded prefix of a record's file name stem.
func sequence(stem string) (int, bool) {
	dash := strings.IndexByte(stem, '-')
	if dash <= 0 {
		return 0, false
	}
	seq, err := strconv.Atoi(stem[:dash])
	if err != nil || seq <= 0 {
		return 0, false
	}
	return seq, true
}
