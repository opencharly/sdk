package agentkit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/google/renameio/v2"
	sdk "github.com/opencharly/sdk"
	"github.com/opencharly/sdk/spec"
)

const (
	agentAbortControlDefinition = "#AgentAbortControl"
	agentEventDefinition        = "#AgentEvent"
	agentFederationDefinition   = "#AgentFederationRecord"
	agentRunDefinition          = "#AgentRunRequest"
	agentSessionDefinition      = "#AgentSession"
	agentTeamDefinition         = "#AgentTeamRecord"
	incidentDefinition          = "#Incident"
	rcaDefinition               = "#RCARecord"
	recoveryDefinition          = "#RecoveryDecision"
	terminalFrameDefinition     = "#TerminalFrame"
	uuidV7Definition            = "#UUIDv7"
)

// Store is a daemon-free durable agent state directory. Every record is a
// CUE-generated domain type; JSONL event logs are append-only and sequence
// checked under an advisory file lock so separate charly invocations compose.
type Store struct{ Dir string }

func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			state = filepath.Join(home, ".local", "state")
		}
		dir = filepath.Join(state, "charly", "agents")
	}
	for _, sub := range []string{"sessions", "runs", "events", "terminal", "teams", "federation", "incidents", "rcas", "recoveries", "evidence"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("agent store: create %s directory: %w", sub, err)
		}
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) PutTeam(v spec.AgentTeamRecord) error {
	return s.put("teams", agentTeamDefinition, v.ID, v)
}
func (s *Store) Team(id spec.UUIDv7) (spec.AgentTeamRecord, error) {
	var v spec.AgentTeamRecord
	return v, s.get("teams", agentTeamDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) Teams() ([]spec.AgentTeamRecord, error) {
	var out []spec.AgentTeamRecord
	err := s.list("teams", func(path string) error {
		var v spec.AgentTeamRecord
		if err := readRecord(path, agentTeamDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, err
}

func (s *Store) PutSession(v spec.AgentSession) error {
	return s.put("sessions", agentSessionDefinition, v.ID, v)
}
func (s *Store) Session(id spec.UUIDv7) (spec.AgentSession, error) {
	var v spec.AgentSession
	return v, s.get("sessions", agentSessionDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) Sessions() ([]spec.AgentSession, error) {
	var out []spec.AgentSession
	err := s.list("sessions", func(path string) error {
		var v spec.AgentSession
		if err := readRecord(path, agentSessionDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, err
}

func (s *Store) PutRun(v spec.AgentRunRequest) error {
	return s.put("runs", agentRunDefinition, v.ID, v)
}

// CreateRunOnce atomically reserves an idempotency key across concurrent
// controller processes. It returns the existing record without overwriting it.
func (s *Store) CreateRunOnce(v spec.AgentRunRequest) (spec.AgentRunRequest, bool, error) {
	var result spec.AgentRunRequest
	created := false
	name, err := validatedRecordName(v.ID)
	if err != nil {
		return result, false, err
	}
	b, err := marshalGenerated(agentRunDefinition, v, true)
	if err != nil {
		return result, false, err
	}
	err = s.withLock(func() error {
		entries, err := os.ReadDir(filepath.Join(s.Dir, "runs"))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var existing spec.AgentRunRequest
			if err := readRecord(
				filepath.Join(s.Dir, "runs", entry.Name()),
				agentRunDefinition,
				&existing,
				func() spec.UUIDv7 { return existing.ID },
			); err != nil {
				return err
			}
			if existing.IdempotencyKey == v.IdempotencyKey {
				result = existing
				return nil
			}
		}
		path := filepath.Join(s.Dir, "runs", name+".json")
		if err := atomicWrite(path, append(b, '\n')); err != nil {
			return err
		}
		result, created = v, true
		return nil
	})
	return result, created, err
}
func (s *Store) Run(id spec.UUIDv7) (spec.AgentRunRequest, error) {
	var v spec.AgentRunRequest
	return v, s.get("runs", agentRunDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) Runs() ([]spec.AgentRunRequest, error) {
	var out []spec.AgentRunRequest
	err := s.list("runs", func(path string) error {
		var v spec.AgentRunRequest
		if err := readRecord(path, agentRunDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return string(out[i].ID) < string(out[j].ID) })
	return out, err
}
func (s *Store) FindIdempotency(key string) (spec.AgentRunRequest, bool, error) {
	var found spec.AgentRunRequest
	if key == "" {
		return found, false, errors.New("agent store: empty idempotency key")
	}
	err := s.list("runs", func(path string) error {
		var v spec.AgentRunRequest
		if err := readRecord(path, agentRunDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		if v.IdempotencyKey == key {
			found = v
		}
		return nil
	})
	return found, found.ID != "", err
}

func (s *Store) AppendEvent(v spec.AgentEvent) error {
	if v.Sequence < 1 {
		return errors.New("agent store: event sequence must be >= 1")
	}
	name, err := validatedRecordName(v.RunID)
	if err != nil {
		return err
	}
	if err := validateGenerated(agentEventDefinition, v); err != nil {
		return err
	}
	return s.withLock(func() error {
		path := filepath.Join(s.Dir, "events", name+".jsonl")
		events, err := readEvents(path)
		if err != nil {
			return err
		}
		want := int64(len(events) + 1)
		if v.Sequence != want {
			return fmt.Errorf("agent store: event sequence %d, want %d", v.Sequence, want)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if err := useFile(f, func() error {
			b, err := marshalGenerated(agentEventDefinition, v, false)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				return err
			}
			return f.Sync()
		}); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	})
}

func (s *Store) Events(runID spec.UUIDv7) ([]spec.AgentEvent, error) {
	name, err := validatedRecordName(runID)
	if err != nil {
		return nil, err
	}
	return readEvents(filepath.Join(s.Dir, "events", name+".jsonl"))
}

// AppendTerminalFrame assigns the next durable per-run sequence under the
// cross-process store lock and appends one generated terminal evidence frame.
func (s *Store) AppendTerminalFrame(v spec.TerminalFrame) (spec.TerminalFrame, error) {
	name, err := validatedRecordName(v.RunID)
	if err != nil {
		return v, err
	}
	err = s.withLock(func() error {
		path := filepath.Join(s.Dir, "terminal", name+".jsonl")
		frames, err := readTerminalFrames(path)
		if err != nil {
			return err
		}
		want := int64(1)
		if len(frames) > 0 {
			want = frames[len(frames)-1].Sequence + 1
		}
		if v.Sequence == 0 {
			v.Sequence = want
		} else if v.Sequence != want && (v.Kind != "resync" || v.Sequence <= want) {
			return fmt.Errorf("agent store: terminal sequence %d, want %d", v.Sequence, want)
		}
		if err := validateGenerated(terminalFrameDefinition, v); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if err := useFile(f, func() error {
			data, err := marshalGenerated(terminalFrameDefinition, v, false)
			if err != nil {
				return err
			}
			if _, err := f.Write(append(data, '\n')); err != nil {
				return err
			}
			return f.Sync()
		}); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	})
	return v, err
}

func (s *Store) TerminalFrames(runID spec.UUIDv7) ([]spec.TerminalFrame, error) {
	name, err := validatedRecordName(runID)
	if err != nil {
		return nil, err
	}
	return readTerminalFrames(filepath.Join(s.Dir, "terminal", name+".jsonl"))
}

// RequestAbort records cross-process cancellation intent for the synchronous,
// ephemeral controller currently owning a run. No agent daemon is involved.
func (s *Store) RequestAbort(v spec.AgentAbortControl) error {
	name, err := validatedRecordName(v.RunID)
	if err != nil {
		return err
	}
	data, err := marshalGenerated(agentAbortControlDefinition, v, false)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		path := filepath.Join(s.Dir, "runs", name+".abort")
		return atomicWrite(path, append(data, '\n'))
	})
}

func (s *Store) AbortRequested(runID spec.UUIDv7) (*spec.AgentAbortControl, error) {
	name, err := validatedRecordName(runID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, "runs", name+".abort")
	var v spec.AgentAbortControl
	err = readGeneratedJSON(path, agentAbortControlDefinition, &v)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err == nil && v.RunID != runID {
		return nil, recordIDMismatch(path, string(v.RunID))
	}
	return &v, err
}

// WaitAbort blocks on fsnotify's platform-native directory notification stream
// until the requested run's abort record exists. The pre/post-watch checks
// close the creation race without polling, sleeps, retries, or a permanent
// controller.
func (s *Store) WaitAbort(ctx context.Context, runID spec.UUIDv7) (*spec.AgentAbortControl, error) {
	if control, err := s.AbortRequested(runID); control != nil || err != nil {
		return control, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("agent store: watch abort controls: %w", err)
	}
	defer watcher.Close() //nolint:errcheck
	if err := watcher.Add(filepath.Join(s.Dir, "runs")); err != nil {
		return nil, fmt.Errorf("agent store: watch runs directory: %w", err)
	}
	if control, err := s.AbortRequested(runID); control != nil || err != nil {
		return control, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case _, ok := <-watcher.Events:
			if !ok {
				return nil, errors.New("agent store: abort notification stream closed")
			}
			if control, err := s.AbortRequested(runID); control != nil || err != nil {
				return control, err
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil, errors.New("agent store: abort error stream closed")
			}
			return nil, fmt.Errorf("agent store: abort notification: %w", err)
		}
	}
}

func (s *Store) ClearAbort(runID spec.UUIDv7) error {
	name, err := validatedRecordName(runID)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.Dir, "runs", name+".abort"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) PutIncident(v spec.Incident) error {
	return s.put("incidents", incidentDefinition, v.ID, v)
}
func (s *Store) Incident(id spec.UUIDv7) (spec.Incident, error) {
	var v spec.Incident
	return v, s.get("incidents", incidentDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) Incidents() ([]spec.Incident, error) {
	var out []spec.Incident
	err := s.list("incidents", func(path string) error {
		var v spec.Incident
		if err := readRecord(path, incidentDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutRCA(v spec.RCARecord) error {
	return s.put("rcas", rcaDefinition, v.ID, v)
}
func (s *Store) RCA(id spec.UUIDv7) (spec.RCARecord, error) {
	var v spec.RCARecord
	return v, s.get("rcas", rcaDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) RCAs() ([]spec.RCARecord, error) {
	var out []spec.RCARecord
	err := s.list("rcas", func(path string) error {
		var v spec.RCARecord
		if err := readRecord(path, rcaDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutRecovery(v spec.RecoveryDecision) error {
	return s.put("recoveries", recoveryDefinition, v.ID, v)
}
func (s *Store) Recovery(id spec.UUIDv7) (spec.RecoveryDecision, error) {
	var v spec.RecoveryDecision
	return v, s.get("recoveries", recoveryDefinition, id, &v, func() spec.UUIDv7 { return v.ID })
}
func (s *Store) Recoveries() ([]spec.RecoveryDecision, error) {
	var out []spec.RecoveryDecision
	err := s.list("recoveries", func(path string) error {
		var v spec.RecoveryDecision
		if err := readRecord(path, recoveryDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	return out, err
}
func (s *Store) PutFederation(v spec.AgentFederationRecord) error {
	return s.put("federation", agentFederationDefinition, v.ID, v)
}
func (s *Store) Federation() ([]spec.AgentFederationRecord, error) {
	var out []spec.AgentFederationRecord
	err := s.list("federation", func(path string) error {
		var v spec.AgentFederationRecord
		if err := readRecord(path, agentFederationDefinition, &v, func() spec.UUIDv7 { return v.ID }); err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt < out[j].UpdatedAt })
	return out, err
}

func (s *Store) put(sub, definition string, id spec.UUIDv7, value any) error {
	name, err := validatedRecordName(id)
	if err != nil {
		return err
	}
	b, err := marshalGenerated(definition, value, true)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		path := filepath.Join(s.Dir, sub, name+".json")
		return atomicWrite(path, append(b, '\n'))
	})
}

func atomicWrite(path string, content []byte) error {
	if err := renameio.WriteFile(path, content, 0o600, renameio.WithStaticPermissions(0o600)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) (returnErr error) {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, dir.Close()) }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync agent store directory %s: %w", path, err)
	}
	return nil
}

func (s *Store) get(
	sub string,
	definition string,
	id spec.UUIDv7,
	dst any,
	recordID func() spec.UUIDv7,
) error {
	name, err := validatedRecordName(id)
	if err != nil {
		return err
	}
	return readRecord(filepath.Join(s.Dir, sub, name+".json"), definition, dst, recordID)
}

func (s *Store) list(sub string, fn func(string) error) error {
	entries, err := os.ReadDir(filepath.Join(s.Dir, sub))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := fn(filepath.Join(s.Dir, sub, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) withLock(fn func() error) (returnErr error) {
	f, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)) }()
	return fn()
}

func useFile(file *os.File, operation func() error) (returnErr error) {
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	return operation()
}

func validateGenerated(definition string, value any) error {
	if err := sdk.ValidateGenerated(definition, value); err != nil {
		return fmt.Errorf("agent store: validate %s: %w", definition, err)
	}
	return nil
}

func marshalGenerated(definition string, value any, indent bool) ([]byte, error) {
	if err := validateGenerated(definition, value); err != nil {
		return nil, err
	}
	if indent {
		return json.MarshalIndent(value, "", "  ")
	}
	return json.Marshal(value)
}

func validatedRecordName(id spec.UUIDv7) (string, error) {
	if err := validateGenerated(uuidV7Definition, id); err != nil {
		return "", err
	}
	return string(id), nil
}

func readGeneratedJSON(path, definition string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := sdk.DecodeGeneratedJSON(definition, b, dst); err != nil {
		return fmt.Errorf("agent store: decode and validate %s as %s: %w", path, definition, err)
	}
	return nil
}

func readRecord(path, definition string, dst any, recordID func() spec.UUIDv7) error {
	if err := readGeneratedJSON(path, definition, dst); err != nil {
		return err
	}
	if got := string(recordID()); got != recordNameFromPath(path) {
		return recordIDMismatch(path, got)
	}
	return nil
}

func recordNameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func recordIDMismatch(path, got string) error {
	return fmt.Errorf(
		"agent store: record id %q does not match filename id %q in %s",
		got,
		recordNameFromPath(path),
		path,
	)
}

func readEvents(path string) (out []spec.AgentEvent, returnErr error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := len(out) + 1
		var event spec.AgentEvent
		if err := sdk.DecodeGeneratedJSON(agentEventDefinition, scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("agent store: decode and validate %s line %d as %s: %w", path, line, agentEventDefinition, err)
		}
		if got := string(event.RunID); got != recordNameFromPath(path) {
			return nil, recordIDMismatch(path, got)
		}
		if event.Sequence != int64(line) {
			return nil, fmt.Errorf(
				"agent store: event sequence %d at %s line %d, want %d",
				event.Sequence,
				path,
				line,
				line,
			)
		}
		out = append(out, event)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}

func readTerminalFrames(path string) (out []spec.TerminalFrame, returnErr error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, f.Close()) }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := len(out) + 1
		var frame spec.TerminalFrame
		if err := sdk.DecodeGeneratedJSON(terminalFrameDefinition, scanner.Bytes(), &frame); err != nil {
			return nil, fmt.Errorf("agent store: decode and validate %s line %d as %s: %w", path, line, terminalFrameDefinition, err)
		}
		if got := string(frame.RunID); got != recordNameFromPath(path) {
			return nil, recordIDMismatch(path, got)
		}
		want := int64(1)
		if len(out) > 0 {
			want = out[len(out)-1].Sequence + 1
		}
		if frame.Sequence != want && (frame.Kind != "resync" || frame.Sequence <= want) {
			return nil, fmt.Errorf(
				"agent store: terminal sequence %d at %s line %d, want %d",
				frame.Sequence,
				path,
				line,
				want,
			)
		}
		out = append(out, frame)
	}
	return out, scanner.Err()
}
