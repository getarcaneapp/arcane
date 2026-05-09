package mobile

import (
	"context"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
	"google.golang.org/grpc"
)

// MobileServer is the gRPC handler for both PairingService and MobileService.
// Construct via NewMobileServer with a fully populated Callbacks struct, then
// register on the shared *grpc.Server alongside any other services.
//
// Mirrors the shape of pkg/libarcane/edge/TunnelServer (see edge/server.go).
type MobileServer struct {
	mobilepb.UnimplementedPairingServiceServer
	mobilepb.UnimplementedMobileServiceServer

	callbacks Callbacks
}

// NewMobileServer constructs a MobileServer with the given callbacks.
// Callbacks may be nil-checked at call time; bootstrap should always populate
// every callback to avoid runtime nils.
func NewMobileServer(callbacks Callbacks) *MobileServer {
	return &MobileServer{callbacks: callbacks}
}

// Register attaches both PairingService and MobileService to the given
// gRPC server. This is a convenience for bootstrap.
func (s *MobileServer) Register(grpcServer *grpc.Server) {
	mobilepb.RegisterPairingServiceServer(grpcServer, s)
	mobilepb.RegisterMobileServiceServer(grpcServer, s)
}

// GRPCServerOptions returns the chained interceptors for the mobile services.
// The auth interceptor is selective by method: PairingService is unauthenticated,
// MobileService requires x-api-key metadata, tunnel.v1.* is passed through.
//
// When sharing a *grpc.Server with the edge tunnel, the auth interceptor here
// is safe to apply globally because it explicitly skips /tunnel.v1.* methods.
func (s *MobileServer) GRPCServerOptions(ctx context.Context) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			s.recoveryUnaryInterceptor(ctx),
			s.loggingUnaryInterceptor(ctx),
			s.authUnaryInterceptor(ctx),
		),
		grpc.ChainStreamInterceptor(
			s.recoveryStreamInterceptor(ctx),
			s.loggingStreamInterceptor(ctx),
			s.authStreamInterceptor(ctx),
		),
	}
}
