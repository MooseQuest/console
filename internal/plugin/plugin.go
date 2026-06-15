// Package plugin implements Console's out-of-process plugin system using
// hashicorp/go-plugin over gRPC. The host (the console binary) launches a
// plugin executable, performs a handshake, and dispenses a typed client that
// satisfies the corresponding core interface — so engines stay unaware that an
// implementation lives in another process.
//
// This package currently implements the storage seam (store.Store); other seams
// follow the same shape.
package plugin

import (
	"context"
	"errors"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/plugin/proto"
	"github.com/moosequest/console/internal/store"
)

// Handshake is shared by host and plugin; a mismatch makes go-plugin refuse to
// run the binary, guarding against accidentally executing a non-plugin.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "CONSOLE_PLUGIN",
	MagicCookieValue: "console-plugin-v1",
}

// StorePluginName is the dispense key for the storage seam.
const StorePluginName = "store"

// StorePlugin is the go-plugin GRPCPlugin for the storage seam. On the plugin
// side Impl holds the real store; on the host side it is nil and GRPCClient
// returns a client adapter.
type StorePlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl store.Store
}

// GRPCServer registers the store service backed by Impl (plugin side).
func (p *StorePlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterStoreServiceServer(s, &grpcServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a store.Store backed by the gRPC connection (host side).
func (p *StorePlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &grpcClient{client: proto.NewStoreServiceClient(c)}, nil
}

// PluginSet returns the go-plugin plugin set for a store implementation. With a
// nil impl it is suitable for the host (dispensing a client); with a real impl
// it is suitable for the plugin (serving it).
func PluginSet(impl store.Store) goplugin.PluginSet {
	return goplugin.PluginSet{StorePluginName: &StorePlugin{Impl: impl}}
}

// toStatus maps a store error to a gRPC status so the sentinel survives the
// process boundary.
func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, core.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, core.ErrConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Unknown, err.Error())
	}
}

// fromStatus maps a gRPC status back to the store sentinel errors.
func fromStatus(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return core.ErrNotFound
	case codes.AlreadyExists:
		return core.ErrConflict
	default:
		return err
	}
}
