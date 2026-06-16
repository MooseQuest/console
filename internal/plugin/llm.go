package plugin

import (
	"context"
	"fmt"
	"sync"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/moosequest/console/internal/llm"
	"github.com/moosequest/console/internal/plugin/proto"
)

// LLMPluginName is the dispense key for the LLM seam.
const LLMPluginName = "llm"

// LLMPlugin is the go-plugin GRPCPlugin for the LLM seam. On the plugin side
// Impl holds the real provider; on the host side it is nil and GRPCClient
// returns a client adapter.
type LLMPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl llm.Provider
}

// GRPCServer registers the LLM service backed by Impl (plugin side).
func (p *LLMPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterLLMServiceServer(s, &grpcLLMServer{impl: p.Impl})
	return nil
}

// GRPCClient returns an llm.Provider backed by the connection (host side).
func (p *LLMPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &grpcLLMClient{client: proto.NewLLMServiceClient(c)}, nil
}

// LLMPluginSet returns the go-plugin set for an LLM implementation.
func LLMPluginSet(impl llm.Provider) goplugin.PluginSet {
	return goplugin.PluginSet{LLMPluginName: &LLMPlugin{Impl: impl}}
}

func requestToProto(req llm.Request) *proto.CompleteRequest {
	msgs := make([]*proto.LLMMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = &proto.LLMMessage{Role: string(m.Role), Text: m.Text}
	}
	return &proto.CompleteRequest{
		System:    req.System,
		Messages:  msgs,
		MaxTokens: int32(req.MaxTokens),
		Model:     req.Model,
	}
}

func requestFromProto(p *proto.CompleteRequest) llm.Request {
	msgs := make([]llm.Message, len(p.Messages))
	for i, m := range p.Messages {
		msgs[i] = llm.Message{Role: llm.Role(m.Role), Text: m.Text}
	}
	return llm.Request{
		System:    p.System,
		Messages:  msgs,
		MaxTokens: int(p.MaxTokens),
		Model:     p.Model,
	}
}

// grpcLLMServer adapts an llm.Provider to the generated service (plugin side).
type grpcLLMServer struct {
	proto.UnimplementedLLMServiceServer
	impl llm.Provider
}

func (s *grpcLLMServer) Name(context.Context, *proto.LLMEmpty) (*proto.LLMName, error) {
	return &proto.LLMName{Name: s.impl.Name()}, nil
}

func (s *grpcLLMServer) Complete(ctx context.Context, req *proto.CompleteRequest) (*proto.CompleteResponse, error) {
	text, err := s.impl.Complete(ctx, requestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &proto.CompleteResponse{Text: text}, nil
}

// grpcLLMClient adapts the generated client to llm.Provider (host side).
type grpcLLMClient struct {
	client   proto.LLMServiceClient
	nameOnce sync.Once
	name     string
}

var _ llm.Provider = (*grpcLLMClient)(nil)

// Name fetches the plugin's name once and caches it; it is used only for logging.
func (c *grpcLLMClient) Name() string {
	c.nameOnce.Do(func() {
		c.name = "plugin"
		if resp, err := c.client.Name(context.Background(), &proto.LLMEmpty{}); err == nil {
			c.name = resp.Name
		}
	})
	return c.name
}

func (c *grpcLLMClient) Complete(ctx context.Context, req llm.Request) (string, error) {
	resp, err := c.client.Complete(ctx, requestToProto(req))
	if err != nil {
		return "", fromStatus(err)
	}
	return resp.Text, nil
}

// LoadLLM launches the LLM plugin executable at path and returns an llm.Provider
// backed by it over gRPC. The returned func stops the subprocess. The plugin
// inherits the host's environment.
func LoadLLM(path string) (llm.Provider, func() error, error) {
	client := goplugin.NewClient(clientConfig(LLMPluginSet(nil), path))
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("start llm plugin %q: %w", path, err)
	}
	raw, err := rpc.Dispense(LLMPluginName)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense llm plugin: %w", err)
	}
	p, ok := raw.(llm.Provider)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("llm plugin returned %T, want llm.Provider", raw)
	}
	return p, func() error { client.Kill(); return nil }, nil
}

// ServeLLM runs an LLM implementation as a plugin. Plugin executables call this
// from main; it blocks until the host disconnects.
func ServeLLM(impl llm.Provider) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         LLMPluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}
