package edge

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

// TunnelClient represents the agent-side tunnel client
type TunnelClient struct {
	cfg                    *Config
	handler                http.Handler
	reconnectInterval      time.Duration
	registrationTimeout    time.Duration
	websocketPreferenceTTL time.Duration
	healthySessionDuration time.Duration
	managerURL             string
	managerGRPCAddr        string
	conn                   atomic.Pointer[connBox]
	transportPreferenceMu  sync.RWMutex
	preferWebSocketUntil   time.Time
	grpcFailureStreak      int
	agentInstanceID        string
	sessionID              concurrency.Snapshot[string]
}

// connBox lets the active connection swap atomically on reconnect: an atomic.Value would panic when the
// concrete type changes between gRPC and WebSocket.
type connBox struct {
	conn TunnelConnection
}

type tunnelTransports struct {
	grpc      bool
	websocket bool
}

type pollManagedTunnelSession struct {
	cancel   context.CancelFunc
	done     chan error
	stopping bool
}

type commandRoute struct {
	Method      string
	PathPattern string
	CommandName string
	Stream      bool
}

type commandRouteKey struct {
	Method string
	Stream bool
}

type commandRouteNode struct {
	static map[string]*commandRouteNode
	param  *commandRouteNode
	route  *commandRoute
}

type commandRouteIndex map[commandRouteKey]*commandRouteNode

// TunnelPollRequest is a forward-compatible control-plane check-in request.
type TunnelPollRequest struct {
	Transport string `json:"transport,omitempty"`
	Connected bool   `json:"connected,omitempty"`
}

// TunnelPollResponse is a forward-compatible control-plane response.
type TunnelPollResponse struct {
	Status              string `json:"status"`
	PollIntervalSeconds int    `json:"pollIntervalSeconds"`
	ActiveTransport     string `json:"activeTransport,omitempty"`
	Connected           bool   `json:"connected,omitempty"`
}

// PollRuntimeState describes the most recent poll-based control-plane activity
// observed for an edge environment.
type PollRuntimeState struct {
	LastPollAt          *time.Time
	PollIntervalSeconds int
}

// TunnelDemandRegistry tracks short-lived tunnel demand on the manager side.
type TunnelDemandRegistry struct {
	demands map[string]time.Time
	mu      sync.RWMutex
}

// PollRuntimeRegistry tracks recent poll check-ins from edge agents.
type PollRuntimeRegistry struct {
	states map[string]PollRuntimeState
	mu     sync.RWMutex
}

// AgentTunnel represents an active tunnel connection from an edge agent
type AgentTunnel struct {
	EnvironmentID string
	Conn          TunnelConnection
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	SessionID     string
	AgentInstance string
	Transport     string
	SecurityMode  string
	Capabilities  []string
	State         string
	DisconnectErr string
	mu            sync.RWMutex
	done          chan struct{}
	closeOnce     sync.Once

	mux *muxSession
}

// TunnelRegistry manages active edge agent tunnel connections
type TunnelRegistry struct {
	stopped atomic.Bool
	tunnels *concurrency.StateMap[string, *AgentTunnel]
}

// TunnelServer handles incoming edge agent connections on the manager side.
type TunnelServer struct {
	// Config holds the edge runtime settings; set it before serving.
	Config *Config
	// NameResolver, when set, names environments in generated mTLS assets.
	NameResolver EnvironmentNameResolver
	// EventCallback, when set, receives agent events.
	EventCallback EventCallback
	// EnrollmentCallback, when set, runs after a successful edge mTLS enrollment.
	EnrollmentCallback EnrollmentCallback

	registry        *TunnelRegistry
	resolver        EnvironmentResolver
	statusCallback  StatusUpdateCallback
	cleanupDone     chan struct{}
	cleanupDoneOnce sync.Once
	statusMu        sync.Mutex
}

type streamIdentityKey struct{}

// streamIdentity is what the gRPC auth interceptor verified for a tunnel stream.
type streamIdentity struct {
	environmentID string
	securityMode  string
}

type contextualServerStream struct {
	grpc.ServerStream

	ctx context.Context
}

// GeneratedMTLSFile describes a generated file that should be copied to the edge agent host.
type GeneratedMTLSFile struct {
	Name          string `json:"name"`
	Content       string `json:"content"`
	ContainerPath string `json:"containerPath"`
	Permissions   string `json:"permissions"`
}

// GeneratedMTLSAssets contains manager-generated edge client certificates and snippet metadata.
type GeneratedMTLSAssets struct {
	Files       []GeneratedMTLSFile `json:"files"`
	HostDirHint string              `json:"hostDirHint"`
	CertIssued  bool                `json:"-"`
	CAGenerated bool                `json:"-"`
	Reenrolled  bool                `json:"-"`
}

type enrollMTLSResponse struct {
	Files []GeneratedMTLSFile `json:"files"`
}

// Config contains the public edge-tunnel runtime settings needed by pkg/libarcane/edge.
type Config struct {
	EdgeAgent             bool
	EdgeTransport         string
	EdgeReconnectInterval int
	EdgeMTLSMode          string
	EdgeMTLSCAFile        string
	EdgeMTLSCertFile      string
	EdgeMTLSKeyFile       string
	EdgeMTLSServerName    string
	EdgeMTLSAssetsDir     string
	AppURL                string
	ManagerApiUrl         string
	AgentToken            string
	Port                  string
	Listen                string
}

// TunnelRuntimeState describes the live, in-memory state of an active edge tunnel.
type TunnelRuntimeState struct {
	// RoundTripTime is the last keepalive round trip.
	RoundTripTime *time.Duration
	// OpenStreams counts open mux streams, including pooled idle ones.
	OpenStreams   int
	Transport     string
	ConnectedAt   *time.Time
	LastHeartbeat *time.Time
	SessionID     string
	AgentInstance string
	SecurityMode  string
	Capabilities  []string
	State         string
}

// TunnelMessage is the register handshake both peers exchange before switching to the multiplexed data plane.
type TunnelMessage struct {
	ID            string            `json:"id"`
	Type          TunnelMessageType `json:"type"`
	AgentToken    string            `json:"agent_token,omitempty"`
	EnvironmentID string            `json:"environment_id,omitempty"`
	Error         string            `json:"error,omitempty"`
	SessionID     string            `json:"session_id,omitempty"`
	ResumeSession string            `json:"resume_session,omitempty"`
	AgentInstance string            `json:"agent_instance,omitempty"`
	SecurityMode  string            `json:"security_mode,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	Accepted      bool              `json:"accepted,omitempty"`
	DrainPrevious bool              `json:"drain_previous,omitempty"`
}

// TunnelEvent is an event payload sent from an agent to the manager.
type TunnelEvent struct {
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
type TunnelConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed atomic.Bool
	muxed  atomic.Bool
}

// GRPCManagerTunnelConn wraps the manager-side gRPC tunnel stream.
type GRPCManagerTunnelConn struct {
	stream grpcManagerStream
	cancel context.CancelFunc
	mu     sync.Mutex
	closed atomic.Bool
	muxed  atomic.Bool
}

// GRPCAgentTunnelConn wraps the agent-side gRPC tunnel stream.
type GRPCAgentTunnelConn struct {
	stream grpcAgentStream
	cancel context.CancelFunc
	mu     sync.Mutex
	closed atomic.Bool
	muxed  atomic.Bool
}

// muxSession is the manager end of a multiplexed data plane. client dials streams once ready closes.
type muxSession struct {
	client    *http.Client
	ready     chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	session   *yamux.Session
	// rtt is the last keepalive round trip in nanoseconds, zero until the first ping.
	rtt atomic.Int64
}

// muxConn adapts a message transport to the byte stream yamux runs on.
type muxConn struct {
	recv           func() ([]byte, error)
	closeTransport func() error
	closeOnce      sync.Once
	unread         []byte

	mu      sync.Mutex
	cond    sync.Cond
	pending []byte
	err     error
}

type cancelableGRPCManagerStream struct {
	stream   grpcManagerStream
	ctx      context.Context
	recvCh   chan grpcRecvResult
	recvOnce sync.Once
}

type grpcRecvResult struct {
	msg *tunnelpb.AgentMessage
	err error
}
