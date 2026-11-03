package bootstrap

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/types/v3"
)

func newEnrollmentTestKV(t *testing.T) *kv.KVService {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&kv.KVEntry{}))
	crypto.InitEncryption(crypto.Config{EncryptionKey: "enrollment-bootstrap-test-key-32bytes", Environment: "test"})
	return kv.NewKVService(&database.DB{DB: gormDB})
}

func localSwarmNodeID(context.Context) (string, error) { return "node1", nil }

func TestEnrollSwarmAgent_RetriesUntilEnrolledAndStoresEncryptedToken(t *testing.T) {
	kvService := newEnrollmentTestKV(t)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "manager is starting", http.StatusServiceUnavailable)
			return
		}
		var request types.NodeAgentEnrollRequest
		if !assert.Equal(t, "/api/swarm/agents/enroll", r.URL.Path) ||
			!assert.Equal(t, "enrollment-secret", r.Header.Get(middleware.HeaderEnrollmentToken)) ||
			!assert.Empty(t, r.Header.Get(middleware.HeaderAgentToken)) ||
			!assert.NoError(t, json.UnmarshalRead(r.Body, &request)) ||
			!assert.Equal(t, "node1", request.NodeID) {
			http.Error(w, "unexpected enrollment request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"environmentId":"env-1","token":"node-token"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cfg := &config.Config{ManagerApiUrl: server.URL, AgentEnrollmentToken: "enrollment-secret"}

	assert.Equal(t, "node-token", enrollSwarmAgent(ctx, cfg, kvService, localSwarmNodeID))
	assert.Equal(t, int32(2), calls.Load())

	stored, found, err := kvService.Get(t.Context(), enrolledAgentTokenKey)
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEqual(t, "node-token", stored, "the token is stored encrypted")
	decrypted, err := crypto.Decrypt(stored)
	require.NoError(t, err)
	assert.Equal(t, "node-token", decrypted)
}

func TestEnrollSwarmAgent_SendsStoredTokenAndKeepsItWhenRefused(t *testing.T) {
	kvService := newEnrollmentTestKV(t)
	encrypted, err := crypto.Encrypt("stored-token")
	require.NoError(t, err)
	require.NoError(t, kvService.Set(t.Context(), enrolledAgentTokenKey, encrypted))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "stored-token", r.Header.Get(middleware.HeaderAgentToken))
		http.Error(w, "invalid swarm enrollment token", http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := &config.Config{ManagerApiUrl: server.URL, AgentEnrollmentToken: "revoked-secret"}
	assert.Equal(t, "stored-token", enrollSwarmAgent(t.Context(), cfg, kvService, localSwarmNodeID))

	stillStored, _, err := kvService.Get(t.Context(), enrolledAgentTokenKey)
	require.NoError(t, err)
	assert.Equal(t, encrypted, stillStored)
}

func TestEnrollSwarmAgent_StopsWhenContextEnds(t *testing.T) {
	kvService := newEnrollmentTestKV(t)
	ctx, cancel := context.WithCancel(t.Context())
	cfg := &config.Config{ManagerApiUrl: "http://127.0.0.1:1", AgentEnrollmentToken: "enrollment-secret"}

	token := enrollSwarmAgent(ctx, cfg, kvService, func(context.Context) (string, error) {
		cancel()
		return "", errors.New("docker is unavailable")
	})

	assert.Empty(t, token)
}
