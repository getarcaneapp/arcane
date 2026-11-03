package agent

import (
	"context"
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
	// TunnelClient represents the agent-side tunnel client
	TunnelClient struct {
		preferWebSocketUntil   time.Time
		handler                http.Handler
		cfg                    *Config
		conn                   atomic.Pointer[connBox]
		sessionID              concurrency.Snapshot[string]
		managerURL             string
		managerGRPCAddr        string
		agentInstanceID        string
		reconnectInterval      time.Duration
		registrationTimeout    time.Duration
		websocketPreferenceTTL time.Duration
		healthySessionDuration time.Duration
		grpcFailureStreak      int
		transportPreferenceMu  sync.RWMutex
	}

	// connBox lets the active connection swap atomically on reconnect: an atomic.Value would panic when the
	// concrete type changes between gRPC and WebSocket.
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

	// AgentTunnel represents an active tunnel connection from an agent
	AgentTunnel struct {
		ConnectedAt   time.Time
		LastHeartbeat time.Time
		Conn          TunnelConnection
		// HTTPClient sends requests to the agent's router over the multiplexed session; use MuxAgentBaseURL as the origin.
		HTTPClient *http.Client
		done       chan struct{}

		mux           *muxSession
		Host          *AgentHostInfo
		EnvironmentID string
		SessionID     string
		AgentInstance string
		Transport     string
		SecurityMode  string
		State         string
		DisconnectErr string
		RemoteAddr    string
		Capabilities  []string
		mu            sync.RWMutex
		closeOnce     sync.Once
	}

	// TunnelRegistry manages active agent tunnel connections
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

	// TunnelServer handles incoming agent connections on the manager side.
	TunnelServer struct {
		// Config holds the agent runtime settings; set it before serving.
		Config *Config
		// NameResolver, when set, names environments in generated mTLS assets.
		NameResolver EnvironmentNameResolver
		// EventCallback, when set, receives agent events.
		EventCallback EventCallback
		// EnrollmentCallback, when set, runs after a successful agent mTLS enrollment.
		EnrollmentCallback EnrollmentCallback

		registry        *TunnelRegistry
		resolver        EnvironmentResolver
		statusCallback  StatusUpdateCallback
		cleanupDone     chan struct{}
		cleanupDoneOnce sync.Once
		statusMu        sync.Mutex
	}

	// TunnelServerOptions configures NewTunnelServer.
	TunnelServerOptions struct {
		Registry           *TunnelRegistry
		Resolver           EnvironmentResolver
		StatusCallback     StatusUpdateCallback
		Config             *Config
		NameResolver       EnvironmentNameResolver
		EventCallback      EventCallback
		EnrollmentCallback EnrollmentCallback
	}

	streamIdentityKey struct{}

	// streamIdentity is what the gRPC auth interceptor verified for a tunnel stream.
	streamIdentity struct {
		environmentID string
		securityMode  string
	}

	contextualServerStream struct {
		grpc.ServerStream

		ctx context.Context
	}

	// GeneratedMTLSFile describes a generated file that should be copied to the agent host.
	GeneratedMTLSFile struct {
		Name          string `json:"name"`
		Content       string `json:"content"`
		ContainerPath string `json:"containerPath"`
		Permissions   string `json:"permissions"`
	}

	// GeneratedMTLSAssets contains manager-generated agent client certificates and snippet metadata.
	GeneratedMTLSAssets struct {
		HostDirHint string              `json:"hostDirHint"`
		Files       []GeneratedMTLSFile `json:"files"`
		CertIssued  bool                `json:"-"`
		CAGenerated bool                `json:"-"`
		Reenrolled  bool                `json:"-"`
	}

	enrollMTLSResponse struct {
		Files []GeneratedMTLSFile `json:"files"`
	}

	// Config contains the public agent-tunnel runtime settings needed by pkg/agent.
	Config struct {
		AgentTransport         string
		AgentMTLSMode          string
		AgentMTLSCAFile        string
		AgentMTLSCertFile      string
		AgentMTLSKeyFile       string
		AgentMTLSServerName    string
		AgentMTLSAssetsDir     string
		AppURL                 string
		ManagerApiUrl          string
		AgentToken             string
		Port                   string
		Listen                 string
		HostInfo               AgentHostInfo
		AgentReconnectInterval int
		AgentEnabled           bool
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

	// TunnelRuntimeState describes the live, in-memory state of an active agent tunnel.
	TunnelRuntimeState struct {
		// RoundTripTime is the last keepalive round trip.
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
		// OpenStreams counts open mux streams, including pooled idle ones.
		OpenStreams   int
		BytesIn       int64
		BytesOut      int64
		StreamsOpened int64
	}

	// TunnelMessage is the register handshake both peers exchange before switching to the multiplexed data plane.
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

	// TunnelEvent is an event payload sent from an agent to the manager.
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

	// TunnelConn wraps a WebSocket connection with send/receive helpers.
	TunnelConn struct {
		conn   *websocket.Conn
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	// GRPCManagerTunnelConn wraps the manager-side gRPC tunnel stream.
	GRPCManagerTunnelConn struct {
		stream grpcManagerStream
		cancel context.CancelFunc
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	// GRPCAgentTunnelConn wraps the agent-side gRPC tunnel stream.
	GRPCAgentTunnelConn struct {
		stream grpcAgentStream
		cancel context.CancelFunc
		mu     sync.Mutex
		closed atomic.Bool
		muxed  atomic.Bool
	}

	// muxSession is the manager end of a multiplexed data plane. Dials wait until ready closes.
	muxSession struct {
		ready   chan struct{}
		closed  chan struct{}
		session *yamux.Session
		conn    *muxConn
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

	// muxConn adapts a message transport to the byte stream yamux runs on.
	muxConn struct {
		err            error
		recv           func() ([]byte, error)
		closeTransport func() error
		cond           sync.Cond
		unread         []byte

		pending   []byte
		closeOnce sync.Once

		rx atomic.Int64
		tx atomic.Int64
		mu sync.Mutex
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
