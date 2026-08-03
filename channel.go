package sdk

// channel.go — re-exports of the channel streaming surface relocated to
// github.com/opencharly/spec/transport (#55 import-purity). The frame-kind
// constants, the ProviderChannel/ChannelProvider interfaces, the
// ReceiveChannelOpen/OpenProviderChannel open-frame helpers, and the
// SequenceGate/ReplayBuffer ordering primitives live once in spec/transport;
// the sdk root re-exports them so candy call sites compile UNCHANGED.

import (
	"github.com/opencharly/spec/transport"
)

// Channel frame kinds are transport vocabulary, not runtime semantics. Runtime-
// specific events travel as CUE-generated JSON in ChannelFrame.PayloadJson.
const (
	ChannelOpen     = transport.ChannelOpen
	ChannelStdin    = transport.ChannelStdin
	ChannelStdout   = transport.ChannelStdout
	ChannelStderr   = transport.ChannelStderr
	ChannelTerminal = transport.ChannelTerminal
	ChannelStatus   = transport.ChannelStatus
	ChannelResize   = transport.ChannelResize
	ChannelSignal   = transport.ChannelSignal
	ChannelAck      = transport.ChannelAck
	ChannelCancel   = transport.ChannelCancel
	ChannelExit     = transport.ChannelExit
	ChannelError    = transport.ChannelError
	ChannelResync   = transport.ChannelResync
)

// ProviderChannel is the common subset of the generated client and server
// streams. It lets in-process and gRPC providers share one channel handler.
type ProviderChannel = transport.ProviderChannel

// ChannelProvider is the optional streaming extension to Provider. The first
// frame has already been validated as an open frame and remains available as
// open; subsequent controller frames arrive through stream. Domain payloads are
// generated from CUE and carried in open.PayloadJson.
type ChannelProvider = transport.ChannelProvider

// ReceiveChannelOpen reads and validates the mandatory first frame. The
// request id, provider class/word, and operation are required so every later
// frame can be correlated without inspecting runtime-specific payloads.
var ReceiveChannelOpen = transport.ReceiveChannelOpen

// OpenProviderChannel starts a generated Provider.Channel stream and sends its
// mandatory open frame. The returned stream is ready for concurrent Send/Recv,
// as supported by gRPC.
var OpenProviderChannel = transport.OpenProviderChannel

// SequenceGate rejects duplicates, regressions, and gaps. A provider can turn
// a gap into ChannelResync using ReplayBuffer.ReplayFrom; it must never silently
// reorder process or terminal output.
type SequenceGate = transport.SequenceGate

// NewSequenceGate constructs a SequenceGate whose next-expected sequence is first.
var NewSequenceGate = transport.NewSequenceGate

// ReplayBuffer is a bounded, acknowledgement-aware frame history for detach /
// reconnect. Bounds are enforced by both frame count and protobuf byte size.
type ReplayBuffer = transport.ReplayBuffer

// NewReplayBuffer constructs a ReplayBuffer capped at maxFrames frames / maxBytes bytes.
var NewReplayBuffer = transport.NewReplayBuffer

// CopyChannel relays frames until EOF or cancellation. It is intentionally a
// byte-preserving transport primitive; it does not inspect agent or terminal
// payloads.
var CopyChannel = transport.CopyChannel

// RelayChannel connects a controller-side ProviderChannel to a downstream gRPC
// channel with ordered half-close semantics. See spec/transport for the
// cancellation-ownership contract.
var RelayChannel = transport.RelayChannel
