package sdk

import (
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk/kit"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// InvokeBuilder is the ONE Invoke dispatch every builder plugin serves (aur, npm, pixi, cargo,
// mise): the build-time OpResolve (kit.BuilderResolve) and the deploy-time OpCollectContext
// (kit.BuilderCollectContext) and OpReverse (kit.BuilderReverse) for `word`. A builder plugin's
// Invoke is one line that calls it — the four byte-identical copies it replaces drifted only in
// their comments (R2, opencharly/sdk#608). Any other op, an undecodable input, and every error the
// kit leg returns are loud.
func InvokeBuilder(word string, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	var out any
	switch req.GetOp() {
	case OpCollectContext:
		var in spec.BuilderCollectInput
		if err := decodeBuilderParams(word, "collect-context", req.GetParamsJson(), &in); err != nil {
			return nil, err
		}
		ctx, err := kit.BuilderCollectContext(word, in)
		if err != nil {
			return nil, err
		}
		out = spec.BuilderCollectReply{Context: ctx, Package: kit.BuilderPackageOutput(word, in.Candy)}
	case OpReverse:
		var in spec.BuilderReverseInput
		if err := decodeBuilderParams(word, "reverse", req.GetParamsJson(), &in); err != nil {
			return nil, err
		}
		out = spec.BuilderReverseReply{ReverseOps: kit.BuilderReverse(word, in)}
	case OpResolve:
		var in spec.BuilderResolveInput
		if err := decodeBuilderParams(word, "resolve", req.GetParamsJson(), &in); err != nil {
			return nil, err
		}
		reply, err := kit.BuilderResolve(word, in)
		if err != nil {
			return nil, err
		}
		out = reply
	default:
		return nil, fmt.Errorf("builder %q: unsupported op %q (serves only %q, %q, %q)", word, req.GetOp(), OpResolve, OpCollectContext, OpReverse)
	}
	j, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return &pb.InvokeReply{ResultJson: j}, nil
}

func decodeBuilderParams(word, leg string, raw []byte, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("builder %q: decode %s input: %w", word, leg, err)
	}
	return nil
}
