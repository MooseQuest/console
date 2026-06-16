package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/plugin/proto"
	"github.com/moosequest/console/internal/status"
)

// StatusProviderPluginName is the dispense key for the status seam.
const StatusProviderPluginName = "status_provider"

// StatusProviderPlugin is the go-plugin GRPCPlugin for the status seam. On the
// plugin side Impl holds the real provider; on the host side it is nil and
// GRPCClient returns a client adapter.
type StatusProviderPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl status.Provider
}

// GRPCServer registers the status-provider service backed by Impl (plugin side).
func (p *StatusProviderPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterStatusProviderServiceServer(s, &grpcStatusServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a status.Provider backed by the connection (host side).
func (p *StatusProviderPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &grpcStatusClient{client: proto.NewStatusProviderServiceClient(c)}, nil
}

// StatusProviderPluginSet returns the go-plugin set for a status provider.
func StatusProviderPluginSet(impl status.Provider) goplugin.PluginSet {
	return goplugin.PluginSet{StatusProviderPluginName: &StatusProviderPlugin{Impl: impl}}
}

func componentToStatusProto(c core.Component) *proto.StatusComponent {
	return &proto.StatusComponent{
		Key:         c.Key,
		Name:        c.Name,
		Description: c.Description,
		Provider:    c.Provider,
		Config:      c.Config,
	}
}

func componentFromStatusProto(p *proto.StatusComponent) core.Component {
	return core.Component{
		Key:         p.Key,
		Name:        p.Name,
		Description: p.Description,
		Provider:    p.Provider,
		Config:      p.Config,
	}
}

func checkToStatusProto(c core.Check) *proto.StatusCheck {
	return &proto.StatusCheck{
		Component:         c.Component,
		State:             int32(c.State),
		Message:           c.Message,
		LatencyNanos:      int64(c.Latency),
		CheckedAtUnixNano: nanos(c.CheckedAt),
	}
}

func checkFromStatusProto(p *proto.StatusCheck) core.Check {
	return core.Check{
		Component: p.Component,
		State:     core.HealthState(p.State),
		Message:   p.Message,
		Latency:   time.Duration(p.LatencyNanos),
		CheckedAt: fromNanos(p.CheckedAtUnixNano),
	}
}

// grpcStatusServer adapts a status.Provider to the generated service (plugin side).
type grpcStatusServer struct {
	proto.UnimplementedStatusProviderServiceServer
	impl status.Provider
}

func (s *grpcStatusServer) Name(context.Context, *proto.StatusEmpty) (*proto.StatusName, error) {
	return &proto.StatusName{Name: s.impl.Name()}, nil
}

func (s *grpcStatusServer) Check(ctx context.Context, c *proto.StatusComponent) (*proto.StatusCheck, error) {
	return checkToStatusProto(s.impl.Check(ctx, componentFromStatusProto(c))), nil
}

// grpcStatusClient adapts the generated client to status.Provider (host side).
type grpcStatusClient struct {
	client   proto.StatusProviderServiceClient
	nameOnce sync.Once
	name     string
}

var _ status.Provider = (*grpcStatusClient)(nil)

// Name fetches the plugin's name once and caches it; it is used for component
// dispatch and logging.
func (c *grpcStatusClient) Name() string {
	c.nameOnce.Do(func() {
		c.name = "plugin"
		if resp, err := c.client.Name(context.Background(), &proto.StatusEmpty{}); err == nil {
			c.name = resp.Name
		}
	})
	return c.name
}

// Check probes the component over gRPC. Because status.Provider.Check returns no
// error, a transport failure is surfaced as an Unknown check on the component.
func (c *grpcStatusClient) Check(ctx context.Context, comp core.Component) core.Check {
	resp, err := c.client.Check(ctx, componentToStatusProto(comp))
	if err != nil {
		return core.Check{
			Component: comp.Key,
			State:     core.StateUnknown,
			Message:   "status plugin: " + err.Error(),
			CheckedAt: time.Now().UTC(),
		}
	}
	return checkFromStatusProto(resp)
}

// LoadStatusProvider launches the status-provider plugin executable at path and
// returns a status.Provider backed by it over gRPC. The returned func stops the
// subprocess. The plugin inherits the host's environment.
func LoadStatusProvider(path string) (status.Provider, func() error, error) {
	client := goplugin.NewClient(clientConfig(StatusProviderPluginSet(nil), path))
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("start status plugin %q: %w", path, err)
	}
	raw, err := rpc.Dispense(StatusProviderPluginName)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense status plugin: %w", err)
	}
	p, ok := raw.(status.Provider)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("status plugin returned %T, want status.Provider", raw)
	}
	return p, func() error { client.Kill(); return nil }, nil
}

// ServeStatusProvider runs a status-provider implementation as a plugin. Plugin
// executables call this from main; it blocks until the host disconnects.
func ServeStatusProvider(impl status.Provider) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         StatusProviderPluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}
