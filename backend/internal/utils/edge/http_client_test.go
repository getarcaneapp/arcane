package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEdgeAwareClient_DoForEnvironment_EdgeWithTunnel(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusOK,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"edge":true}`),
		}
	})
	defer server.Close()
	defer tunnel.Close()

	envID := "env-edge-1"
	GetRegistry().Register(envID, tunnel)
	defer GetRegistry().Unregister(envID)

	client := NewEdgeAwareClient(1 * time.Second)

	resp, err := client.DoForEnvironment(
		context.Background(),
		envID,
		true, // isEdge
		"GET",
		"http://ignored/api/path",
		"/api/path",
		map[string]string{"X-H": "v"},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte(`{"edge":true}`), resp.Body)
}

func TestEdgeAwareClient_DoForEnvironment_EdgeNoTunnel(t *testing.T) {
	client := NewEdgeAwareClient(1 * time.Second)

	_, err := client.DoForEnvironment(
		context.Background(),
		"env-edge-missing",
		true, // isEdge
		"GET",
		"http://ignored/api/path",
		"/api/path",
		nil,
		nil,
	)

	assert.Contains(t, err.Error(), "not connected")
}

func TestEdgeAwareClient_DoForEnvironment_NonEdge(t *testing.T) {
	// Setup direct server
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/direct", r.URL.Path)
		assert.Equal(t, "val", r.Header.Get("X-Direct"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("direct response"))
	}))
	defer directServer.Close()

	client := NewEdgeAwareClient(1 * time.Second)

	resp, err := client.DoForEnvironment(
		context.Background(),
		"env-direct",
		false, // isEdge (false)
		"GET",
		directServer.URL+"/api/direct",
		"/api/direct",
		map[string]string{"X-Direct": "val"},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, []byte("direct response"), resp.Body)
}

func TestDoEdgeAwareRequest_Helper(t *testing.T) {
	// Just test it calls the default client correctly (using non-edge for simplicity)
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("helper"))
	}))
	defer directServer.Close()

	resp, err := DoEdgeAwareRequest(
		context.Background(),
		"env-helper",
		false,
		"GET",
		directServer.URL,
		"/",
		nil,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte("helper"), resp.Body)
}
