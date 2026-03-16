package edge

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// DefaultTunnelPollInterval is how often poll-mode agents should check in.
	DefaultTunnelPollInterval = 2 * time.Second
	// DefaultPollRuntimeTTL is how long a poll check-in is considered fresh for
	// runtime status reporting when no live tunnel is currently open.
	DefaultPollRuntimeTTL = 6 * time.Second
	// DefaultTunnelDemandTTL is how long the manager should keep an edge tunnel
	// marked as required after a user/API request touches the environment.
	DefaultTunnelDemandTTL = 2 * time.Minute

	// TunnelStatusIdle indicates that no reverse tunnel is currently needed.
	TunnelStatusIdle = "IDLE"
	// TunnelStatusRequired indicates that the manager needs the agent to open a tunnel.
	TunnelStatusRequired = "REQUIRED"
	// TunnelStatusActive indicates that the manager still needs the tunnel and it is already open.
	TunnelStatusActive = "ACTIVE"
)

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

func pollRuntimeTTLInternal(state PollRuntimeState) time.Duration {
	ttl := DefaultPollRuntimeTTL
	if state.PollIntervalSeconds > 0 {
		intervalTTL := time.Duration(state.PollIntervalSeconds) * time.Second * 3
		if intervalTTL > ttl {
			ttl = intervalTTL
		}
	}
	return ttl
}

// NewTunnelDemandRegistry creates a new tunnel demand registry.
func NewTunnelDemandRegistry() *TunnelDemandRegistry {
	return &TunnelDemandRegistry{demands: make(map[string]time.Time)}
}

// NewPollRuntimeRegistry creates a new poll runtime registry.
func NewPollRuntimeRegistry() *PollRuntimeRegistry {
	return &PollRuntimeRegistry{states: make(map[string]PollRuntimeState)}
}

// Touch marks an environment as requiring a reverse tunnel for the specified TTL.
func (r *TunnelDemandRegistry) Touch(envID string, ttl time.Duration) time.Time {
	if r == nil || strings.TrimSpace(envID) == "" {
		return time.Time{}
	}
	if ttl <= 0 {
		ttl = DefaultTunnelDemandTTL
	}

	expiresAt := time.Now().Add(ttl)
	r.mu.Lock()
	r.demands[envID] = expiresAt
	r.mu.Unlock()
	return expiresAt
}

// DesiredStatus returns the desired manager-side tunnel state for an environment.
func (r *TunnelDemandRegistry) DesiredStatus(envID string, hasActiveTunnel bool, now time.Time) string {
	if r == nil || strings.TrimSpace(envID) == "" {
		if hasActiveTunnel {
			return TunnelStatusActive
		}
		return TunnelStatusIdle
	}
	if now.IsZero() {
		now = time.Now()
	}

	r.mu.RLock()
	expiresAt, ok := r.demands[envID]
	r.mu.RUnlock()

	if ok && !now.Before(expiresAt) {
		r.mu.Lock()
		expiresAt, ok = r.demands[envID]
		if ok && !now.Before(expiresAt) {
			delete(r.demands, envID)
			ok = false
		}
		r.mu.Unlock()
	}

	if !ok {
		return TunnelStatusIdle
	}
	if hasActiveTunnel {
		return TunnelStatusActive
	}
	return TunnelStatusRequired
}

var (
	defaultDemandRegistry = NewTunnelDemandRegistry()
	defaultPollRuntime    = NewPollRuntimeRegistry()
)

// GetDemandRegistry returns the process-wide tunnel demand registry.
func GetDemandRegistry() *TunnelDemandRegistry {
	return defaultDemandRegistry
}

// GetPollRuntimeRegistry returns the process-wide poll runtime registry.
func GetPollRuntimeRegistry() *PollRuntimeRegistry {
	return defaultPollRuntime
}

// TouchTunnelDemand marks an edge environment as requiring an on-demand tunnel.
func TouchTunnelDemand(envID string, ttl time.Duration) time.Time {
	return GetDemandRegistry().Touch(envID, ttl)
}

// Update records a poll check-in for an environment.
func (r *PollRuntimeRegistry) Update(envID string, interval time.Duration, now time.Time) PollRuntimeState {
	if r == nil || strings.TrimSpace(envID) == "" {
		return PollRuntimeState{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	seconds := int(interval / time.Second)
	if seconds <= 0 {
		seconds = int(DefaultTunnelPollInterval / time.Second)
	}

	state := PollRuntimeState{
		LastPollAt:          &now,
		PollIntervalSeconds: seconds,
	}

	r.mu.Lock()
	r.states[envID] = state
	r.mu.Unlock()

	return state
}

// Get returns the most recent poll runtime state if it is still fresh.
func (r *PollRuntimeRegistry) Get(envID string, now time.Time) (PollRuntimeState, bool) {
	if r == nil || strings.TrimSpace(envID) == "" {
		return PollRuntimeState{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	r.mu.RLock()
	state, ok := r.states[envID]
	r.mu.RUnlock()
	if !ok || state.LastPollAt == nil {
		return PollRuntimeState{}, false
	}

	ttl := pollRuntimeTTLInternal(state)

	if now.Sub(*state.LastPollAt) > ttl {
		r.mu.Lock()
		state, ok = r.states[envID]
		if !ok || state.LastPollAt == nil {
			r.mu.Unlock()
			return PollRuntimeState{}, false
		}
		ttl = pollRuntimeTTLInternal(state)
		if now.Sub(*state.LastPollAt) > ttl {
			delete(r.states, envID)
			r.mu.Unlock()
			return PollRuntimeState{}, false
		}
		r.mu.Unlock()
	}

	return state, true
}

func decodeTunnelPollRequestInternal(c *gin.Context) (*TunnelPollRequest, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return &TunnelPollRequest{}, nil
	}

	defer func() { _ = c.Request.Body.Close() }()

	var req TunnelPollRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		if errors.Is(err, http.ErrBodyReadAfterClose) {
			return &TunnelPollRequest{}, nil
		}
		if errors.Is(err, io.EOF) {
			return &TunnelPollRequest{}, nil
		}
		return nil, err
	}

	return &req, nil
}

func (s *TunnelServer) pollStatusInternal(envID string) TunnelPollResponse {
	hasActiveTunnel := false
	activeTransport := ""

	if tunnel, ok := s.registry.Get(envID); ok && tunnel != nil && tunnel.Conn != nil && !tunnel.Conn.IsClosed() {
		hasActiveTunnel = true
		switch tunnel.Conn.(type) {
		case *GRPCManagerTunnelConn:
			activeTransport = EdgeTransportGRPC
		case *TunnelConn:
			activeTransport = EdgeTransportWebSocket
		}
	}

	status := GetDemandRegistry().DesiredStatus(envID, hasActiveTunnel, time.Now())
	return TunnelPollResponse{
		Status:              status,
		PollIntervalSeconds: int(DefaultTunnelPollInterval / time.Second),
		ActiveTransport:     activeTransport,
		Connected:           hasActiveTunnel,
	}
}

// HandlePoll is the HTTP control-plane endpoint used by poll-mode agents.
func (s *TunnelServer) HandlePoll(c *gin.Context) {
	ctx := c.Request.Context()

	if _, err := decodeTunnelPollRequestInternal(c); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid poll payload"})
		return
	}

	token := c.GetHeader(HeaderAgentToken)
	if token == "" {
		token = c.GetHeader(HeaderAPIKey)
	}
	if token == "" {
		slog.WarnContext(ctx, "Edge poll request without token")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "agent token required"})
		return
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token for edge poll", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
		return
	}

	pollInterval := DefaultTunnelPollInterval
	GetPollRuntimeRegistry().Update(envID, pollInterval, time.Now())

	resp := s.pollStatusInternal(envID)
	resp.PollIntervalSeconds = int(pollInterval / time.Second)
	c.JSON(http.StatusOK, resp)
}
