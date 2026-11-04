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

	"github.com/labstack/echo/v5"
	"github.com/moby/moby/client"
	"github.com/pires/go-proxyproto"
	"go.uber.org/fx"
	"google.golang.org/grpc"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/httpx"
	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	HTTPServerParams struct {
		fx.In

		AppCtx       context.Context
		Config       *config.Config
		Router       *echo.Echo
		TunnelServer *agent.TunnelServer
	}
)

var serverOptions = fx.Options(
	fx.Provide(
		newRouter,
		newTunnelServer,
		NewHTTPServer,
	),
)

// NewHTTPServer builds the shared HTTP/gRPC server and registers its lifecycle.
func NewHTTPServer(lc fx.Lifecycle, p HTTPServerParams) (*http.Server, error) {
	if p.Config.AgentEnabled {
		return &http.Server{Handler: p.Router}, nil
	}
	listenAddr := p.Config.ListenAddr()
	useTLS, tlsCertFile, tlsKeyFile, agentCfg, err := prepareServerTLS(p.AppCtx, p.Config)
	if err != nil {
		return nil, err
	}
	p.TunnelServer.Config = agentCfg

	// Managers serve the gRPC agent tunnel on the shared listener.
	//nolint:contextcheck // Stream interceptors receive request-scoped context from grpc.ServerStream.
	grpcServer := grpc.NewServer(p.TunnelServer.GRPCServerOptions()...)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, p.TunnelServer)
	slog.InfoContext(p.AppCtx, "Using shared HTTP/gRPC listener for agent tunnel", "addr", listenAddr)
	httpHandler, protocols := configureHTTPProtocols(useTLS, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isTunnelGRPCRequest(r) {
			grpcServer.ServeHTTP(w, normalizeTunnelGRPCRequestPath(withClientRemoteAddr(r, p.Router.IPExtractor)))
			return
		}
		p.Router.ServeHTTP(w, r)
	}))

	var tlsConfig *tls.Config
	if useTLS {
		certificate, loadErr := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
		if loadErr != nil {
			return nil, loadErr
		}
		agentTLSConfig, tlsErr := agent.BuildManagerServerTLSConfig(agentCfg)
		if tlsErr != nil {
			return nil, tlsErr
		}
		tlsConfig = cmp.Or(agentTLSConfig, &tls.Config{})
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
		// Agent gRPC tunnels are one long HTTP/2 stream, so the 1 MiB default window would cap agent throughput per RTT.
		HTTP2: &http.HTTP2Config{
			MaxReceiveBufferPerStream:     8 << 20,
			MaxReceiveBufferPerConnection: 16 << 20,
			SendPingTimeout:               30 * time.Second,
			PingTimeout:                   10 * time.Second,
		},
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
					serveErr = srv.ServeTLS(proxyProtocolListener(p.AppCtx, listener, p.Config.TrustedProxies), "", "")
				} else {
					serveErr = srv.Serve(proxyProtocolListener(p.AppCtx, listener, p.Config.TrustedProxies))
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
			if shutdownErr == nil {
				slog.InfoContext(ctx, "Server stopped gracefully")
			}
			return shutdownErr
		},
	})

	return srv, nil
}

func prepareServerTLS(ctx context.Context, cfg *config.Config) (bool, string, string, *agent.Config, error) {
	useTLS := cfg.TLSEnabled
	tlsCertFile := strings.TrimSpace(cfg.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(cfg.TLSKeyFile)
	agentCfg := buildAgentRuntimeConfig(cfg, "", agent.AgentHostInfo{})
	if useTLS && (tlsCertFile == "" || tlsKeyFile == "") {
		return false, "", "", nil, errors.New("ARCANE_TLS_ENABLED requires both ARCANE_TLS_CERT_FILE and ARCANE_TLS_KEY_FILE")
	}

	if err := agent.PrepareManagerMTLSAssetsWithContext(ctx, agentCfg); err != nil {
		return false, "", "", nil, err
	}

	if agent.NormalizeMTLSMode(cfg.AgentMTLSMode) != agent.MTLSModeDisabled {
		if err := agent.ValidateManagerMTLSConfig(agentCfg); err != nil {
			return false, "", "", nil, err
		}
	}

	return useTLS, tlsCertFile, tlsKeyFile, agentCfg, nil
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

func buildAgentRuntimeConfig(cfg *config.Config, agentToken string, hostInfo agent.AgentHostInfo) *agent.Config {
	return &agent.Config{
		AgentEnabled:           cfg.AgentEnabled,
		AgentTransport:         cfg.AgentTransport,
		AgentReconnectInterval: cfg.AgentReconnectInterval,
		AgentMTLSMode:          cfg.AgentMTLSMode,
		AgentMTLSCAFile:        cfg.AgentMTLSCAFile,
		AgentMTLSCertFile:      cfg.AgentMTLSCertFile,
		AgentMTLSKeyFile:       cfg.AgentMTLSKeyFile,
		AgentMTLSServerName:    cfg.AgentMTLSServerName,
		AgentMTLSAssetsDir:     cfg.AgentMTLSAssetsDir,
		AppURL:                 cfg.GetAppURL(),
		ManagerApiUrl:          cfg.ManagerApiUrl,
		AgentToken:             agentToken,
		Port:                   cfg.Port,
		Listen:                 cfg.Listen,
		HostInfo:               hostInfo,
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

func proxyProtocolListener(ctx context.Context, listener net.Listener, trustedProxies string) net.Listener {
	ranges := strings.Fields(strings.ReplaceAll(trustedProxies, ",", " "))
	if len(ranges) == 0 {
		return listener
	}
	policy, err := proxyproto.PolicyFromRanges(ranges, proxyproto.USE, proxyproto.SKIP)
	if err != nil {
		slog.WarnContext(ctx, "Invalid ARCANE_TRUSTED_PROXIES; PROXY protocol is disabled", "error", err)
		return listener
	}
	return &proxyproto.Listener{
		Listener:   listener,
		ConnPolicy: policy,
	}
}

func withClientRemoteAddr(r *http.Request, extractIP echo.IPExtractor) *http.Request {
	_, port, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r
	}
	clone := r.Clone(r.Context())
	clone.RemoteAddr = net.JoinHostPort(extractIP(r), port)
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

func agentHostInfo(ctx context.Context, dockerService *docker.DockerClientService) agent.AgentHostInfo {
	infoCtx, cancel := context.WithTimeout(ctx, startupTaskTimeout)
	defer cancel()
	dockerClient, err := dockerService.GetClient(infoCtx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to connect to Docker for agent host info", "error", err)
		return agent.AgentHostInfo{AgentVersion: config.Version}
	}
	result, err := dockerClient.Info(infoCtx, client.InfoOptions{})
	if err != nil {
		slog.WarnContext(ctx, "Failed to read Docker info for agent host info", "error", err)
		return agent.AgentHostInfo{AgentVersion: config.Version}
	}
	return agent.AgentHostInfo{
		Hostname:      result.Info.Name,
		OS:            result.Info.OperatingSystem,
		KernelVersion: result.Info.KernelVersion,
		Architecture:  result.Info.Architecture,
		DockerVersion: result.Info.ServerVersion,
		AgentVersion:  config.Version,
		MemoryBytes:   result.Info.MemTotal,
		CPUs:          int32(result.Info.NCPU),
	}
}
