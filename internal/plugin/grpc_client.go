package plugin

import (
	"context"

	"github.com/moosequest/console/internal/core"
	"github.com/moosequest/console/internal/plugin/proto"
	"github.com/moosequest/console/internal/store"
)

// grpcClient adapts the generated StoreService client to store.Store. It runs
// in the host process. The subprocess lifecycle is owned by the host loader
// (LoadStore); Close here is a no-op so callers can treat it like any store.
type grpcClient struct {
	client proto.StoreServiceClient
}

// compile-time check that the client satisfies the store interface.
var _ store.Store = (*grpcClient)(nil)

func (c *grpcClient) CreateFlag(ctx context.Context, f core.Flag) error {
	_, err := c.client.CreateFlag(ctx, flagToProto(f))
	return fromStatus(err)
}

func (c *grpcClient) GetFlag(ctx context.Context, key string) (core.Flag, error) {
	f, err := c.client.GetFlag(ctx, &proto.Key{Key: key})
	if err != nil {
		return core.Flag{}, fromStatus(err)
	}
	return flagFromProto(f), nil
}

func (c *grpcClient) ListFlags(ctx context.Context) ([]core.Flag, error) {
	list, err := c.client.ListFlags(ctx, empty)
	if err != nil {
		return nil, fromStatus(err)
	}
	out := make([]core.Flag, len(list.Flags))
	for i, f := range list.Flags {
		out[i] = flagFromProto(f)
	}
	return out, nil
}

func (c *grpcClient) UpdateFlag(ctx context.Context, f core.Flag) error {
	_, err := c.client.UpdateFlag(ctx, flagToProto(f))
	return fromStatus(err)
}

func (c *grpcClient) DeleteFlag(ctx context.Context, key string) error {
	_, err := c.client.DeleteFlag(ctx, &proto.Key{Key: key})
	return fromStatus(err)
}

func (c *grpcClient) CreateComponent(ctx context.Context, comp core.Component) error {
	_, err := c.client.CreateComponent(ctx, componentToProto(comp))
	return fromStatus(err)
}

func (c *grpcClient) GetComponent(ctx context.Context, key string) (core.Component, error) {
	comp, err := c.client.GetComponent(ctx, &proto.Key{Key: key})
	if err != nil {
		return core.Component{}, fromStatus(err)
	}
	return componentFromProto(comp), nil
}

func (c *grpcClient) ListComponents(ctx context.Context) ([]core.Component, error) {
	list, err := c.client.ListComponents(ctx, empty)
	if err != nil {
		return nil, fromStatus(err)
	}
	out := make([]core.Component, len(list.Components))
	for i, comp := range list.Components {
		out[i] = componentFromProto(comp)
	}
	return out, nil
}

func (c *grpcClient) UpdateComponent(ctx context.Context, comp core.Component) error {
	_, err := c.client.UpdateComponent(ctx, componentToProto(comp))
	return fromStatus(err)
}

func (c *grpcClient) DeleteComponent(ctx context.Context, key string) error {
	_, err := c.client.DeleteComponent(ctx, &proto.Key{Key: key})
	return fromStatus(err)
}

func (c *grpcClient) RecordCheck(ctx context.Context, ck core.Check) error {
	_, err := c.client.RecordCheck(ctx, checkToProto(ck))
	return fromStatus(err)
}

func (c *grpcClient) LatestCheck(ctx context.Context, component string) (core.Check, error) {
	ck, err := c.client.LatestCheck(ctx, &proto.Key{Key: component})
	if err != nil {
		return core.Check{}, fromStatus(err)
	}
	return checkFromProto(ck), nil
}

func (c *grpcClient) LatestChecks(ctx context.Context) ([]core.Check, error) {
	list, err := c.client.LatestChecks(ctx, empty)
	if err != nil {
		return nil, fromStatus(err)
	}
	out := make([]core.Check, len(list.Checks))
	for i, ck := range list.Checks {
		out[i] = checkFromProto(ck)
	}
	return out, nil
}

func (c *grpcClient) Ping(ctx context.Context) error {
	_, err := c.client.Ping(ctx, empty)
	return fromStatus(err)
}

// Close is a no-op; the host loader owns the plugin subprocess lifecycle.
func (c *grpcClient) Close() error { return nil }
