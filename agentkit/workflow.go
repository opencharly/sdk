// Package agentkit implements transport-independent control-plane invariants.
// Persistent stores and command surfaces can wrap this state machine; the wire
// records themselves are generated from schema/agent_control.cue.
package agentkit

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opencharly/sdk/spec"
)

type Workflow struct {
	mu        sync.Mutex
	incidents map[spec.UUIDv7]spec.Incident
	rcas      map[spec.UUIDv7]spec.RCARecord
}

func NewWorkflow() *Workflow {
	return &Workflow{incidents: map[spec.UUIDv7]spec.Incident{}, rcas: map[spec.UUIDv7]spec.RCARecord{}}
}

func (w *Workflow) RecordIncident(runID spec.UUIDv7, summary string, evidence []string) (spec.Incident, error) {
	if summary == "" {
		return spec.Incident{}, errors.New("agent workflow: incident summary is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	incident := spec.Incident{ID: newID(), RunID: runID, State: "needs_rca", Summary: summary, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), EvidenceRefs: append([]string(nil), evidence...)}
	w.incidents[incident.ID] = incident
	return incident, nil
}

func (w *Workflow) StartRCA(incidentID spec.UUIDv7) (spec.RCARecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	incident, ok := w.incidents[incidentID]
	if !ok {
		return spec.RCARecord{}, fmt.Errorf("agent workflow: incident %s not found", incidentID)
	}
	if incident.State != "needs_rca" {
		return spec.RCARecord{}, fmt.Errorf("agent workflow: incident %s state is %s", incidentID, incident.State)
	}
	rca := spec.RCARecord{ID: newID(), IncidentID: incidentID, State: "active"}
	w.rcas[rca.ID] = rca
	incident.State = "rca_active"
	w.incidents[incidentID] = incident
	return rca, nil
}

func (w *Workflow) CompleteRCA(rcaID spec.UUIDv7, rootCause string, findings []string) (spec.RCARecord, error) {
	if rootCause == "" {
		return spec.RCARecord{}, errors.New("agent workflow: completed RCA requires a root cause")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	rca, ok := w.rcas[rcaID]
	if !ok || rca.State != "active" {
		return spec.RCARecord{}, fmt.Errorf("agent workflow: active RCA %s not found", rcaID)
	}
	rca.State, rca.RootCause, rca.Findings, rca.CompletedAt = "complete", rootCause, append([]string(nil), findings...), time.Now().UTC().Format(time.RFC3339Nano)
	w.rcas[rcaID] = rca
	incident := w.incidents[rca.IncidentID]
	incident.State = "awaiting_recovery"
	w.incidents[incident.ID] = incident
	return rca, nil
}

// DecideRecovery plans the recovery for an incident whose RCA completed (or an
// explicitly authorized emergency abort). The returned decision is deliberately
// NOT recorded here and the incident deliberately STAYS in awaiting_recovery:
// agentkit is the transport-independent invariant layer — executing a recovery
// action (reattach/resume/restart/…) is transport work, and durability is the
// agentkit.Store layer, so neither belongs in this state machine. The #Incident
// state enum has no state between awaiting_recovery and resolved, so an
// in-memory transition here would claim progress no execution produced.
//
// The owning durable component is the agent control plane's recovery leg —
// candy/plugin-agent's applyRecovery — which persists the decision
// (Store.PutRecovery: planned → applied/failed with applied_at), executes the
// action, and only then moves the incident to resolved (Store.PutIncident).
func (w *Workflow) DecideRecovery(incidentID, rcaID spec.UUIDv7, action string, emergencyAbort bool, params *spec.RecoveryParams) (spec.RecoveryDecision, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.incidents[incidentID]
	if !ok {
		return spec.RecoveryDecision{}, fmt.Errorf("agent workflow: incident %s not found", incidentID)
	}
	completed := false
	if rca, ok := w.rcas[rcaID]; ok {
		completed = rca.IncidentID == incidentID && rca.State == "complete"
	}
	if !completed && (action != "abort" || !emergencyAbort) {
		return spec.RecoveryDecision{}, errors.New("agent workflow: recovery requires a completed RCA; only an explicitly authorized emergency abort may bypass it")
	}
	decision := spec.RecoveryDecision{ID: newID(), IncidentID: incidentID, RCAID: rcaID, Action: action, AuthorizedEmergencyAbort: emergencyAbort, Params: params, State: "planned", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	return decision, nil
}

func newID() spec.UUIDv7 {
	id, err := uuid.NewV7()
	if err != nil {
		panic("uuid v7: " + err.Error())
	}
	return spec.UUIDv7(id.String())
}

// NewID returns a UUIDv7 for durable agent-domain records.
func NewID() spec.UUIDv7 { return newID() }
