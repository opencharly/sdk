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
