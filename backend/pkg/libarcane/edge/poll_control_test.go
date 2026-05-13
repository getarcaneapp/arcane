package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTunnelDemandRegistryDesiredStatus(t *testing.T) {
	r := NewTunnelDemandRegistry()
	now := time.Now()

	assert.Equal(t, TunnelStatusIdle, r.DesiredStatus("env-1", false, now))

	r.Touch("env-1", time.Minute)
	assert.Equal(t, TunnelStatusRequired, r.DesiredStatus("env-1", false, now))
	assert.Equal(t, TunnelStatusActive, r.DesiredStatus("env-1", true, now))
	assert.Equal(t, TunnelStatusIdle, r.DesiredStatus("env-1", false, now.Add(2*time.Minute)))
}

func TestTunnelServer_HandlePoll(t *testing.T) {
	registry := NewTunnelRegistry()
	server := NewTunnelServerWithRegistry(registry, func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return "env-poll-1", nil
	}, nil)

	router := echo.New()
	router.POST("/api/tunnel/poll", server.HandlePoll)

	TouchTunnelDemand("env-poll-1", time.Minute)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/poll", bytes.NewBufferString(`{"transport":"poll"}`))
	req.Header.Set(HeaderAgentToken, "valid-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp TunnelPollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, TunnelStatusRequired, resp.Status)
	assert.Equal(t, int(DefaultTunnelPollInterval/time.Second), resp.PollIntervalSeconds)
	assert.False(t, resp.Connected)
	if state, ok := GetPollRuntimeRegistry().Get("env-poll-1", time.Now()); assert.True(t, ok) {
		assert.NotNil(t, state.LastPollAt)
		assert.Equal(t, int(DefaultTunnelPollInterval/time.Second), state.PollIntervalSeconds)
	}

	registry.Register("env-poll-1", NewAgentTunnelWithConn("env-poll-1", NewGRPCManagerTunnelConn(nil)))
	t.Cleanup(func() { registry.Unregister("env-poll-1") })

	req = httptest.NewRequest(http.MethodPost, "/api/tunnel/poll", bytes.NewBufferString(`{"transport":"poll","connected":true}`))
	req.Header.Set(HeaderAgentToken, "valid-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, TunnelStatusActive, resp.Status)
	assert.Equal(t, int(DefaultTunnelPollInterval/time.Second), resp.PollIntervalSeconds)
	assert.True(t, resp.Connected)
	assert.Equal(t, EdgeTransportGRPC, resp.ActiveTransport)
}

// TestTunnelServer_HandlePoll_AcceptsTokenFromAllSupportedHeaders pins that
// every header form the agent sends — X-Arcane-Agent-Token, X-API-Key, and
// Authorization: Bearer — is accepted by the manager. The Authorization
// fallback specifically guards against reverse proxies (e.g. Cloudflare-style
// access policies) that strip non-standard X- headers; without it, agents
// behind such proxies log "invalid agent token" indefinitely.
func TestTunnelServer_HandlePoll_AcceptsTokenFromAllSupportedHeaders(t *testing.T) {
	cases := []struct {
		name        string
		setHeader   func(req *http.Request)
		expectCode  int
		expectError string
	}{
		{
			name: "X-Arcane-Agent-Token",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAgentToken, "valid-token")
			},
			expectCode: http.StatusOK,
		},
		{
			name: "X-API-Key",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAPIKey, "valid-token")
			},
			expectCode: http.StatusOK,
		},
		{
			name: "Authorization Bearer",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAuthorization, "Bearer valid-token")
			},
			expectCode: http.StatusOK,
		},
		{
			name: "Authorization bearer lowercase scheme",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAuthorization, "bearer valid-token")
			},
			expectCode: http.StatusOK,
		},
		{
			name: "Authorization without Bearer scheme is rejected",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAuthorization, "valid-token")
			},
			expectCode:  http.StatusUnauthorized,
			expectError: "agent token required",
		},
		{
			name: "Authorization Basic is rejected",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAuthorization, "Basic dmFsaWQtdG9rZW4=")
			},
			expectCode:  http.StatusUnauthorized,
			expectError: "agent token required",
		},
		{
			name: "no headers is rejected",
			setHeader: func(req *http.Request) {
			},
			expectCode:  http.StatusUnauthorized,
			expectError: "agent token required",
		},
		{
			name: "wrong token is rejected",
			setHeader: func(req *http.Request) {
				req.Header.Set(HeaderAuthorization, "Bearer not-the-token")
			},
			expectCode:  http.StatusUnauthorized,
			expectError: "invalid agent token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := NewTunnelServerWithRegistry(NewTunnelRegistry(), func(ctx context.Context, token string) (string, error) {
				if token != "valid-token" {
					return "", errors.New("invalid token")
				}
				return "env-headers", nil
			}, nil)

			router := echo.New()
			router.POST("/api/tunnel/poll", server.HandlePoll)

			req := httptest.NewRequest(http.MethodPost, "/api/tunnel/poll", bytes.NewBufferString(`{"transport":"poll"}`))
			tc.setHeader(req)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, tc.expectCode, rec.Code, "body: %s", rec.Body.String())
			if tc.expectError != "" {
				assert.Contains(t, rec.Body.String(), tc.expectError)
			}
		})
	}
}

func TestTunnelServer_HandlePoll_AcceptsTokenAfterProxyTerminatedMTLS(t *testing.T) {
	server := NewTunnelServerWithRegistry(NewTunnelRegistry(), func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return "env-proxy-mtls", nil
	}, nil)
	server.SetConfig(&Config{
		EdgeMTLSMode: EdgeMTLSModeRequired,
		AppURL:       "https://manager.example.com",
	})

	router := echo.New()
	router.POST("/api/tunnel/poll", server.HandlePoll)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/poll", bytes.NewBufferString(`{"transport":"poll"}`))
	req.Header.Set(HeaderAgentToken, "valid-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp TunnelPollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, TunnelStatusIdle, resp.Status)
}

func TestPollRuntimeRegistryGetExpiresStaleState(t *testing.T) {
	r := NewPollRuntimeRegistry()
	now := time.Now()
	r.Update("env-stale", DefaultTunnelPollInterval, now)

	state, ok := r.Get("env-stale", now.Add(DefaultPollRuntimeTTL+time.Second))
	assert.False(t, ok)
	assert.Nil(t, state.LastPollAt)
}
