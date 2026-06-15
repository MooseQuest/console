package plugin

import (
	"context"

	"github.com/moosequest/console/internal/plugin/proto"
	"github.com/moosequest/console/internal/store"
)

// grpcServer adapts a store.Store to the generated StoreService server. It runs
// inside the plugin process; store errors are translated to gRPC status codes.
type grpcServer struct {
	proto.UnimplementedStoreServiceServer
	impl store.Store
}

var empty = &proto.Empty{}

func (s *grpcServer) CreateFlag(ctx context.Context, f *proto.Flag) (*proto.Empty, error) {
	return empty, toStatus(s.impl.CreateFlag(ctx, flagFromProto(f)))
}

func (s *grpcServer) GetFlag(ctx context.Context, k *proto.Key) (*proto.Flag, error) {
	f, err := s.impl.GetFlag(ctx, k.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	return flagToProto(f), nil
}

func (s *grpcServer) ListFlags(ctx context.Context, _ *proto.Empty) (*proto.FlagList, error) {
	fs, err := s.impl.ListFlags(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &proto.FlagList{Flags: make([]*proto.Flag, len(fs))}
	for i, f := range fs {
		out.Flags[i] = flagToProto(f)
	}
	return out, nil
}

func (s *grpcServer) UpdateFlag(ctx context.Context, f *proto.Flag) (*proto.Empty, error) {
	return empty, toStatus(s.impl.UpdateFlag(ctx, flagFromProto(f)))
}

func (s *grpcServer) DeleteFlag(ctx context.Context, k *proto.Key) (*proto.Empty, error) {
	return empty, toStatus(s.impl.DeleteFlag(ctx, k.Key))
}

func (s *grpcServer) CreateComponent(ctx context.Context, c *proto.Component) (*proto.Empty, error) {
	return empty, toStatus(s.impl.CreateComponent(ctx, componentFromProto(c)))
}

func (s *grpcServer) GetComponent(ctx context.Context, k *proto.Key) (*proto.Component, error) {
	c, err := s.impl.GetComponent(ctx, k.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	return componentToProto(c), nil
}

func (s *grpcServer) ListComponents(ctx context.Context, _ *proto.Empty) (*proto.ComponentList, error) {
	cs, err := s.impl.ListComponents(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &proto.ComponentList{Components: make([]*proto.Component, len(cs))}
	for i, c := range cs {
		out.Components[i] = componentToProto(c)
	}
	return out, nil
}

func (s *grpcServer) UpdateComponent(ctx context.Context, c *proto.Component) (*proto.Empty, error) {
	return empty, toStatus(s.impl.UpdateComponent(ctx, componentFromProto(c)))
}

func (s *grpcServer) DeleteComponent(ctx context.Context, k *proto.Key) (*proto.Empty, error) {
	return empty, toStatus(s.impl.DeleteComponent(ctx, k.Key))
}

func (s *grpcServer) RecordCheck(ctx context.Context, c *proto.Check) (*proto.Empty, error) {
	return empty, toStatus(s.impl.RecordCheck(ctx, checkFromProto(c)))
}

func (s *grpcServer) LatestCheck(ctx context.Context, k *proto.Key) (*proto.Check, error) {
	c, err := s.impl.LatestCheck(ctx, k.Key)
	if err != nil {
		return nil, toStatus(err)
	}
	return checkToProto(c), nil
}

func (s *grpcServer) LatestChecks(ctx context.Context, _ *proto.Empty) (*proto.CheckList, error) {
	cs, err := s.impl.LatestChecks(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := &proto.CheckList{Checks: make([]*proto.Check, len(cs))}
	for i, c := range cs {
		out.Checks[i] = checkToProto(c)
	}
	return out, nil
}

func (s *grpcServer) Ping(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	return empty, toStatus(s.impl.Ping(ctx))
}
