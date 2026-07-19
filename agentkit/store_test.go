package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencharly/sdk/spec"
)

func TestStoreRejectsInvalidCUERecordsBeforeMutation(t *testing.T) {
	validID := NewID
	tests := []struct {
		name string
		sub  string
		put  func(*Store) error
	}{
		{
			name: "team",
			sub:  "teams",
			put: func(store *Store) error {
				return store.PutTeam(spec.AgentTeamRecord{
					ID: validID(), Team: spec.AgentTeam{}, CreatedAt: "now",
				})
			},
		},
		{
			name: "session",
			sub:  "sessions",
			put: func(store *Store) error {
				return store.PutSession(spec.AgentSession{
					ID: validID(), Runtime: "pi", State: "unknown", CreatedAt: "now", UpdatedAt: "now",
				})
			},
		},
		{
			name: "run-put",
			sub:  "runs",
			put: func(store *Store) error {
				return store.PutRun(spec.AgentRunRequest{
					ID: validID(), SessionID: validID(), RequestID: validID(),
				})
			},
		},
		{
			name: "run-create-once",
			sub:  "runs",
			put: func(store *Store) error {
				_, _, err := store.CreateRunOnce(spec.AgentRunRequest{
					ID: validID(), SessionID: validID(), RequestID: validID(),
				})
				return err
			},
		},
		{
			name: "event",
			sub:  "events",
			put: func(store *Store) error {
				return store.AppendEvent(spec.AgentEvent{RunID: validID(), Sequence: 1, Type: "unknown", Time: "now"})
			},
		},
		{
			name: "terminal",
			sub:  "terminal",
			put: func(store *Store) error {
				_, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: validID(), Sequence: 1, Kind: "unknown"})
				return err
			},
		},
		{
			name: "federation",
			sub:  "federation",
			put: func(store *Store) error {
				return store.PutFederation(spec.AgentFederationRecord{
					ID: validID(), Node: "box-a", Owner: "operator", State: "unknown", UpdatedAt: "now",
				})
			},
		},
		{
			name: "incident",
			sub:  "incidents",
			put: func(store *Store) error {
				return store.PutIncident(spec.Incident{
					ID: validID(), State: "unknown", Summary: "failure", CreatedAt: "now",
				})
			},
		},
		{
			name: "rca",
			sub:  "rcas",
			put: func(store *Store) error {
				return store.PutRCA(spec.RCARecord{ID: validID(), IncidentID: validID(), State: "unknown"})
			},
		},
		{
			name: "recovery",
			sub:  "recoveries",
			put: func(store *Store) error {
				return store.PutRecovery(spec.RecoveryDecision{
					ID: validID(), IncidentID: validID(), Action: "unknown", State: "planned", DecidedAt: "now",
				})
			},
		},
		{
			name: "abort",
			sub:  "runs",
			put: func(store *Store) error {
				return store.RequestAbort(spec.AgentAbortControl{
					RunID: validID(), RequestID: spec.UUIDv7("not-a-uuid"), RequestedAt: "now",
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := test.put(store); err == nil {
				t.Fatal("invalid generated record was persisted")
			}
			entries, err := os.ReadDir(filepath.Join(store.Dir, test.sub))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("invalid write mutated %s: %v", test.sub, entries)
			}
		})
	}
}

func TestStoreRejectsMalformedLookupIDsWithoutFilenameAliasing(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bad := spec.UUIDv7("bad/id")
	checks := []struct {
		name string
		read func() error
	}{
		{"session", func() error { _, err := store.Session(bad); return err }},
		{"events", func() error { _, err := store.Events(bad); return err }},
		{"terminal", func() error { _, err := store.TerminalFrames(bad); return err }},
		{"abort", func() error { _, err := store.AbortRequested(bad); return err }},
		{"clear-abort", func() error { return store.ClearAbort(bad) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.read()
			if err == nil || !strings.Contains(err.Error(), "#UUIDv7") {
				t.Fatalf("malformed lookup error = %v, want #UUIDv7 validation", err)
			}
		})
	}
	for _, sub := range []string{"sessions", "events", "terminal", "runs"} {
		if _, err := os.Stat(filepath.Join(store.Dir, sub, "invalid.json")); !os.IsNotExist(err) {
			t.Fatalf("malformed ID aliased to %s/invalid.json: %v", sub, err)
		}
		if _, err := os.Stat(filepath.Join(store.Dir, sub, "invalid.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("malformed ID aliased to %s/invalid.jsonl: %v", sub, err)
		}
		if _, err := os.Stat(filepath.Join(store.Dir, sub, "invalid.abort")); !os.IsNotExist(err) {
			t.Fatalf("malformed ID aliased to %s/invalid.abort: %v", sub, err)
		}
	}
}

func TestStoreRejectsCorruptPersistedRecordsAtReadIngress(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := NewID()
	corrupt := spec.AgentSession{ID: id, Runtime: "pi", State: "unknown", CreatedAt: "now", UpdatedAt: "now"}
	data, err := json.Marshal(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "sessions", string(id)+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Session(id); err == nil || !strings.Contains(err.Error(), "#AgentSession") {
		t.Fatalf("corrupt direct read error = %v, want #AgentSession validation", err)
	}
	if _, err := store.Sessions(); err == nil || !strings.Contains(err.Error(), "#AgentSession") {
		t.Fatalf("corrupt list error = %v, want #AgentSession validation", err)
	}
}

func TestStoreRejectsRecordWhoseIDDoesNotMatchFilename(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantID, contentID := NewID(), NewID()
	record := spec.AgentSession{
		ID: contentID, Runtime: "pi", State: "new", CreatedAt: "now", UpdatedAt: "now",
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "sessions", string(wantID)+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Session(wantID); err == nil || !strings.Contains(err.Error(), "does not match filename") {
		t.Fatalf("mismatched record error = %v", err)
	}
}

func TestStoreRejectsCorruptAndCrossRunJSONLAtReadIngress(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	otherRunID := NewID()
	event, err := json.Marshal(spec.AgentEvent{RunID: otherRunID, Sequence: 1, Type: "started", Time: "now"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "events", string(runID)+".jsonl"), append(event, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Events(runID); err == nil || !strings.Contains(err.Error(), "does not match filename") {
		t.Fatalf("cross-run event error = %v", err)
	}

	frame, err := json.Marshal(spec.TerminalFrame{RunID: runID, Sequence: 1, Kind: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, "terminal", string(runID)+".jsonl"), append(frame, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalFrames(runID); err == nil || !strings.Contains(err.Error(), "#TerminalFrame") {
		t.Fatalf("corrupt terminal frame error = %v", err)
	}
}

func TestStoreRejectsMalformedTerminalJSONAtReadIngress(t *testing.T) {
	tests := []struct {
		name    string
		payload func(spec.UUIDv7) string
		want    string
	}{
		{
			name: "invalid-base64",
			payload: func(runID spec.UUIDv7) string {
				return fmt.Sprintf(
					`{"run_id":%q,"sequence":1,"kind":"raw","data":%q}`,
					runID,
					"%%%",
				)
			},
			want: "base64",
		},
		{
			name: "unknown-field",
			payload: func(runID spec.UUIDv7) string {
				return fmt.Sprintf(`{"run_id":%q,"sequence":1,"kind":"status","unexpected":true}`, runID)
			},
			want: "unknown field",
		},
		{
			name: "fractional-sequence",
			payload: func(runID spec.UUIDv7) string {
				return fmt.Sprintf(`{"run_id":%q,"sequence":1.5,"kind":"status"}`, runID)
			},
			want: "cannot unmarshal number 1.5",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			runID := NewID()
			path := filepath.Join(store.Dir, "terminal", string(runID)+".jsonl")
			if err := os.WriteFile(path, []byte(test.payload(runID)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.TerminalFrames(runID); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("terminal ingress error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestStoreRejectsEmptyIdempotencyLookup(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FindIdempotency(""); err == nil || !strings.Contains(err.Error(), "empty idempotency key") {
		t.Fatalf("empty idempotency lookup error = %v", err)
	}
}

func TestStoreWaitAbortUsesCrossProcessNotification(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	control := spec.AgentAbortControl{RunID: NewID(), RequestID: NewID(), RequestedAt: "now"}
	result := make(chan *spec.AgentAbortControl, 1)
	errs := make(chan error, 1)
	go func() {
		got, err := store.WaitAbort(ctx, control.RunID)
		result <- got
		errs <- err
	}()
	if err := store.RequestAbort(control); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if got := <-result; got == nil || got.RequestID != control.RequestID {
		t.Fatalf("abort notification = %#v", got)
	}
}

func TestStorePersistsOrderedTerminalEvidence(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	first, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "raw", Stream: "terminal", Data: []byte{0, 1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	code := 7
	second, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "exit", ExitCode: &code})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d, %d", first.Sequence, second.Sequence)
	}
	frames, err := store.TerminalFrames(runID)
	if err != nil || len(frames) != 2 {
		t.Fatalf("frames = %#v, %v", frames, err)
	}
	if !bytes.Equal(frames[0].Data, []byte{0, 1, 2}) || frames[1].ExitCode == nil || *frames[1].ExitCode != 7 {
		t.Fatalf("terminal evidence did not round trip: %#v", frames)
	}
}

func TestStoreRejectsTerminalSequenceGapBeforeAppend(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	if _, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Sequence: 2, Kind: "status"}); err == nil {
		t.Fatal("terminal evidence sequence gap was appended")
	}
	frames, err := store.TerminalFrames(runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("sequence-gap append mutated transcript: %#v", frames)
	}
}

func TestStoreAcceptsExplicitTerminalResynchronizationGap(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID := NewID()
	if _, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Sequence: 3, Kind: "resync", Snapshot: "recovered"}); err != nil {
		t.Fatal(err)
	}
	next, err := store.AppendTerminalFrame(spec.TerminalFrame{RunID: runID, Kind: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != 4 {
		t.Fatalf("sequence after resync = %d, want 4", next.Sequence)
	}
}

func TestStorePersistsIdempotentRunsAndOrderedEvents(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runID, sessionID, requestID := NewID(), NewID(), NewID()
	run := spec.AgentRunRequest{ID: runID, SessionID: sessionID, RequestID: requestID, IdempotencyKey: "same-request"}
	if err := store.PutRun(run); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.FindIdempotency("same-request")
	if err != nil || !ok || got.ID != runID {
		t.Fatalf("find = %#v, %v, %v", got, ok, err)
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 1, Type: "started", Time: "now"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 3, Type: "failed", Time: "now"}); err == nil {
		t.Fatal("sequence gap accepted")
	}
	if err := store.AppendEvent(spec.AgentEvent{RunID: runID, Sequence: 2, Type: "completed", Time: "now"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(runID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestStoreCreateRunOnceIsAtomicAcrossControllers(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const controllers = 16
	results := make(chan spec.AgentRunRequest, controllers)
	var wg sync.WaitGroup
	for range controllers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run := spec.AgentRunRequest{ID: NewID(), SessionID: NewID(), RequestID: NewID(), IdempotencyKey: "one"}
			got, _, err := store.CreateRunOnce(run)
			if err != nil {
				t.Errorf("CreateRunOnce: %v", err)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	var id spec.UUIDv7
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID != id {
			t.Errorf("different run IDs: %s and %s", id, result.ID)
		}
	}
}

func TestStorePersistsTeamAuthorityAndCrossProcessAbort(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	team := spec.AgentTeamRecord{
		ID: NewID(),
		Team: spec.AgentTeam{
			Agents: []spec.AgentTeamMember{{Name: "lead", Runtime: "pi"}, {Name: "reviewer", Runtime: "tmux"}},
			Edges:  []spec.AgentDelegationEdge{{From: "lead", To: "reviewer", Allow: []string{"run"}}},
		},
		Sessions:  map[string]spec.UUIDv7{"lead": NewID(), "reviewer": NewID()},
		CreatedAt: "now",
	}
	if err := store.PutTeam(team); err != nil {
		t.Fatal(err)
	}
	got, err := store.Team(team.ID)
	if err != nil || got.Sessions["reviewer"] != team.Sessions["reviewer"] {
		t.Fatalf("team = %#v, %v", got, err)
	}

	control := spec.AgentAbortControl{RunID: NewID(), RequestID: NewID(), RequestedAt: "now"}
	if err := store.RequestAbort(control); err != nil {
		t.Fatal(err)
	}
	pending, err := store.AbortRequested(control.RunID)
	if err != nil || pending == nil || pending.RequestID != control.RequestID {
		t.Fatalf("abort = %#v, %v", pending, err)
	}
	if err := store.ClearAbort(control.RunID); err != nil {
		t.Fatal(err)
	}
	pending, err = store.AbortRequested(control.RunID)
	if err != nil || pending != nil {
		t.Fatalf("cleared abort = %#v, %v", pending, err)
	}
}
