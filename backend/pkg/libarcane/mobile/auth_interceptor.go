package mobile

import (
	"context"
	"strings"

	mobilepb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/mobile/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// metadataKeyAPIKey is the gRPC metadata key carrying the device token.
// Lowercased per gRPC conventions.
const metadataKeyAPIKey = "x-api-key"

// Context-key types for stamping caller identity into the request context.
type (
	ctxKeyUserID   struct{}
	ctxKeyDeviceID struct{}
)

// UserIDFromContext returns the authenticated user ID from a request context
// produced by the auth interceptor.
func UserIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(ctxKeyUserID{}).(string)
	return v, ok && v != ""
}

// DeviceIDFromContext returns the authenticated device ID from a request
// context produced by the auth interceptor.
func DeviceIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(ctxKeyDeviceID{}).(string)
	return v, ok && v != ""
}

// authUnaryInterceptor validates the x-api-key metadata for any RPC that is
// not on the unauthenticated allowlist. Methods on PairingService are
// allowlisted; methods on MobileService require a valid token. Tunnel-side
// gRPC methods (/tunnel.v1.*) are passed through untouched — that service
// has its own auth at the stream level (see edge/server.go).
func (s *MobileServer) authUnaryInterceptor(_ context.Context) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !methodRequiresAuth(info.FullMethod) {
			return handler(ctx, req)
		}

		userID, deviceID, err := s.validateMetadataToken(ctx)
		if err != nil {
			return nil, statusFromError(err)
		}

		ctx = stampIdentity(ctx, userID, deviceID)
		if s.callbacks.TouchLastSeen != nil {
			go s.callbacks.TouchLastSeen(context.WithoutCancel(ctx), deviceID)
		}
		return handler(ctx, req)
	}
}

func (s *MobileServer) authStreamInterceptor(_ context.Context) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !methodRequiresAuth(info.FullMethod) {
			return handler(srv, ss)
		}

		userID, deviceID, err := s.validateMetadataToken(ss.Context())
		if err != nil {
			return statusFromError(err)
		}

		ctx := stampIdentity(ss.Context(), userID, deviceID)
		if s.callbacks.TouchLastSeen != nil {
			go s.callbacks.TouchLastSeen(context.WithoutCancel(ctx), deviceID)
		}
		return handler(srv, &contextualServerStream{ServerStream: ss, ctx: ctx})
	}
}

// methodRequiresAuth returns true if the given full method name is on the
// authenticated MobileService surface. Returns false for tunnel methods
// (handled by edge package) and for the unauthenticated PairingService.
func methodRequiresAuth(fullMethod string) bool {
	switch {
	case strings.HasPrefix(fullMethod, "/tunnel.v1."):
		return false
	case fullMethod == mobilepb.PairingService_RedeemCode_FullMethodName:
		return false
	case strings.HasPrefix(fullMethod, "/mobile.v1.MobileService/"):
		return true
	}
	// Default: anything else on a non-mobile package falls through unauthenticated;
	// gRPC will return Unimplemented if no service handles it.
	return false
}

func (s *MobileServer) validateMetadataToken(ctx context.Context) (userID, deviceID string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", ErrUnauthenticated
	}
	values := md.Get(metadataKeyAPIKey)
	if len(values) == 0 {
		return "", "", ErrUnauthenticated
	}
	token := strings.TrimSpace(values[0])
	if token == "" {
		return "", "", ErrUnauthenticated
	}

	if s.callbacks.ValidateToken == nil {
		return "", "", ErrUnauthenticated
	}
	userID, deviceID, err = s.callbacks.ValidateToken(ctx, token)
	if err != nil {
		return "", "", err
	}
	if userID == "" || deviceID == "" {
		return "", "", ErrInvalidToken
	}
	return userID, deviceID, nil
}

func stampIdentity(ctx context.Context, userID, deviceID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID{}, userID)
	ctx = context.WithValue(ctx, ctxKeyDeviceID{}, deviceID)
	return ctx
}

type contextualServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextualServerStream) Context() context.Context {
	return s.ctx
}
