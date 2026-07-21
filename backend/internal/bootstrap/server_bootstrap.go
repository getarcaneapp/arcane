package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx"
	"google.golang.org/grpc"
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
}

// NewHTTPServer builds the shared HTTP/gRPC server and registers its lifecycle.
func NewHTTPServer(lc fx.Lifecycle, p HTTPServerParams) (*http.Server, error) {
	listenAddr := p.Config.ListenAddr()
	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLSInternal(p.AppCtx, p.Config)
	if err != nil {
		return nil, err
	}
	if p.TunnelServer != nil {
		p.TunnelServer.SetConfig(edgeCfg)
	}

	httpHandler, grpcServer := configureTunnelServerInternal(p.AppCtx, p.Config, p.Router, p.TunnelServer, listenAddr)
	httpHandler, protocols := configureHTTPProtocolsInternal(useTLS, httpHandler)

	// Request contexts deliberately do not inherit the app lifecycle marker.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	srv, err := newHTTPServerInternal(baseCtx, listenAddr, httpHandler, protocols, useTLS, edgeCfg)
	if err != nil {
		cancelBase()
		return nil, err
	}
	if useTLS {
		certificate, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
		if err != nil {
			cancelBase()
			return nil, err
		}
		if srv.TLSConfig == nil {
			srv.TLSConfig = &tls.Config{}
		} else {
			srv.TLSConfig = srv.TLSConfig.Clone()
		}
		srv.TLSConfig.Certificates = []tls.Certificate{certificate}
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			listener, err := new(net.ListenConfig).Listen(ctx, "tcp", listenAddr)
			if err != nil {
				cancelBase()
				return err
			}
			srv.Addr = listener.Addr().String()

			slog.InfoContext(ctx, "Starting HTTP server", "addr", listenAddr, "listen", p.Config.Listen, "port", p.Config.Port, "tls_enabled", useTLS)
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
			// Shutdown does not cancel active request contexts, so unblock streaming
			// handlers before waiting for the server to drain.
			cancelBase()
			shutdownErr := srv.Shutdown(ctx)
			if shutdownErr != nil {
				slog.ErrorContext(ctx, "Server forced to shutdown", "error", shutdownErr)
			}

			if grpcServer != nil {
				grpcServer.GracefulStop()
			}
			if p.TunnelServer != nil {
				p.TunnelServer.WaitForCleanupDone()
			}
			if shutdownErr == nil {
				slog.InfoContext(ctx, "Server stopped gracefully")
			}
			return shutdownErr
		},
	})

	return srv, nil
}

func prepareServerTLSInternal(ctx context.Context, cfg *config.Config) (bool, string, string, *edge.Config, error) {
	useTLS := cfg.TLSEnabled
	tlsCertFile := strings.TrimSpace(cfg.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(cfg.TLSKeyFile)
	edgeCfg := buildEdgeRuntimeConfigInternal(cfg)
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

func configureTunnelServerInternal(appCtx context.Context, cfg *config.Config, router http.Handler, tunnelServer *edge.TunnelServer, listenAddr string) (http.Handler, *grpc.Server) {
	httpHandler := router
	var grpcServer *grpc.Server

	if !cfg.AgentMode && tunnelServer != nil {
		grpcServer = grpc.NewServer(tunnelServer.GRPCServerOptions(appCtx)...)
		tunnelpb.RegisterTunnelServiceServer(grpcServer, tunnelServer)

		httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isTunnelGRPCRequestInternal(r) {
				grpcReq := normalizeTunnelGRPCRequestPathInternal(r)
				grpcServer.ServeHTTP(w, grpcReq)
				return
			}
			router.ServeHTTP(w, r)
		})
		slog.InfoContext(appCtx, "Using shared HTTP/gRPC listener for edge tunnel", "addr", listenAddr)
	}

	return httpHandler, grpcServer
}

func configureHTTPProtocolsInternal(useTLS bool, handler http.Handler) (http.Handler, *http.Protocols) {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	if useTLS {
		protocols.SetHTTP2(true)
		return handler, &protocols
	}

	protocols.SetUnencryptedHTTP2(true)
	return handler, &protocols
}

func newHTTPServerInternal(baseCtx context.Context, listenAddr string, handler http.Handler, protocols *http.Protocols, useTLS bool, edgeCfg *edge.Config) (*http.Server, error) {
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	if !useTLS {
		return srv, nil
	}

	tlsConfig, err := edge.BuildManagerServerTLSConfig(edgeCfg)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		srv.TLSConfig = tlsConfig
	}
	return srv, nil
}

func buildEdgeRuntimeConfigInternal(cfg *config.Config) *edge.Config {
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
	}
}

func normalizeTunnelGRPCRequestPathInternal(r *http.Request) *http.Request {
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
		cloneURL := *clone.URL
		cloneURL.Path = connectMethodPath
		clone.URL = &cloneURL
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
	cloneURL := *clone.URL
	cloneURL.Path = normalizedPath
	clone.URL = &cloneURL
	clone.RequestURI = normalizedPath
	return clone
}

func isTunnelGRPCRequestInternal(r *http.Request) bool {
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
