package kit

import (
	"fmt"

	"github.com/opencharly/sdk/spec"
)

// VenueFromDescriptor re-materializes a spec.VenueDescriptor into a real DeployExecutor — the
// decouple point that lets a substrate lifecycle plugin run out-of-process: it hands back a
// serializable venue description over the wire, and whichever side needs a LIVE executor
// (previously only the host, via charly/substrate_lifecycle_grpc.go's now-deleted
// venueFromDescriptor) re-materializes it locally. Promoted here (S3b, Unit-6 design) because the
// function has ZERO core-only dependency (pure sdk/kit + sdk/spec) and now has TWO callers that
// must construct the byte-identical executor from the same descriptor: the host (still
// re-materializing it for --verify / subsequent dispatch calls on the same target) and
// candy/plugin-bundle (materializing its OWN executor for the substrate's Execute call, immediately
// after driving that same substrate's OpPrepareVenue). R3 — one function, not a duplicate in each
// package.
func VenueFromDescriptor(d spec.VenueDescriptor) (spec.DeployExecutor, error) {
	switch d.Kind {
	case "":
		return nil, nil // no venue (e.g. VenueExecutor declining → caller keeps its executor)
	case "shell":
		return ShellExecutor{}, nil
	case "ssh":
		return &SSHExecutor{User: d.User, Host: d.Host, Port: d.Port, Args: d.Args, ConnectTimeout: d.ConnectTimeout}, nil
	default:
		return nil, fmt.Errorf("venue descriptor: unknown kind %q", d.Kind)
	}
}

// DescriptorFromExecutor is the pure INVERSE of VenueFromDescriptor: it derives a serializable
// spec.VenueDescriptor from an already-materialized, LIVE executor value. Only the two round-
// trippable shapes VenueFromDescriptor understands ("shell"/"ssh") are recognized; any other
// concrete type (e.g. a composed *NestedExecutor, which cannot be flattened into one
// {kind,host,port} tuple) returns the zero VenueDescriptor{} — callers treat that exactly like
// VenueFromDescriptor's own "" case (no venue; keep whatever executor is already in hand).
//
// The bed-regression fix this promotion serves (FIX ROUND, S3b follow-up): a NESTED external
// deploy's ancestor executor (deploykit.RootExecutorForDeployNode's "ssh" result, threaded core-
// side as EmitOpts.ParentExec via the ancestor-chain walk in charly/bundle_add_cmd.go's
// deriveChildExecutorForPath) is ALWAYS a plain ShellExecutor/*SSHExecutor for a single hop into a
// vm guest — never a NestedExecutor — so it round-trips through this exact pair of functions.
// charly/unified_targets.go's pluginDeployTarget.Add uses this to convert that live ancestor
// executor into venue_json BEFORE dispatch, since a live Go interface value cannot itself cross
// the []byte wire to candy/plugin-bundle (mirrors candy/plugin-bundle's OWN identically-shaped
// former venueDescriptorFromExecutor, now deleted — R3, one function for both directions' callers).
func DescriptorFromExecutor(exec spec.DeployExecutor) spec.VenueDescriptor {
	switch e := exec.(type) {
	case ShellExecutor:
		return spec.VenueDescriptor{Kind: "shell"}
	case *SSHExecutor:
		return spec.VenueDescriptor{Kind: "ssh", User: e.User, Host: e.Host, Port: e.Port, Args: e.Args, ConnectTimeout: e.ConnectTimeout}
	default:
		return spec.VenueDescriptor{}
	}
}
