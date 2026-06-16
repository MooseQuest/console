package plugin

import (
	"context"
	"fmt"
	"sync"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/notify"
	"github.com/moosequest/console/internal/plugin/proto"
)

// NotifierPluginName is the dispense key for the notify seam.
const NotifierPluginName = "notifier"

// NotifierPlugin is the go-plugin GRPCPlugin for the notify seam.
type NotifierPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl notify.Notifier
}

// GRPCServer registers the notifier service backed by Impl (plugin side).
func (p *NotifierPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterNotifierServiceServer(s, &grpcNotifierServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a notify.Notifier backed by the connection (host side).
func (p *NotifierPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &grpcNotifierClient{client: proto.NewNotifierServiceClient(c)}, nil
}

// NotifierPluginSet returns the go-plugin set for a notifier implementation.
func NotifierPluginSet(impl notify.Notifier) goplugin.PluginSet {
	return goplugin.PluginSet{NotifierPluginName: &NotifierPlugin{Impl: impl}}
}

func eventToProto(ev core.Event) *proto.Event {
	return &proto.Event{
		Type:       string(ev.Type),
		Title:      ev.Title,
		Message:    ev.Message,
		Component:  ev.Component,
		Flag:       ev.Flag,
		From:       int32(ev.From),
		To:         int32(ev.To),
		AtUnixNano: nanos(ev.At),
	}
}

func eventFromProto(p *proto.Event) core.Event {
	return core.Event{
		Type:      core.EventType(p.Type),
		Title:     p.Title,
		Message:   p.Message,
		Component: p.Component,
		Flag:      p.Flag,
		From:      core.HealthState(p.From),
		To:        core.HealthState(p.To),
		At:        fromNanos(p.AtUnixNano),
	}
}

// grpcNotifierServer adapts a notify.Notifier to the generated service (plugin side).
type grpcNotifierServer struct {
	proto.UnimplementedNotifierServiceServer
	impl notify.Notifier
}

func (s *grpcNotifierServer) Name(context.Context, *proto.NotifyEmpty) (*proto.NotifierName, error) {
	return &proto.NotifierName{Name: s.impl.Name()}, nil
}

func (s *grpcNotifierServer) Notify(ctx context.Context, e *proto.Event) (*proto.NotifyEmpty, error) {
	if err := s.impl.Notify(ctx, eventFromProto(e)); err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	return &proto.NotifyEmpty{}, nil
}

// grpcNotifierClient adapts the generated client to notify.Notifier (host side).
type grpcNotifierClient struct {
	client   proto.NotifierServiceClient
	nameOnce sync.Once
	name     string
}

var _ notify.Notifier = (*grpcNotifierClient)(nil)

// Name fetches the plugin's name once and caches it; it is used only for logging.
func (c *grpcNotifierClient) Name() string {
	c.nameOnce.Do(func() {
		c.name = "plugin"
		if resp, err := c.client.Name(context.Background(), &proto.NotifyEmpty{}); err == nil {
			c.name = resp.Name
		}
	})
	return c.name
}

func (c *grpcNotifierClient) Notify(ctx context.Context, ev core.Event) error {
	_, err := c.client.Notify(ctx, eventToProto(ev))
	return err
}

// LoadNotifier launches the notifier-plugin executable at path and returns a
// notify.Notifier backed by it over gRPC. The returned func stops the
// subprocess. The plugin inherits the host's environment.
func LoadNotifier(path string) (notify.Notifier, func() error, error) {
	client := goplugin.NewClient(clientConfig(NotifierPluginSet(nil), path))
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("start notifier plugin %q: %w", path, err)
	}
	raw, err := rpc.Dispense(NotifierPluginName)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense notifier plugin: %w", err)
	}
	n, ok := raw.(notify.Notifier)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("notifier plugin returned %T, want notify.Notifier", raw)
	}
	return n, func() error { client.Kill(); return nil }, nil
}

// ServeNotifier runs a notifier implementation as a plugin. Plugin executables
// call this from main; it blocks until the host disconnects.
func ServeNotifier(impl notify.Notifier) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         NotifierPluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}
