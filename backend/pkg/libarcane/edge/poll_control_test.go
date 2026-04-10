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

	"github.com/gin-gonic/gin"
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
	gin.SetMode(gin.TestMode)

	registry := NewTunnelRegistry()
	server := NewTunnelServerWithRegistry(registry, func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return "env-poll-1", nil
	}, nil)

	router := gin.New()
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

func TestPollRuntimeRegistryGetExpiresStaleState(t *testing.T) {
	r := NewPollRuntimeRegistry()
	now := time.Now()
	r.Update("env-stale", DefaultTunnelPollInterval, now)

	state, ok := r.Get("env-stale", now.Add(DefaultPollRuntimeTTL+time.Second))
	assert.False(t, ok)
	assert.Nil(t, state.LastPollAt)
}
