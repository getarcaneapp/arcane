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
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/httpx"
	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	HTTPServerParams struct {
		fx.In

		AppCtx          context.Context
		Config          *config.Config
		Router          *echo.Echo
		TunnelServer    *agent.TunnelServer
		SettingsService *settings.SettingsService
	}
)

var serverOptions = fx.Options(
	fx.Provide(
		func(cfg *config.Config) *agent.MTLS {
			return buildAgentRuntimeConfig(cfg, "", agent.AgentHostInfo{}).MTLS
		},
		newRouter,
		newTunnelServer,
		NewHTTPServer,
	),
)

func NewHTTPServer(lc fx.Lifecycle, p HTTPServerParams) (*http.Server, error) {
	if p.Config.AgentEnabled {
		return &http.Server{Handler: p.Router}, nil
	}
	listenAddr := p.Config.ListenAddr()
	useTLS, tlsCertFile, tlsKeyFile, agentTLSConfig, err := prepareServerTLS(p.AppCtx, p.Config, p.TunnelServer.MTLS)
	if err != nil {
		return nil, err
	}

	// Managers serve the gRPC agent tunnel on the shared listener.
	//nolint:contextcheck // Stream interceptors receive request-scoped context from grpc.ServerStream.
	grpcServer := grpc.NewServer(p.TunnelServer.GRPCServerOptions()...)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, p.TunnelServer)
	slog.InfoContext(p.AppCtx, "Using shared HTTP/gRPC listener for agent tunnel", "addr", listenAddr)
	tunnelHandler := agent.GRPCTunnelHandler(agent.GRPCTunnelHandlerConfig{
		GRPC:      grpcServer,
		Next:      p.Router,
		ExtractIP: p.Router.IPExtractor,
	})
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(useTLS)
	protocols.SetUnencryptedHTTP2(!useTLS)
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// go1.26.6 never clears ReadHeaderTimeout after the h2c handoff.
		if r.ProtoMajor == 2 && r.TLS == nil {
			httpx.ClearReadDeadline(r.Context())
		}
		tunnelHandler.ServeHTTP(w, r)
	})

	trustedProxyRanges := strings.Fields(strings.ReplaceAll(p.Config.TrustedProxies, ",", " "))
	proxyPolicy, proxyPolicyErr := proxyproto.PolicyFromRanges(trustedProxyRanges, proxyproto.USE, proxyproto.SKIP)
	if len(trustedProxyRanges) > 0 && proxyPolicyErr != nil {
		slog.WarnContext(p.AppCtx, "Invalid ARCANE_TRUSTED_PROXIES; PROXY protocol is disabled", "error", proxyPolicyErr)
	}

	var tlsConfig *tls.Config
	if useTLS {
		certificate, loadErr := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
		if loadErr != nil {
			return nil, loadErr
		}
		tlsConfig = cmp.Or(agentTLSConfig, &tls.Config{})
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	http2Window := p.SettingsService.GetSettingsOrDefaults(p.AppCtx).HTTP2ReceiveWindowMb.AsInt() << 20
	// Request contexts deliberately do not inherit the app lifecycle marker.
	baseCtx, cancelBase := context.WithCancel(context.Background()) //nolint:forbidigo // HTTP requests need a root without the application lifecycle marker.
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httpHandler,
		Protocols:         &protocols,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		// Long-lived stream handlers tune dead-peer detection per connection; see handlerutil.SetDeadPeerTimeout.
		ConnContext: httpx.WithConn,
		HTTP2: &http.HTTP2Config{
			MaxReceiveBufferPerStream:     http2Window,
			MaxReceiveBufferPerConnection: 2 * http2Window,
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
			if len(trustedProxyRanges) > 0 && proxyPolicyErr == nil {
				listener = &proxyproto.Listener{
					Listener:   listener,
					ConnPolicy: proxyPolicy,
				}
			}

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

func prepareServerTLS(ctx context.Context, cfg *config.Config, mtls *agent.MTLS) (bool, string, string, *tls.Config, error) {
	useTLS := cfg.TLSEnabled
	tlsCertFile := strings.TrimSpace(cfg.TLSCertFile)
	tlsKeyFile := strings.TrimSpace(cfg.TLSKeyFile)
	if useTLS && (tlsCertFile == "" || tlsKeyFile == "") {
		return false, "", "", nil, errors.New("ARCANE_TLS_ENABLED requires both ARCANE_TLS_CERT_FILE and ARCANE_TLS_KEY_FILE")
	}

	agentTLSConfig, err := mtls.Server(ctx)
	if err != nil {
		return false, "", "", nil, err
	}
	return useTLS, tlsCertFile, tlsKeyFile, agentTLSConfig, nil
}

func buildAgentRuntimeConfig(cfg *config.Config, agentToken string, hostInfo agent.AgentHostInfo) *agent.Config {
	return &agent.Config{
		MTLS: agent.NewMTLS(agent.MTLSConfig{
			Mode:       cfg.AgentMTLSMode,
			CAFile:     cfg.AgentMTLSCAFile,
			CertFile:   cfg.AgentMTLSCertFile,
			KeyFile:    cfg.AgentMTLSKeyFile,
			ServerName: cfg.AgentMTLSServerName,
			AssetsDir:  cfg.AgentMTLSAssetsDir,
			AppURL:     cfg.GetAppURL(),
			ManagerURL: cfg.ManagerApiUrl,
		}),
		AgentEnabled:           cfg.AgentEnabled,
		AgentTransport:         cfg.AgentTransport,
		AgentReconnectInterval: cfg.AgentReconnectInterval,
		AgentToken:             agentToken,
		HostInfo:               hostInfo,
	}
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
