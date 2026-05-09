package mobile

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recovery, logging, and selective-auth interceptors for the mobile gRPC
// services. The recovery and logging shapes mirror
// pkg/libarcane/edge/server.go:602-639.

func (s *MobileServer) recoveryUnaryInterceptor(_ context.Context) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(ctx, "panic in mobile gRPC unary handler",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal mobile error")
			}
		}()
		return handler(ctx, req)
	}
}

func (s *MobileServer) loggingUnaryInterceptor(_ context.Context) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		if err != nil {
			slog.WarnContext(ctx, "mobile gRPC call failed",
				"method", info.FullMethod,
				"duration", duration,
				"error", err)
			return resp, err
		}

		slog.DebugContext(ctx, "mobile gRPC call completed",
			"method", info.FullMethod,
			"duration", duration)
		return resp, nil
	}
}

func (s *MobileServer) recoveryStreamInterceptor(_ context.Context) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.ErrorContext(ss.Context(), "panic in mobile gRPC stream handler",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal mobile error")
			}
		}()
		return handler(srv, ss)
	}
}

func (s *MobileServer) loggingStreamInterceptor(_ context.Context) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)

		if err != nil {
			slog.WarnContext(ss.Context(), "mobile gRPC stream failed",
				"method", info.FullMethod,
				"duration", duration,
				"error", err)
			return err
		}

		slog.DebugContext(ss.Context(), "mobile gRPC stream completed",
			"method", info.FullMethod,
			"duration", duration)
		return nil
	}
}
