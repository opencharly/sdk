package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/opencharly/sdk/kit"
	pb "github.com/opencharly/sdk/proto"
	"github.com/opencharly/sdk/spec"
)

// ServeCheckVerb serves a HOST-COUPLED check verb (kit.CheckVerbProvider) OUT-OF-PROCESS
// (F2): it wraps the verb in a pb.ProviderServer whose Invoke reconstructs a kit.CheckContext
// from the host's reverse channel (ExecutorService for Exec + CheckContextService for
// HTTPDo/AddBackground, on the InvokeRequest broker) plus the env_json CheckEnv snapshot
// (Mode/Box/Instance/Distros/DialTimeout), runs RunVerb, and returns the verdict. The SAME
// kit verb compiles INTO charly in-process (registerCompiledCheckVerb passes the live *Runner
// as the CheckContext); this is the out-of-process placement, ZERO authoring change. A kit
// candy's cmd/serve is the SAME one-liner shape as every pb-provider plugin:
// sdk.ServeCheckVerb(pkg.NewCheckVerb(), pkg.NewMeta()) — meta is the ONE shared NewMeta the
// candy also exports, unifying kit candies with the pb-provider authoring shape (R3).
func ServeCheckVerb(kv kit.CheckVerbProvider, meta pb.PluginMetaServer) {
	Serve(&checkVerbServer{kv: kv}, meta)
}

// checkVerbServer is the pb.ProviderServer that runs a kit verb out-of-process.
type checkVerbServer struct {
	pb.UnimplementedProviderServer
	kv kit.CheckVerbProvider
}

func kitStatusWire(s kit.Status) string {
	switch s {
	case kit.StatusFail:
		return "fail"
	case kit.StatusSkip:
		return "skip"
	default:
		return "pass"
	}
}

func (s *checkVerbServer) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	var op spec.Op
	if len(req.GetParamsJson()) > 0 {
		if err := json.Unmarshal(req.GetParamsJson(), &op); err != nil {
			return ResultJSON("fail", "check verb: decode op: "+err.Error())
		}
	}
	var env spec.CheckEnv
	if len(req.GetEnvJson()) > 0 {
		_ = json.Unmarshal(req.GetEnvJson(), &env)
	}
	cc, err := newSDKCheckContext(req.GetExecutorBrokerId(), env)
	if err != nil {
		return ResultJSON("fail", "check verb: reverse channel: "+err.Error())
	}
	res := s.kv.RunVerb(ctx, cc, &op)
	return ResultJSON(kitStatusWire(res.Status), res.Message)
}

func (s *checkVerbServer) InvokeStream(req *pb.InvokeRequest, stream pb.Provider_InvokeStreamServer) error {
	rep, err := s.Invoke(stream.Context(), req)
	if err != nil {
		return err
	}
	return stream.Send(&pb.Frame{ResultJson: rep.GetResultJson()})
}

// newSDKCheckContext builds the kit.CheckContext an out-of-process kit verb consumes: Exec
// over ExecutorService, HTTPDo/AddBackground over CheckContextService, and the scalar legs from
// the env snapshot. It dials the broker exactly ONCE: the host serves BOTH reverse services on
// the SAME broker id (one grpc.Server via InvokeWithExecutor), and go-plugin's GRPCBroker
// pairs ONE Dial with ONE AcceptAndServe per id — a second Dial would hang ("timeout waiting
// for connection info"). gRPC multiplexes both service clients on the single conn.
func newSDKCheckContext(brokerID uint32, env spec.CheckEnv) (kit.CheckContext, error) {
	if servedBroker == nil {
		return nil, errors.New("sdk: no go-plugin broker (plugin not served over go-plugin)")
	}
	if brokerID == 0 {
		return nil, errors.New("sdk: no host reverse channel attached (executor_broker_id=0)")
	}
	conn, err := servedBroker.Dial(brokerID)
	if err != nil {
		return nil, err
	}
	return &sdkCheckContext{
		exec: &Executor{client: pb.NewExecutorServiceClient(conn)},
		cc:   pb.NewCheckContextServiceClient(conn),
		env:  env,
	}, nil
}

// sdkCheckContext is the OUT-OF-PROCESS kit.CheckContext: the plugin-side twin of charly's
// in-proc runnerCheckContext, backed by the reverse channel + the env snapshot.
type sdkCheckContext struct {
	exec *Executor
	cc   pb.CheckContextServiceClient
	env  spec.CheckEnv
}

func (c *sdkCheckContext) Exec() kit.Executor {
	return &sdkKitExecutor{e: c.exec, kind: c.env.VenueKind}
}

func (c *sdkCheckContext) Mode() kit.RunMode {
	if c.env.Mode == "box" {
		return kit.ModeBox
	}
	return kit.ModeLive
}

func (c *sdkCheckContext) Box() string                { return c.env.Box }
func (c *sdkCheckContext) Instance() string           { return c.env.Instance }
func (c *sdkCheckContext) Distros() []string          { return c.env.Distros }
func (c *sdkCheckContext) DialTimeout() time.Duration { return time.Duration(c.env.DialTimeoutNs) }

func (c *sdkCheckContext) HTTPDo(ctx context.Context, req kit.HTTPRequest) (kit.HTTPResponse, error) {
	rep, err := c.cc.HTTPDo(ctx, &pb.HTTPDoRequest{
		Method:            req.Method,
		Url:               req.URL,
		Body:              req.Body,
		Headers:           req.Headers,
		Timeout:           req.Timeout,
		AllowInsecure:     req.AllowInsecure,
		NoFollowRedirects: req.NoFollowRedirects,
		CaPem:             req.CAPEM,
	})
	if err != nil {
		return kit.HTTPResponse{}, err
	}
	if rep.GetError() != "" {
		return kit.HTTPResponse{}, errors.New(rep.GetError())
	}
	return kit.HTTPResponse{Status: int(rep.GetStatus()), Body: rep.GetBody(), HeaderBlob: rep.GetHeaderBlob()}, nil
}

func (c *sdkCheckContext) AddBackground(pid int) {
	_, _ = c.cc.AddBackground(context.Background(), &pb.AddBackgroundRequest{Pid: int32(pid)})
}

func (c *sdkCheckContext) ResolveEndpoint(ctx context.Context, port int) (string, error) {
	rep, err := c.cc.ResolveEndpoint(ctx, &pb.ResolveEndpointRequest{Port: int32(port)})
	if err != nil {
		return "", err
	}
	if rep.GetError() != "" {
		return "", errors.New(rep.GetError())
	}
	return rep.GetAddr(), nil
}

func (c *sdkCheckContext) ResolveGraphicsEndpoint(ctx context.Context, kind string) (kit.GraphicsEndpoint, error) {
	rep, err := c.cc.ResolveGraphicsEndpoint(ctx, &pb.ResolveGraphicsEndpointRequest{Kind: kind})
	if err != nil {
		return kit.GraphicsEndpoint{}, err
	}
	if rep.GetError() != "" {
		return kit.GraphicsEndpoint{}, errors.New(rep.GetError())
	}
	return kit.GraphicsEndpoint{
		Addr:        rep.GetAddr(),
		Socket:      rep.GetSocket(),
		Password:    rep.GetPassword(),
		Skip:        rep.GetSkip(),
		SkipMessage: rep.GetSkipMessage(),
	}, nil
}

func (c *sdkCheckContext) ResolveClusterContext(ctx context.Context, cluster string) (string, error) {
	rep, err := c.cc.ResolveClusterContext(ctx, &pb.ResolveClusterContextRequest{Cluster: cluster})
	if err != nil {
		return "", err
	}
	if rep.GetError() != "" {
		return "", errors.New(rep.GetError())
	}
	return rep.GetContext(), nil
}

func (c *sdkCheckContext) ResolveImageLabel(ctx context.Context, label string) (string, error) {
	rep, err := c.cc.ResolveImageLabel(ctx, &pb.ResolveImageLabelRequest{Label: label})
	if err != nil {
		return "", err
	}
	if rep.GetError() != "" {
		return "", errors.New(rep.GetError())
	}
	return rep.GetValue(), nil
}

// NewCheckContext builds the out-of-process kit.CheckContext for a RAW pb.Provider (Invoke)
// that needs the reverse-channel legs (ResolveEndpoint / HTTPDo / Exec) but is NOT a
// kit.CheckVerbProvider (so the kit-verb serve path never built one for it). envJSON is the
// InvokeRequest.env_json (the host's CheckEnv snapshot). Dials the broker ONCE — do NOT also
// call ExecutorFromInvoke on the same Invoke (a second Dial hangs; use cc.Exec() instead).
func NewCheckContext(brokerID uint32, envJSON []byte) (kit.CheckContext, error) {
	var env spec.CheckEnv
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &env); err != nil {
			return nil, fmt.Errorf("sdk: decode check env: %w", err)
		}
	}
	return newSDKCheckContext(brokerID, env)
}

// sdkKitExecutor adapts the plugin-side *sdk.Executor to kit.Executor (RunCapture over the
// reverse channel; Kind from the env's venue_kind, since the executor's Kind is a host fact).
type sdkKitExecutor struct {
	e    *Executor
	kind string
}

func (x *sdkKitExecutor) RunCapture(ctx context.Context, script string) (string, string, int, error) {
	return x.e.RunCapture(ctx, script)
}

func (x *sdkKitExecutor) Kind() string { return x.kind }
