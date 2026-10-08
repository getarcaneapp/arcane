package edge

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/grpc"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

// activeWSStream tracks an active WebSocket stream on the agent side.
type activeWSStream struct {
	ws     *websocket.Conn
	conn   TunnelConnection // tunnel connection the stream was opened on
	cancel context.CancelFunc
	dataCh chan wsPayload
	mu     sync.Mutex
	closed bool
}

type wsPayload struct {
	messageType int
	data        []byte
}

type commandRequestTransfer struct {
	request      *TunnelMessage
	conn         TunnelConnection
	body         bytes.Buffer
	nextSequence int64
	timerMu      sync.Mutex
	timer        *time.Timer
}

// TunnelClient represents the agent-side tunnel client
type TunnelClient struct {
	cfg                    *Config
	handler                http.Handler
	reconnectInterval      time.Duration
	heartbeatInterval      time.Duration
	registrationTimeout    time.Duration
	websocketPreferenceTTL time.Duration
	healthySessionDuration time.Duration
	managerURL             string
	managerGRPCAddr        string
	localPort              string // Port the agent is running on locally
	conn                   atomic.Pointer[connBox]
	requestTimeout         time.Duration
	activeStreams          sync.Map // map[string]*activeWSStream
	requestTransfers       sync.Map // map[string]*commandRequestTransfer
	commandRecorders       sync.Map // map[string]*commandResponseRecorder awaiting credit
	transportPreferenceMu  sync.RWMutex
	preferWebSocketUntil   time.Time
	grpcFailureStreak      int
	agentInstanceID        string
	registration           concurrency.Snapshot[clientRegistrationInternal]
}

type clientRegistrationInternal struct {
	sessionID string
	// commandCredit is set when the manager grants command output flow-control credit.
	commandCredit bool
}

// connBox wraps the active TunnelConnection so it can be swapped atomically on
// reconnect. The wrapper is required because the gRPC and WebSocket connections
// are different concrete types; a bare atomic.Value would panic on the type
// change, whereas an atomic.Pointer to a fixed box type does not.
type connBox struct {
	conn TunnelConnection
}

type managedTunnelTransportsInternal struct {
	grpc      bool
	websocket bool
}

// responseRecorder captures HTTP responses
type responseRecorder struct {
	headers    http.Header
	body       bytes.Buffer
	statusCode int
}

type commandResponseRecorder struct {
	conn        TunnelConnection
	headers     http.Header
	commandID   string
	commandName string
	buffer      bytes.Buffer
	statusCode  int
	sequence    int64
	// flushErr keeps the first send failure from Flush, which cannot return it.
	flushErr error
	// With manager flow control, sends wait while inFlight would exceed window; credits signal room.
	ctx         context.Context
	window      int64
	inFlight    atomic.Int64
	credited    chan struct{}
	mu          sync.Mutex
	wroteHeader bool
	streaming   bool
	closed      bool
}

type streamingResponseRecorder struct {
	requestID   string
	conn        TunnelConnection
	headers     http.Header
	statusCode  int
	wroteHeader bool
	closed      bool
	mu          sync.Mutex
}

type pollManagedTunnelSession struct {
	cancel   context.CancelFunc
	done     chan error
	stopping bool
}

type CommandRequest struct {
	ID            string
	Command       string
	Method        string
	Path          string
	Query         string
	Headers       map[string]string
	Body          []byte
	TimeoutMillis int64
	// Output, when set, receives the response as it streams in; CommandResult.Body is then nil.
	Output http.ResponseWriter
}

type CommandResult struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

type CommandClient struct{}

type commandRoute struct {
	Method      string
	PathPattern string
	CommandName string
	Stream      bool
	LocalOnly   bool
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

type commandRouteIndexInternal struct {
	roots map[commandRouteKey]*commandRouteNode
}

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

type grpcResponseState struct {
	out         http.ResponseWriter
	outErr      error
	credit      func(consumed int)
	status      int
	respHeaders map[string]string
	respBody    bytes.Buffer
	gotResponse bool
	committed   bool
}

// AgentTunnel represents an active tunnel connection from an edge agent
type AgentTunnel struct {
	EnvironmentID string
	Conn          TunnelConnection
	Pending       sync.Map // map[string]*PendingRequest
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

	// Command credits queue here for a per-tunnel sender, so readers never block on the connection.
	creditOnce   sync.Once
	creditMu     sync.Mutex
	credits      map[string]int64
	creditSignal chan struct{}
}

// TunnelRegistry manages active edge agent tunnel connections
type TunnelRegistry struct {
	stopped atomic.Bool
	tunnels *concurrency.StateMap[string, *AgentTunnel]
}

type internalTunnelRequestContextKey struct{}

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
	Transport     string
	ConnectedAt   *time.Time
	LastHeartbeat *time.Time
	SessionID     string
	AgentInstance string
	SecurityMode  string
	Capabilities  []string
	State         string
}

// TunnelMessage represents a transport-agnostic edge tunnel message.
type TunnelMessage struct {
	Headers       map[string]string `json:"headers,omitempty"`         // HTTP headers
	Event         *TunnelEvent      `json:"event,omitempty"`           // Agent event payload
	Metadata      map[string]string `json:"metadata,omitempty"`        // Correlation and audit metadata
	ID            string            `json:"id"`                        // Unique request/stream ID
	Type          TunnelMessageType `json:"type"`                      // Message type
	Method        string            `json:"method,omitempty"`          // HTTP method for requests
	Path          string            `json:"path,omitempty"`            // Request path
	Query         string            `json:"query,omitempty"`           // Query string
	AgentToken    string            `json:"agent_token,omitempty"`     // Register request token
	EnvironmentID string            `json:"environment_id,omitempty"`  // Manager-resolved environment ID
	Error         string            `json:"error,omitempty"`           // Error field for register response
	Command       string            `json:"command,omitempty"`         // Typed command name
	SessionID     string            `json:"session_id,omitempty"`      // Manager-issued session identifier
	ResumeSession string            `json:"resume_session,omitempty"`  // Previous session being resumed
	AgentInstance string            `json:"agent_instance,omitempty"`  // Stable agent runtime identity
	SecurityMode  string            `json:"security_mode,omitempty"`   // token, mtls, etc.
	Body          []byte            `json:"body,omitempty"`            // Request/response body
	Capabilities  []string          `json:"capabilities,omitempty"`    // Agent advertised capabilities
	WSMessageType int               `json:"ws_message_type,omitempty"` // WebSocket message type
	Status        int               `json:"status,omitempty"`          // HTTP status for responses
	TimeoutMillis int64             `json:"timeout_millis,omitempty"`  // Command timeout
	Sequence      int64             `json:"sequence,omitempty"`        // Chunk sequence number
	Credit        int64             `json:"credit,omitempty"`          // Command output bytes the manager consumed
	Accepted      bool              `json:"accepted,omitempty"`        // Registration accepted
	DrainPrevious bool              `json:"drain_previous,omitempty"`  // Replace previous session
	Streaming     bool              `json:"streaming,omitempty"`       // Response used chunked output
	EOF           bool              `json:"eof,omitempty"`             // Final chunk indicator
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

// PendingRequest tracks an in-flight request waiting for response.
type PendingRequest struct {
	ResponseCh chan *TunnelMessage
	failureCh  chan error

	// mu guards the ordered backlog that absorbs bursts while ResponseCh is full.
	mu           sync.Mutex
	backlog      []*TunnelMessage
	backlogBytes int
	draining     bool
	failed       bool
}

// TunnelConn wraps a WebSocket connection with send/receive helpers.
type TunnelConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed atomic.Bool
}

// GRPCManagerTunnelConn wraps the manager-side gRPC tunnel stream.
type GRPCManagerTunnelConn struct {
	stream grpcManagerStream
	cancel context.CancelFunc
	mu     sync.Mutex
	closed atomic.Bool
	// parity is set at registration when the agent advertises proto-parity-v1,
	// enabling native stream_data/stream_end instead of the legacy ws_data re-encode.
	parity bool
}

// GRPCAgentTunnelConn wraps the agent-side gRPC tunnel stream.
type GRPCAgentTunnelConn struct {
	stream grpcAgentStream
	cancel context.CancelFunc
	mu     sync.Mutex
	closed atomic.Bool
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
