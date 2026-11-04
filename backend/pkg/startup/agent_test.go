package startup

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/types/v3"
)

type memoryTokenStore map[string]string

func (m memoryTokenStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := m[key]
	return value, ok, nil
}

func (m memoryTokenStore) Set(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

func newEnrollmentTestStore(t *testing.T) memoryTokenStore {
	t.Helper()
	crypto.InitEncryption(crypto.Config{EncryptionKey: "enrollment-startup-test-key-32bytes!", Environment: "test"})
	return memoryTokenStore{}
}

func describeSwarmHost(context.Context) (types.AgentEnrollRequest, error) {
	return types.AgentEnrollRequest{
		Hostname:       "host-1",
		SwarmNodeID:    "node1",
		SwarmClusterID: "cluster1",
	}, nil
}

func TestEnrollAgent_RetriesUntilEnrolledAndStoresEncryptedToken(t *testing.T) {
	kvService := newEnrollmentTestStore(t)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "manager is starting", http.StatusServiceUnavailable)
			return
		}
		var request types.AgentEnrollRequest
		if !assert.Equal(t, "/api/agents/enroll", r.URL.Path) ||
			!assert.Equal(t, "arc_etkn_enrollment-secret", r.Header.Get(agent.HeaderEnrollmentToken)) ||
			!assert.Empty(t, r.Header.Get(agent.HeaderAgentToken)) ||
			!assert.NoError(t, json.UnmarshalRead(r.Body, &request)) ||
			!assert.Equal(t, types.AgentEnrollRequest{
				Hostname:       "host-1",
				SwarmNodeID:    "node1",
				SwarmClusterID: "cluster1",
			}, request) {
			http.Error(w, "unexpected enrollment request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"environmentId":"env-1","token":"node-token"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	assetsDir := t.TempDir()
	for _, name := range []string{"agent.crt", "agent.key", "ca.crt"} {
		require.NoError(t, os.WriteFile(filepath.Join(assetsDir, name), []byte("previous enrollment"), 0o600))
	}
	cfg := &agent.Config{
		MTLS: agent.NewMTLS(agent.MTLSConfig{
			AssetsDir:  assetsDir,
			ManagerURL: server.URL,
		}),
		AgentToken: "arc_etkn_enrollment-secret",
	}

	token, err := EnrollAgent(ctx, EnrollAgentOptions{
		Config:       cfg,
		Store:        kvService,
		DescribeHost: describeSwarmHost,
	})
	require.NoError(t, err)
	assert.Equal(t, "node-token", token)
	assert.Equal(t, int32(2), calls.Load())
	assert.NoFileExists(t, filepath.Join(assetsDir, "agent.crt"), "a certificate naming the previous environment is discarded")
	assert.NoFileExists(t, filepath.Join(assetsDir, "agent.key"))
	assert.FileExists(t, filepath.Join(assetsDir, "ca.crt"))

	stored, found, err := kvService.Get(t.Context(), enrolledAgentTokenKey)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEqual(t, "node-token", stored, "the token is stored encrypted")
	decrypted, err := crypto.Decrypt(stored)
	require.NoError(t, err)
	assert.Equal(t, "node-token", decrypted)
}

func TestEnrollAgent_SendsStoredTokenAndKeepsItWhenEnrollmentFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{name: "refused", status: http.StatusUnauthorized},
		{name: "manager unavailable", status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kvService := newEnrollmentTestStore(t)
			encrypted, err := crypto.Encrypt("stored-token")
			require.NoError(t, err)
			require.NoError(t, kvService.Set(t.Context(), enrolledAgentTokenKey, encrypted))

			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "stored-token", r.Header.Get(agent.HeaderAgentToken))
				http.Error(w, "enrollment failed", tc.status)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			token, err := EnrollAgent(ctx, EnrollAgentOptions{
				Config: &agent.Config{
					MTLS:       agent.NewMTLS(agent.MTLSConfig{ManagerURL: server.URL}),
					AgentToken: "arc_etkn_revoked-secret",
				},
				Store:        kvService,
				DescribeHost: describeSwarmHost,
			})
			require.NoError(t, err)
			assert.Equal(t, "stored-token", token)
			assert.Equal(t, int32(1), calls.Load(), "with a stored token the agent tries once and starts with it")

			stillStored, _, err := kvService.Get(t.Context(), enrolledAgentTokenKey)
			require.NoError(t, err)
			assert.Equal(t, encrypted, stillStored)
		})
	}
}

func TestEnrollAgent_StopsWhenContextEnds(t *testing.T) {
	kvService := newEnrollmentTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cfg := &agent.Config{
		MTLS:       agent.NewMTLS(agent.MTLSConfig{ManagerURL: "http://127.0.0.1:1"}),
		AgentToken: "arc_etkn_enrollment-secret",
	}

	token, err := EnrollAgent(ctx, EnrollAgentOptions{
		Config: cfg,
		Store:  kvService,
		DescribeHost: func(context.Context) (types.AgentEnrollRequest, error) {
			cancel()
			return types.AgentEnrollRequest{}, errors.New("docker is unavailable")
		},
	})

	require.Error(t, err)
	assert.Empty(t, token)
}

func TestEnrollAgent_KeepsApiKeyTokensForPairing(t *testing.T) {
	kvService := newEnrollmentTestStore(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()

	token, err := EnrollAgent(t.Context(), EnrollAgentOptions{
		Config: &agent.Config{
			MTLS:       agent.NewMTLS(agent.MTLSConfig{ManagerURL: server.URL}),
			AgentToken: "arc_existing-api-key",
		},
		Store:        kvService,
		DescribeHost: describeSwarmHost,
	})
	require.NoError(t, err)
	assert.Equal(t, "arc_existing-api-key", token)
	assert.Zero(t, calls.Load(), "an API key token pairs instead of enrolling")

	_, found, err := kvService.Get(t.Context(), enrolledAgentTokenKey)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestEnrollAgent_FailsWhenRefusedWithoutStoredToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		calls  int32
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, calls: 2},
		{name: "conflict", status: http.StatusConflict, calls: 2},
		{name: "too many requests is retried", status: http.StatusTooManyRequests, calls: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kvService := newEnrollmentTestStore(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				switch calls.Add(1) {
				case 1:
					http.Error(w, "manager is starting", http.StatusServiceUnavailable)
				case 2:
					http.Error(w, "refused by manager", tc.status)
				default:
					http.Error(w, "invalid enrollment token", http.StatusUnauthorized)
				}
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			token, err := EnrollAgent(ctx, EnrollAgentOptions{
				Config: &agent.Config{
					MTLS:       agent.NewMTLS(agent.MTLSConfig{ManagerURL: server.URL}),
					AgentToken: "arc_etkn_revoked-secret",
				},
				Store:        kvService,
				DescribeHost: describeSwarmHost,
			})

			require.Error(t, err)
			assert.Empty(t, token)
			require.ErrorContains(t, err, "ARCANE_AGENT_TOKEN")
			assert.Equal(t, tc.calls, calls.Load(), "only 5xx, 408 and 429 responses are retried")
		})
	}
}
