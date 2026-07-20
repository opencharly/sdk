package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/proto"

	pb "github.com/opencharly/sdk/proto"
)

// Channel frame kinds are transport vocabulary, not runtime semantics. Runtime-
// specific events travel as CUE-generated JSON in ChannelFrame.PayloadJson.
const (
	ChannelOpen     = "open"
	ChannelStdin    = "stdin"
	ChannelStdout   = "stdout"
	ChannelStderr   = "stderr"
	ChannelTerminal = "terminal"
	ChannelStatus   = "status"
	ChannelResize   = "resize"
	ChannelSignal   = "signal"
	ChannelAck      = "ack"
	ChannelCancel   = "cancel"
	ChannelExit     = "exit"
	ChannelError    = "error"
	ChannelResync   = "resync"
)

// ProviderChannel is the common subset of the generated client and server
// streams. It lets in-process and gRPC providers share one channel handler.
type ProviderChannel interface {
	Context() context.Context
	Send(*pb.ChannelFrame) error
	Recv() (*pb.ChannelFrame, error)
}

// ChannelProvider is the optional streaming extension to Provider. The first
// frame has already been validated as an open frame and remains available as
// open; subsequent controller frames arrive through stream. Domain payloads are
// generated from CUE and carried in open.PayloadJson.
type ChannelProvider interface {
	OpenChannel(open *pb.ChannelFrame, stream ProviderChannel) error
}

// ReceiveChannelOpen reads and validates the mandatory first frame. The
// request id, provider class/word, and operation are required so every later
// frame can be correlated without inspecting runtime-specific payloads.
func ReceiveChannelOpen(stream ProviderChannel) (*pb.ChannelFrame, error) {
	open, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if open.GetKind() != ChannelOpen {
		return nil, fmt.Errorf("sdk channel: first frame kind %q, want %q", open.GetKind(), ChannelOpen)
	}
	if open.GetRequestId() == "" || open.GetClass() == "" || open.GetReserved() == "" || open.GetOp() == "" {
		return nil, errors.New("sdk channel: open requires request_id, class, reserved, and op")
	}
	return open, nil
}

// OpenProviderChannel starts a generated Provider.Channel stream and sends its
// mandatory open frame. The returned stream is ready for concurrent Send/Recv,
// as supported by gRPC.
func OpenProviderChannel(ctx context.Context, client pb.ProviderClient, open *pb.ChannelFrame) (pb.Provider_ChannelClient, error) {
	if open == nil {
		return nil, errors.New("sdk channel: nil open frame")
	}
	if open.Kind == "" {
		open.Kind = ChannelOpen
	}
	if open.Kind != ChannelOpen {
		return nil, fmt.Errorf("sdk channel: open frame kind %q", open.Kind)
	}
	stream, err := client.Channel(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(open); err != nil {
		return nil, err
	}
	return stream, nil
}

// SequenceGate rejects duplicates, regressions, and gaps. A provider can turn
// a gap into ChannelResync using ReplayBuffer.ReplayFrom; it must never silently
// reorder process or terminal output.
type SequenceGate struct {
	mu   sync.Mutex
	next uint64
}

func NewSequenceGate(first uint64) *SequenceGate { return &SequenceGate{next: first} }

func (g *SequenceGate) Accept(frame *pb.ChannelFrame) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if frame.GetSequence() != g.next {
		if frame.GetKind() == ChannelResync && frame.GetSequence() > g.next {
			g.next = frame.GetSequence() + 1
			return nil
		}
		return fmt.Errorf("sdk channel: sequence %d, want %d", frame.GetSequence(), g.next)
	}
	g.next++
	return nil
}

// Expected returns the next sequence without advancing the gate.
func (g *SequenceGate) Expected() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.next
}

// ReplayBuffer is a bounded, acknowledgement-aware frame history for detach /
// reconnect. Bounds are enforced by both frame count and protobuf byte size.
// When an unacknowledged frame would be evicted, Add fails loudly; callers must
// preserve evidence and enter the incident/RCA workflow rather than hide loss.
type ReplayBuffer struct {
	mu       sync.Mutex
	frames   []*pb.ChannelFrame
	bytes    int
	maxFrame int
	maxBytes int
	acked    uint64
}

func NewReplayBuffer(maxFrames, maxBytes int) *ReplayBuffer {
	return &ReplayBuffer{maxFrame: maxFrames, maxBytes: maxBytes}
}

func (b *ReplayBuffer) Add(frame *pb.ChannelFrame) error {
	if frame == nil || frame.GetSequence() == 0 {
		return errors.New("sdk channel: replay frame requires a non-zero sequence")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n := len(b.frames); n > 0 && frame.GetSequence() <= b.frames[n-1].GetSequence() {
		return fmt.Errorf("sdk channel: replay sequence %d is not monotonic", frame.GetSequence())
	}
	cloned := proto.Clone(frame).(*pb.ChannelFrame)
	size := proto.Size(cloned)
	if (b.maxFrame > 0 && len(b.frames)+1 > b.maxFrame) || (b.maxBytes > 0 && b.bytes+size > b.maxBytes) {
		if len(b.frames) == 0 || b.frames[0].GetSequence() > b.acked {
			// The diagnostic names the UNACKNOWLEDGED OLDEST frame whose loss
			// blocks the eviction, not the incoming frame that tripped the
			// bound. With an empty buffer the incoming frame is itself the
			// unacknowledged one.
			unacknowledged := frame.GetSequence()
			if len(b.frames) > 0 {
				unacknowledged = b.frames[0].GetSequence()
			}
			return fmt.Errorf("sdk channel: replay capacity exceeded with unacknowledged sequence %d", unacknowledged)
		}
		b.dropAcknowledgedLocked()
	}
	if (b.maxFrame > 0 && len(b.frames)+1 > b.maxFrame) || (b.maxBytes > 0 && b.bytes+size > b.maxBytes) {
		return fmt.Errorf("sdk channel: frame %d exceeds replay capacity", frame.GetSequence())
	}
	b.frames = append(b.frames, cloned)
	b.bytes += size
	return nil
}

func (b *ReplayBuffer) Acknowledge(sequence uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sequence > b.acked {
		b.acked = sequence
	}
	b.dropAcknowledgedLocked()
}

func (b *ReplayBuffer) dropAcknowledgedLocked() {
	cut := 0
	for cut < len(b.frames) && b.frames[cut].GetSequence() <= b.acked {
		b.bytes -= proto.Size(b.frames[cut])
		cut++
	}
	b.frames = b.frames[cut:]
}

func (b *ReplayBuffer) ReplayFrom(sequence uint64) ([]*pb.ChannelFrame, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.frames) > 0 && sequence < b.frames[0].GetSequence() {
		return nil, fmt.Errorf("sdk channel: sequence %d is no longer available; oldest is %d", sequence, b.frames[0].GetSequence())
	}
	out := make([]*pb.ChannelFrame, 0, len(b.frames))
	for _, frame := range b.frames {
		if frame.GetSequence() >= sequence {
			out = append(out, proto.Clone(frame).(*pb.ChannelFrame))
		}
	}
	return out, nil
}

// CopyChannel relays frames until EOF or cancellation. It is intentionally a
// byte-preserving transport primitive; it does not inspect agent or terminal
// payloads.
func CopyChannel(dst interface{ Send(*pb.ChannelFrame) error }, src interface {
	Recv() (*pb.ChannelFrame, error)
}) error {
	for {
		frame, err := src.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := dst.Send(frame); err != nil {
			return err
		}
	}
}

// RelayChannel connects a controller-side ProviderChannel to a downstream gRPC
// channel with ordered half-close semantics. Provider output is the evidence
// writer: once that direction ends, no later frame can mutate controller state
// and the relay may return. If controller input ends first, CloseSend delivers
// the protocol EOF and the relay drains provider output before returning. This
// prevents a completed command from racing its successor's durable cursor.
//
// Cancellation ownership: when the provider-output direction finishes first,
// RelayChannel returns while the input-copy goroutine may still be blocked in
// upstream.Recv(). The CALLER owns upstream's lifecycle — after return, cancel
// the context upstream carries (or close upstream) to release that goroutine.
func RelayChannel(upstream ProviderChannel, downstream interface {
	ProviderChannel
	CloseSend() error
}) error {
	type result struct {
		providerOutput bool
		err            error
	}
	results := make(chan result, 2)
	go func() {
		// Controller input owns the downstream send side. Once input ends,
		// half-close it and keep draining provider output below.
		err := CopyChannel(downstream, upstream)
		if errors.Is(err, io.EOF) {
			// A provider may close immediately after its terminal frame while the
			// controller's acknowledgement is concurrently in flight.
			err = nil
		}
		closeErr := downstream.CloseSend()
		if errors.Is(closeErr, io.EOF) {
			closeErr = nil
		}
		err = errors.Join(err, closeErr)
		results <- result{err: err}
	}()
	go func() {
		results <- result{providerOutput: true, err: CopyChannel(upstream, downstream)}
	}()
	first := <-results
	if first.providerOutput {
		return first.err
	}
	second := <-results
	return errors.Join(first.err, second.err)
}
