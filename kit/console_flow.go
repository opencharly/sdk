// console_flow.go is the TRANSPORT-AGNOSTIC console FLOW engine: a bounded
// state machine that drives a remote machine's console by CONTINUOUS screenshot
// + OCR until a named condition is observed, then acts and transitions.
//
// It exists because a fixed `wait_for` anchor + fixed `timeout` is brittle on a
// real console: a boot screen appears at an unknown time, a passphrase may be
// accepted or rejected, an installer may offer or skip a screen, and a retry
// loop must be bounded rather than guessed. A flow makes ALL of that explicit
// DATA:
//
//   - CONTINUOUS OCR-UNTIL-CONDITION — a node's `wait` lists one or more NAMED
//     outcomes; the engine re-captures and OCRs (a real condition poll, never a
//     sleep) until ANY outcome's substring appears, and reports WHICH one.
//   - MULTIPLE OUTCOMES — each named outcome is a branch (success, failure, or
//     any intermediate state), so one wait can distinguish "booted", "wrong
//     passphrase", and "still waiting" without a guessed timeout.
//   - if/then/else + case/switch — `transitions` maps an outcome name to the next
//     node id, so routing on the OBSERVED outcome IS the conditional. A node with
//     no transition for the matched outcome falls through to its default `next`.
//   - while loop — a transition that points BACK to an earlier node is a loop,
//     bounded by `max_loops` (per node) and `max_steps` (whole flow), so a
//     condition that never becomes true FAILS instead of spinning forever. This
//     is the R4-safe form of "repeat until": a bounded, condition-driven loop,
//     never an unbounded retry.
//
// The engine holds NO wire type (SDD): each transport decodes its own authored
// flow and converts it to these neutral structs. It is shared (R3) for the same
// reason ConsoleWizard is — the JetKVM and SPICE transports drive the same
// consoles.

package kit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ConsoleFlowOutcome is ONE named condition a node waits for. The engine
// OCR-polls until any outcome's Match substring is present.
type ConsoleFlowOutcome struct {
	// Name is the outcome's identifier, keyed in a node's Transitions.
	Name string
	// Match is the case-insensitive substring that identifies this outcome.
	Match string
	// Failure marks an outcome that is a FAILURE unless a transition routes it
	// explicitly. It lets a flow fail fast on a wrong passphrase / an error
	// screen while still allowing a deliberate recovery branch.
	Failure bool
}

// ConsoleFlowAction is what a node sends once its wait matched (or immediately,
// if it has no wait). Exactly one of the fields is used.
type ConsoleFlowAction struct {
	// Key / Combo / Type are the raw inputs a node can send.
	Key   string
	Combo string
	Text  string
	// Command runs a shell command in an OPEN terminal, reading its output by
	// OCR (the marker-completed path from ConsoleSession). Sudo runs it through
	// sudo with the flow's SudoPassword.
	Command string
	Sudo    bool
	// Expect, for Command, requires the substring in the command's output.
	Expect string
	// CloseTerminal sends `exit` (a convenience for the final node).
	CloseTerminal bool
}

// consoleFlowActionIsSet reports whether a is a no-op.
func consoleFlowActionIsSet(a ConsoleFlowAction) bool {
	return a.Key != "" || a.Combo != "" || a.Text != "" || a.Command != "" || a.CloseTerminal
}

// ConsoleFlowNode is ONE state of the flow.
type ConsoleFlowNode struct {
	// ID is the node's identifier (its map key; carried for evidence).
	ID string
	// Description is an optional evidence label.
	Description string
	// Wait lists the outcomes to OCR-poll for. Empty means "act immediately".
	Wait []ConsoleFlowOutcome
	// Action is sent after a wait matched (or immediately when Wait is empty).
	Action ConsoleFlowAction
	// Transitions maps an outcome Name to the next node ID. A matched outcome
	// with no entry falls through to Next.
	Transitions map[string]string
	// Next is the default target for an unmatched outcome ("" = end the flow).
	Next string
	// Artifact, when set, saves the frame captured at this node's decision.
	Artifact string
	// TimeoutSec bounds this node's OCR poll (default ConsoleDefaultTimeoutSec).
	TimeoutSec int
}

// ConsoleFlow drives a state machine over a ConsoleTransport.
type ConsoleFlow struct {
	// Start is the entry node ID.
	Start string
	// Nodes is the node set, keyed by ID.
	Nodes map[string]ConsoleFlowNode
	// Transport is the per-transport capture/input primitive set.
	Transport ConsoleTransport
	// SudoPassword is entered at a sudo prompt for a Command action.
	SudoPassword string
	// OCR defaults to OCRBytes when nil.
	OCR func(png []byte) (string, error)
	// PollInterval is the continuous-capture cadence (default
	// ConsoleSessionDefaultPollInterval) — a condition poll, not a sleep.
	PollInterval time.Duration
	// MaxSteps is the hard bound on node executions for the WHOLE flow (default
	// ConsoleFlowDefaultMaxSteps). Exceeding it FAILS, so a cycle that never
	// reaches a terminal node cannot spin forever (R4 — no unbounded loops).
	MaxSteps int
	// MaxLoops is the per-node revisit bound (default ConsoleFlowDefaultMaxLoops).
	MaxLoops int
	// Logger, when set, receives one line per node (evidence).
	Logger func(format string, args ...any)
	// session is the internal terminal session for Command actions, created once.
	session *ConsoleSession
}

// ConsoleFlowDefaultMaxSteps bounds total node executions.
const ConsoleFlowDefaultMaxSteps = 200

// ConsoleFlowDefaultMaxLoops bounds how many times one node may be entered.
const ConsoleFlowDefaultMaxLoops = 50

// ConsoleStepResult is the outcome of one visited node.
type ConsoleStepResult struct {
	Node    string
	Outcome string
	// Text is the OCR text read at the node's decision.
	Text string
	// CommandOutput is the OCR-read output of a `command` action (empty for a
	// raw-input node), so "read the results via OCR" is visible in the evidence.
	CommandOutput string
}

// ConsoleFlowResult is the full evidence of a flow run.
type ConsoleFlowResult struct {
	Steps   []ConsoleStepResult
	Final   string
	LogText string
}

// Validate checks the flow's shape WITHOUT a transport: the start node exists,
// every transition and Next names a real node, and bounds are positive. It is
// PURE, so a malformed flow fails before any device session.
func (f *ConsoleFlow) Validate() error {
	if f.Start == "" {
		return fmt.Errorf("console flow: start node is required")
	}
	if _, ok := f.Nodes[f.Start]; !ok {
		return fmt.Errorf("console flow: start node %q is not defined", f.Start)
	}
	if len(f.Nodes) == 0 {
		return fmt.Errorf("console flow: at least one node is required")
	}
	for id, n := range f.Nodes {
		if n.Action.Command != "" && n.Action.Key != "" {
			return fmt.Errorf("console flow: node %q sets both command and key (one action per node)", id)
		}
		for _, o := range n.Wait {
			if strings.TrimSpace(o.Match) == "" {
				return fmt.Errorf("console flow: node %q has an outcome %q with an empty match", id, o.Name)
			}
			if o.Name == "" {
				return fmt.Errorf("console flow: node %q has an outcome with a match but no name", id)
			}
		}
		for name, target := range n.Transitions {
			if target == "" {
				return fmt.Errorf("console flow: node %q transition %q has an empty target", id, name)
			}
			if _, ok := f.Nodes[target]; !ok {
				return fmt.Errorf("console flow: node %q transition %q -> %q names an undefined node", id, name, target)
			}
		}
		if n.Next != "" {
			if _, ok := f.Nodes[n.Next]; !ok {
				return fmt.Errorf("console flow: node %q next -> %q names an undefined node", id, n.Next)
			}
		}
	}
	return nil
}

// Run executes the flow from Start, returning per-node evidence. It FAILS on:
// an undefined node (should be caught by Validate), a wait timeout (naming the
// node, its outcomes, and what OCR read), a failure outcome with no recovery
// transition, exceeding MaxSteps, or exceeding a node's MaxLoops.
func (f *ConsoleFlow) Run(ctx context.Context) (ConsoleFlowResult, error) {
	if err := f.Validate(); err != nil {
		return ConsoleFlowResult{}, err
	}
	maxSteps := f.MaxSteps
	if maxSteps <= 0 {
		maxSteps = ConsoleFlowDefaultMaxSteps
	}
	maxLoops := f.MaxLoops
	if maxLoops <= 0 {
		maxLoops = ConsoleFlowDefaultMaxLoops
	}
	f.session = &ConsoleSession{
		Transport:    f.Transport,
		SudoPassword: f.SudoPassword,
		OCR:          f.OCR,
		PollInterval: f.PollInterval,
		Logger:       f.Logger,
	}

	var res ConsoleFlowResult
	visits := map[string]int{}
	cur := f.Start
	for step := 0; ; step++ {
		if step >= maxSteps {
			return res, fmt.Errorf("console flow: exceeded max_steps %d (a cycle never reached a terminal node); visited: %s",
				maxSteps, describeVisits(visits))
		}
		node, ok := f.Nodes[cur]
		if !ok {
			return res, fmt.Errorf("console flow: node %q is not defined", cur)
		}
		visits[cur]++
		if visits[cur] > maxLoops {
			return res, fmt.Errorf("console flow: node %q entered %d times (> max_loops %d) — the loop condition never became true",
				cur, visits[cur], maxLoops)
		}

		outcome, text, cmdOut, err := f.runNode(ctx, node)
		if err != nil {
			return res, fmt.Errorf("console flow: node %q (%s): %w", cur, node.Description, err)
		}
		res.Steps = append(res.Steps, ConsoleStepResult{Node: cur, Outcome: outcome, Text: text, CommandOutput: cmdOut})
		f.logf("node %q -> outcome %q", cur, outcome)

		// Route: an explicit transition wins; else Next; else end.
		next, has := node.Transitions[outcome]
		if !has {
			next = node.Next
		}
		if next == "" {
			res.Final = cur
			res.LogText = renderFlowLog(res.Steps)
			return res, nil
		}
		cur = next
	}
}

// runNode performs one node: OCR-poll for a wait outcome (if any), then the
// action. It returns the matched outcome name ("" for an action-only node), the
// OCR text at the decision, and any command action's OCR-read output.
func (f *ConsoleFlow) runNode(ctx context.Context, node ConsoleFlowNode) (string, string, string, error) {
	outcome, text := "", ""
	if len(node.Wait) > 0 {
		o, got, err := f.waitOutcome(ctx, node)
		if err != nil {
			return "", "", "", err
		}
		if o == nil {
			return "", got, "", fmt.Errorf("timed out after %ds waiting for any of %v; the screen last read %q",
				nodeTimeout(node), outcomeNames(node.Wait), ConsolePreview(got, 300))
		}
		if o.Failure {
			if _, routed := node.Transitions[o.Name]; !routed {
				return o.Name, got, "", fmt.Errorf("failure outcome %q observed and not routed: %q",
					o.Name, ConsolePreview(got, 300))
			}
		}
		outcome, text = o.Name, got
	}
	cmdOut, err := f.apply(ctx, node)
	if err != nil {
		return outcome, text, cmdOut, err
	}
	return outcome, text, cmdOut, nil
}

// waitOutcome OCR-polls until any wait outcome matches, returning the FIRST match
// (checked in declaration order) and the OCR text. A capture/OCR error is
// distinct from "no match" and returned as an error.
func (f *ConsoleFlow) waitOutcome(ctx context.Context, node ConsoleFlowNode) (*ConsoleFlowOutcome, string, error) {
	deadline := time.Now().Add(time.Duration(nodeTimeout(node)) * time.Second)
	poll := f.PollInterval
	if poll <= 0 {
		poll = ConsoleSessionDefaultPollInterval
	}
	var last string
	for {
		_, got, err := f.session.screenHasAny(ctx, nil, node.Artifact)
		if err != nil {
			return nil, last, err
		}
		last = got
		lower := strings.ToLower(got)
		for i := range node.Wait {
			if strings.Contains(lower, strings.ToLower(node.Wait[i].Match)) {
				return &node.Wait[i], got, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, last, nil
		}
		select {
		case <-ctx.Done():
			return nil, last, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// apply sends the node's action, if any, returning a command action's OCR-read
// output (empty otherwise).
func (f *ConsoleFlow) apply(ctx context.Context, node ConsoleFlowNode) (string, error) {
	a := node.Action
	switch {
	case a.Command != "":
		res, err := f.session.RunCommand(ctx, ConsoleCommand{
			Command: a.Command, Sudo: a.Sudo, Expect: a.Expect, TimeoutSec: node.TimeoutSec,
		})
		return res.Output, err
	case a.Key != "":
		return "", f.Transport.PressKey(ctx, a.Key)
	case a.Combo != "":
		return "", f.Transport.PressCombo(ctx, a.Combo)
	case a.Text != "":
		return "", f.Transport.Type(ctx, a.Text)
	case a.CloseTerminal:
		if err := f.Transport.Type(ctx, "exit"); err != nil {
			return "", err
		}
		return "", f.Transport.PressKey(ctx, "Return")
	}
	return "", nil
}

func nodeTimeout(node ConsoleFlowNode) int {
	if node.TimeoutSec > 0 {
		return node.TimeoutSec
	}
	return ConsoleDefaultTimeoutSec
}

func outcomeNames(os []ConsoleFlowOutcome) []string {
	out := make([]string, 0, len(os))
	for _, o := range os {
		out = append(out, o.Name)
	}
	return out
}

func describeVisits(v map[string]int) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, v[k]))
	}
	return strings.Join(parts, ", ")
}

func renderFlowLog(steps []ConsoleStepResult) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "step %d: %s", i+1, s.Node)
		if s.Outcome != "" {
			fmt.Fprintf(&b, " -> %s", s.Outcome)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

func (f *ConsoleFlow) logf(format string, args ...any) {
	if f.Logger != nil {
		f.Logger(format, args...)
	}
}
