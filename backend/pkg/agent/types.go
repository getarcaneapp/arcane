package agent

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"

	"kit.getarcane.app/concurrency"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	TunnelClient struct {
		handler                http.Handler
		cfg                    *Config
		streamWindowMiB        func() int
		errCh                  chan error
		conn                   atomic.Pointer[connBox]
		preference             atomic.Pointer[transportPreference]
		sessionID              concurrency.Snapshot[string]
		managerURL             string
		managerGRPCAddr        string
		agentInstanceID        string
		reconnectInterval      time.Duration
		registrationTimeout    time.Duration
		websocketPreferenceTTL time.Duration
		healthySessionDuration time.Duration
	}

	transportPreference struct {
		webSocketUntil time.Time
		grpcFailures   int
	}

	connBox struct {
		conn TunnelConnection
	}

	tunnelTransports struct {
		grpc      bool
		websocket bool
	}

	commandRoute struct {
		Method      string
		PathPattern string
		CommandName string
		Stream      bool
	}

	commandRouteKey struct {
		Method string
		Stream bool
	}

	commandRouteNode struct {
		static map[string]*commandRouteNode
		param  *commandRouteNode
		route  *commandRoute
	}

	commandRouteIndex map[commandRouteKey]*commandRouteNode

	AgentTunnel struct {
		ConnectedAt   time.Time
		LastHeartbeat time.Time
		Conn          TunnelConnection
		HTTPClient    *http.Client
		done          chan struct{}

		mux           *muxSession
		Host          *AgentHostInfo
		EnvironmentID string
		SessionID     string
		AgentInstance string
		SecurityMode  string
		State         string
		DisconnectErr string
		RemoteAddr    string
		Capabilities  []string
		mu            sync.RWMutex
		closeOnce     sync.Once
	}

	TunnelRegistry struct {
		tunnels    *concurrency.StateMap[string, *AgentTunnel]
		history    *concurrency.StateMap[string, TunnelHistory]
		registered *concurrency.Signal[string]
		stopped    atomic.Bool
	}

	TunnelHistory struct {
		LastDisconnectAt     time.Time
		LastDisconnectReason string
		Sessions             int
	}

	TunnelServer struct {
		MTLS *MTLS

		registry        *TunnelRegistry
		resolver        EnvironmentResolver
		statusCallback  StatusUpdateCallback
		eventCallback   EventCallback
		streamWindowMiB func() int
		statusMu        sync.Mutex
	}

	TunnelServerOptions struct {
		Registry        *TunnelRegistry
		Resolver        EnvironmentResolver
		StatusCallback  StatusUpdateCallback
		MTLS            *MTLS
		EventCallback   EventCallback
		StreamWindowMiB func() int
	}

	streamIdentityKey struct{}

	streamIdentity struct {
		environmentID string
		securityMode  string
	}

	contextualServerStream struct {
		grpc.ServerStream

		ctx context.Context
	}

	GeneratedMTLSFile struct {
		Name          string `json:"name"`
		Content       string `json:"content"`
		ContainerPath string `json:"containerPath"`
		Permissions   string `json:"permissions"`
	}

	GeneratedMTLSAssets struct {
		HostDirHint string              `json:"hostDirHint"`
		Files       []GeneratedMTLSFile `json:"files"`
		CertIssued  bool                `json:"-"`
		CAGenerated bool                `json:"-"`
	}

	EnrollMTLSResponse struct {
		Files []GeneratedMTLSFile `json:"files"`
	}

	MTLSEnrollment struct {
		Assets     *GeneratedMTLSAssets
		Cached     bool
		Reenrolled bool
	}

	Config struct {
		MTLS                   *MTLS
		AgentTransport         string
		AgentToken             string
		HostInfo               AgentHostInfo
		AgentReconnectInterval int
		AgentEnabled           bool
	}

	GRPCTunnelHandlerConfig struct {
		GRPC      http.Handler
		Next      http.Handler
		ExtractIP func(*http.Request) string
	}

	TunnelClientConfig struct {
		Config          *Config
		Handler         http.Handler
		Errors          chan error
		StreamWindowMiB func() int
	}

	MTLSConfig struct {
		Mode       string
		CAFile     string
		CertFile   string
		KeyFile    string
		ServerName string
		AssetsDir  string
		AppURL     string
		ManagerURL string
	}

	MTLS struct {
		MTLSConfig
	}

	IssueClientOptions struct {
		EnvironmentID   string
		EnvironmentName string
		FallbackAppURL  string
	}

	ManagerClientOptions struct {
		AgentToken string
		Timeout    time.Duration
		Enroll     bool
	}

	AgentHostInfo struct {
		Hostname      string `json:"hostname,omitempty"`
		OS            string `json:"os,omitempty"`
		KernelVersion string `json:"kernel_version,omitempty"`
		Architecture  string `json:"architecture,omitempty"`
		DockerVersion string `json:"docker_version,omitempty"`
		AgentVersion  string `json:"agent_version,omitempty"`
		MemoryBytes   int64  `json:"memory_bytes,omitempty"`
		CPUs          int32  `json:"cpus,omitempty"`
	}

	TunnelRuntimeState struct {
		RoundTripTime *time.Duration
		RTTMin        *time.Duration
		RTTAvg        *time.Duration
		RTTMax        *time.Duration
		ConnectedAt   *time.Time
		LastHeartbeat *time.Time
		Host          *AgentHostInfo
		Transport     string
		SessionID     string
		AgentInstance string
		SecurityMode  string
		State         string
		RemoteAddr    string
		Capabilities  []string
		OpenStreams   int
		BytesIn       int64
		BytesOut      int64
		StreamsOpened int64
	}

	TunnelMessage struct {
		ID            string            `json:"id"`
		Type          TunnelMessageType `json:"type"`
		AgentToken    string            `json:"agent_token,omitempty"`
		EnvironmentID string            `json:"environment_id,omitempty"`
		Error         string            `json:"error,omitempty"`
		SessionID     string            `json:"session_id,omitempty"`
		ResumeSession string            `json:"resume_session,omitempty"`
		AgentInstance string            `json:"agent_instance,omitempty"`
		SecurityMode  string            `json:"security_mode,omitempty"`
		Host          *AgentHostInfo    `json:"host,omitempty"`
		Capabilities  []string          `json:"capabilities,omitempty"`
		Accepted      bool              `json:"accepted,omitempty"`
		DrainPrevious bool              `json:"drain_previous,omitempty"`
	}

	TunnelEvent struct {
		Type         string `json:"type"`
		Severity     string `json:"severity,omitempty"`
		Title        string `json:"title"`
		Description  string `json:"description,omitempty"`
		ResourceType string `json:"resource_type,omitempty"`
		ResourceID   string `json:"resource_id,omitempty"`
		ResourceName string `json:"resource_name,omitempty"`
		UserID       string `json:"user_id,omitempty"`
		Username     string `json:"username,omitempty"`
		MetadataJSON []byte `json:"metadata_json,omitempty"`
	}

	WebSocketTunnelConn struct {
		conn   *websocket.Conn
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	GRPCManagerTunnelConn struct {
		stream grpcManagerStream
		cancel context.CancelFunc
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	GRPCAgentTunnelConn struct {
		stream grpcAgentStream
		cancel context.CancelFunc
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	muxSession struct {
		ready   chan struct{}
		closed  chan struct{}
		session atomic.Pointer[yamux.Session]
		conn    atomic.Pointer[muxConn]
		// rtt is the last keepalive round trip in nanoseconds, zero until the first ping.
		rtt       atomic.Int64
		rttMin    atomic.Int64
		rttMax    atomic.Int64
		rttSum    atomic.Int64
		rttCount  atomic.Int64
		opened    atomic.Int64
		closeOnce sync.Once
		mu        sync.Mutex
	}

	muxConn struct {
		err            error
		recv           func() ([]byte, error)
		closeTransport func() error
		cond           sync.Cond
		unread         []byte

		pending   []byte
		closeOnce sync.Once

		pendingLimit atomic.Int64
		rx           atomic.Int64
		tx           atomic.Int64
		sent         atomic.Int64
		mu           sync.Mutex
	}

	muxStream struct {
		net.Conn

		conn *muxConn
	}

	muxListener struct {
		*yamux.Session

		conn *muxConn
	}

	cancelableGRPCManagerStream struct {
		stream   grpcManagerStream
		ctx      context.Context
		recvCh   chan grpcRecvResult
		recvOnce sync.Once
	}

	grpcRecvResult struct {
		msg *tunnelpb.AgentMessage
		err error
	}
)
