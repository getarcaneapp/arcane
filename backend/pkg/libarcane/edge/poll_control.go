package edge

import (
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
)

const (
	// DefaultTunnelPollInterval is how often poll-mode agents should check in.
	DefaultTunnelPollInterval = 2 * time.Second
	// DefaultPollRuntimeTTL is the minimum time a poll check-in stays fresh for runtime status.
	DefaultPollRuntimeTTL = 6 * time.Second
	// DefaultTunnelDemandTTL keeps a touched environment's tunnel required; it exceeds the 2m health
	// interval so health checks keep idle poll-mode tunnels open instead of flapping.
	DefaultTunnelDemandTTL = 5 * time.Minute

	// TunnelStatusIdle indicates that no reverse tunnel is currently needed.
	TunnelStatusIdle = "IDLE"
	// TunnelStatusRequired indicates that the manager needs the agent to open a tunnel.
	TunnelStatusRequired = "REQUIRED"
	// TunnelStatusActive indicates that the manager still needs the tunnel and it is already open.
	TunnelStatusActive = "ACTIVE"
)

// pollRuntimeTTL is how long a check-in stays fresh: three poll intervals, never under DefaultPollRuntimeTTL.
func pollRuntimeTTL(state PollRuntimeState) time.Duration {
	return max(DefaultPollRuntimeTTL, time.Duration(state.PollIntervalSeconds)*time.Second*3)
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
		return kit.Ternary(hasActiveTunnel, TunnelStatusActive, TunnelStatusIdle)
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
	return kit.Ternary(hasActiveTunnel, TunnelStatusActive, TunnelStatusRequired)
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
func (r *PollRuntimeRegistry) Get(envID string, now time.Time) mo.Option[PollRuntimeState] {
	if r == nil || strings.TrimSpace(envID) == "" {
		return mo.None[PollRuntimeState]()
	}
	if now.IsZero() {
		now = time.Now()
	}

	r.mu.RLock()
	state, ok := r.states[envID]
	r.mu.RUnlock()
	if !ok || state.LastPollAt == nil {
		return mo.None[PollRuntimeState]()
	}

	ttl := pollRuntimeTTL(state)

	if now.Sub(*state.LastPollAt) > ttl {
		r.mu.Lock()
		state, ok = r.states[envID]
		if !ok || state.LastPollAt == nil {
			r.mu.Unlock()
			return mo.None[PollRuntimeState]()
		}
		ttl = pollRuntimeTTL(state)
		if now.Sub(*state.LastPollAt) > ttl {
			delete(r.states, envID)
			r.mu.Unlock()
			return mo.None[PollRuntimeState]()
		}
		r.mu.Unlock()
	}

	return mo.Some(state)
}

// HandlePoll is the HTTP control-plane endpoint used by poll-mode agents.
func (s *TunnelServer) HandlePoll(c *echo.Context) error {
	req := c.Request()
	ctx := req.Context()

	if req.Body != nil {
		var pollReq TunnelPollRequest
		err := json.UnmarshalRead(req.Body, &pollReq)
		_ = req.Body.Close()
		if err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) && !errors.Is(err, io.EOF) {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid poll payload"})
		}
	}

	// Proxy-terminated mTLS consumes the client certificate, so the token remains the environment claim.
	token, source := agentToken(req.Header.Values)
	if token == "" {
		slog.WarnContext(ctx, "Edge poll request without token")
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "agent token required"})
	}
	if source != HeaderAgentToken {
		slog.DebugContext(ctx, "Edge poll request authenticated via fallback header",
			"sourceHeader", source,
			"tokenLength", len(token),
		)
	}

	envID, err := s.resolveEnvironment(ctx, token)
	if err != nil {
		slog.WarnContext(ctx, "Failed to resolve agent token for edge poll",
			"error", err,
			"sourceHeader", source,
			"tokenLength", len(token),
			"tokenFingerprint", remenv.RedactedTokenFingerprint(token),
		)
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid agent token"})
	}
	if identityErr := s.requireCertificateIdentity(req.TLS, envID); identityErr != nil {
		slog.WarnContext(ctx, "Rejected edge poll request with mismatched client certificate", "environmentId", envID, "error", identityErr)
		return c.JSON(http.StatusUnauthorized, map[string]any{"error": identityErr.Error()})
	}

	now := time.Now()
	GetPollRuntimeRegistry().Update(envID, DefaultTunnelPollInterval, now)

	tunnel, _ := s.registry.Get(envID).Get()
	connected := tunnel.connected()
	resp := TunnelPollResponse{
		Status:              GetDemandRegistry().DesiredStatus(envID, connected, now),
		PollIntervalSeconds: int(DefaultTunnelPollInterval / time.Second),
		Connected:           connected,
	}
	if connected {
		resp.ActiveTransport = tunnel.Conn.Transport()
	}
	return c.JSON(http.StatusOK, resp)
}
