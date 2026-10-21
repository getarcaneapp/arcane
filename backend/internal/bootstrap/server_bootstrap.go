package bootstrap

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"go.uber.org/fx"
	"google.golang.org/grpc"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

var serverOptions = fx.Options(
	fx.Provide(
		newRouter,
		newJobScheduler,
		NewHTTPServer,
	),
)

type HTTPServerParams struct {
	fx.In

	AppCtx       context.Context
	Config       *config.Config
	Router       *echo.Echo
	TunnelServer *edge.TunnelServer
	Scheduler    types.JobScheduler
}

// NewHTTPServer builds the shared HTTP/gRPC server and registers its lifecycle.
func NewHTTPServer(lc fx.Lifecycle, p HTTPServerParams) (*http.Server, error) {
	listenAddr := p.Config.ListenAddr()
	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLS(p.AppCtx, p.Config)
	if err != nil {
		return nil, err
	}
	if p.TunnelServer != nil {
		p.TunnelServer.Config = edgeCfg
	}

	// Managers serve the gRPC edge tunnel on the shared listener.
	var httpHandler http.Handler = p.Router
	var grpcServer *grpc.Server
	if !p.Config.AgentMode && p.TunnelServer != nil {
		//nolint:contextcheck // Stream interceptors receive request-scoped context from grpc.ServerStream.
		grpcServer = grpc.NewServer(p.TunnelServer.GRPCServerOptions()...)
		tunnelpb.RegisterTunnelServiceServer(grpcServer, p.TunnelServer)
		httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isTunnelGRPCRequest(r) {
				grpcServer.ServeHTTP(w, normalizeTunnelGRPCRequestPath(r))
				return
			}
			p.Router.ServeHTTP(w, r)
		})
		slog.InfoContext(p.AppCtx, "Using shared HTTP/gRPC listener for edge tunnel", "addr", listenAddr)
	}
	httpHandler, protocols := configureHTTPProtocols(useTLS, httpHandler)

	var tlsConfig *tls.Config
	if useTLS {
		certificate, loadErr := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
		if loadErr != nil {
			return nil, loadErr
		}
		edgeTLSConfig, tlsErr := edge.BuildManagerServerTLSConfig(edgeCfg)
		if tlsErr != nil {
			return nil, tlsErr
		}
		tlsConfig = cmp.Or(edgeTLSConfig, &tls.Config{})
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	// Request contexts deliberately do not inherit the app lifecycle marker.
	baseCtx, cancelBase := context.WithCancel(context.Background()) //nolint:forbidigo // HTTP requests need a root without the application lifecycle marker.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httpHandler,
		Protocols:         protocols,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		// Long-lived stream handlers tune dead-peer detection per connection; see handlerutil.SetDeadPeerTimeout.
		ConnContext: httpx.WithConn,
		// Edge gRPC tunnels are one long HTTP/2 stream, so the 1 MiB default window would cap agent throughput per RTT.
		HTTP2: &http.HTTP2Config{MaxReceiveBufferPerStream: 8 << 20, MaxReceiveBufferPerConnection: 16 << 20},
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			listener, listenErr := new(net.ListenConfig).Listen(ctx, "tcp", listenAddr)
			if listenErr != nil {
				cancelBase()
				return listenErr
			}
			srv.Addr = listener.Addr().String()

			slog.InfoContext(ctx, "Starting HTTP server", "addr", listenAddr, "listen", p.Config.Listen, "port", p.Config.Port, "tlsEnabled", useTLS)
			go func() {
				defer func() { _ = listener.Close() }()
				var serveErr error
				if useTLS {
					serveErr = srv.ServeTLS(listener, "", "")
				} else {
					serveErr = srv.Serve(listener)
				}
				if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					slog.ErrorContext(p.AppCtx, "HTTP server exited with error", "error", serveErr)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownDone := make(chan error, 1)
			go func() { shutdownDone <- srv.Shutdown(ctx) }()
			drainTimer := time.NewTimer(3 * time.Second)
			var shutdownErr error
			select {
			case shutdownErr = <-shutdownDone:
				drainTimer.Stop()
				cancelBase()
			case <-drainTimer.C:
				cancelBase()
				shutdownErr = <-shutdownDone
			}
			if shutdownErr != nil {
				slog.ErrorContext(ctx, "Server forced to shutdown", "error", shutdownErr)
			}

			if grpcServer != nil {
				grpcStopped := make(chan struct{})
				go func() {
					grpcServer.GracefulStop()
					close(grpcStopped)
				}()
				grpcTimer := time.NewTimer(2 * time.Second)
				select {
				case <-grpcStopped:
					grpcTimer.Stop()
				case <-grpcTimer.C:
					grpcServer.Stop()
					<-grpcStopped
				}
			}
			if shutdownErr == nil {
				slog.InfoContext(ctx, "Server stopped gracefully")
			}
			return shutdownErr
		},
	})

	return srv, nil
}

func prepareServerTLS(ctx context.Context, cfg *config.Config) (bool, string, string, *edge.Config, error) {
	useTLS := cfg.TLSEnabled
	tlsCertFile := strings.TrimSpace(cfg.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(cfg.TLSKeyFile)
	edgeCfg := buildEdgeRuntimeConfig(cfg)
	if useTLS && (tlsCertFile == "" || tlsKeyFile == "") {
		return false, "", "", nil, errors.New("TLS_ENABLED requires both TLS_CERT_FILE and TLS_KEY_FILE")
	}

	if cfg.AgentMode {
		return useTLS, tlsCertFile, tlsKeyFile, edgeCfg, nil
	}

	if err := edge.PrepareManagerMTLSAssetsWithContext(ctx, edgeCfg); err != nil {
		return false, "", "", nil, err
	}

	if edge.NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) != edge.EdgeMTLSModeDisabled {
		if err := edge.ValidateManagerMTLSConfig(edgeCfg); err != nil {
			return false, "", "", nil, err
		}
	}

	return useTLS, tlsCertFile, tlsKeyFile, edgeCfg, nil
}

func configureHTTPProtocols(useTLS bool, handler http.Handler) (http.Handler, *http.Protocols) {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	if useTLS {
		protocols.SetHTTP2(true)
		return handler, &protocols
	}

	protocols.SetUnencryptedHTTP2(true)
	inner := handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// go1.26.6 arms ReadHeaderTimeout before the h2c handoff and never clears it, so every h2c
		// connection (and its gRPC tunnel stream) would die ReadHeaderTimeout after accept.
		if r.ProtoMajor == 2 && r.TLS == nil {
			httpx.ClearReadDeadline(r.Context())
		}
		inner.ServeHTTP(w, r)
	})
	return handler, &protocols
}

func buildEdgeRuntimeConfig(cfg *config.Config) *edge.Config {
	return &edge.Config{
		EdgeAgent:             cfg.EdgeAgent,
		EdgeTransport:         cfg.EdgeTransport,
		EdgeReconnectInterval: cfg.EdgeReconnectInterval,
		EdgeMTLSMode:          cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:        cfg.EdgeMTLSCAFile,
		EdgeMTLSCertFile:      cfg.EdgeMTLSCertFile,
		EdgeMTLSKeyFile:       cfg.EdgeMTLSKeyFile,
		EdgeMTLSServerName:    cfg.EdgeMTLSServerName,
		EdgeMTLSAssetsDir:     cfg.EdgeMTLSAssetsDir,
		AppURL:                cfg.GetAppURL(),
		ManagerApiUrl:         cfg.ManagerApiUrl,
		AgentToken:            cfg.AgentToken,
		Port:                  cfg.Port,
		Listen:                cfg.Listen,
		EdgeMuxDisabled:       cfg.EdgeMuxDisabled,
	}
}

func normalizeTunnelGRPCRequestPath(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	if r.URL == nil {
		return r
	}

	connectMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	const tunnelConnectPath = "/api/tunnel/connect"
	if strings.HasSuffix(r.URL.Path, tunnelConnectPath) {
		clone := r.Clone(r.Context())
		clone.URL.Path = connectMethodPath
		clone.RequestURI = connectMethodPath
		return clone
	}

	idx := strings.Index(r.URL.Path, connectMethodPath)
	if idx <= 0 {
		return r
	}

	normalizedPath := r.URL.Path[idx:]
	if normalizedPath == r.URL.Path {
		return r
	}

	clone := r.Clone(r.Context())
	clone.URL.Path = normalizedPath
	clone.RequestURI = normalizedPath
	return clone
}

func isTunnelGRPCRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}

	if r.Method != http.MethodPost {
		return false
	}

	path := r.URL.Path
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName
	if path == fullMethodPath || strings.HasSuffix(path, fullMethodPath) {
		return true
	}

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	return strings.HasPrefix(contentType, "application/grpc")
}
