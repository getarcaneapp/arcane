package handlers

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	envtypes "github.com/getarcaneapp/arcane/types/environment"
	"github.com/stretchr/testify/assert"
)

func TestEnvironmentMTLSAssetDownloadName(t *testing.T) {
	env := &models.Environment{Name: "Lab Server"}
	env.ID = "env-123"

	assert.Equal(t, "Lab-Server-env-123.pem", environmentMTLSAssetDownloadNameInternal(env, "agent.crt"))
	assert.Equal(t, "Lab-Server-env-123.key", environmentMTLSAssetDownloadNameInternal(env, "agent.key"))
	assert.Equal(t, "ca.crt", environmentMTLSAssetDownloadNameInternal(env, "ca.crt"))
}

func TestEnvironmentHandlerApplyEdgeRuntimeState(t *testing.T) {
	t.Run("leaves non-edge environments unchanged", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		env := envtypes.Environment{
			ID:     "0",
			Status: string(models.EnvironmentStatusOnline),
			IsEdge: false,
		}

		handler.applyEdgeRuntimeState(&env)

		assert.Equal(t, string(models.EnvironmentStatusOnline), env.Status)
		assert.Nil(t, env.EdgeTransport)
		assert.Nil(t, env.EdgeSecurityMode)
		assert.Nil(t, env.EdgeSessionID)
		assert.Nil(t, env.EdgeAgentInstance)
		assert.Nil(t, env.EdgeCapabilities)
		assert.Nil(t, env.Connected)
		assert.Nil(t, env.ConnectedAt)
		assert.Nil(t, env.LastHeartbeat)
	})

	t.Run("marks stale edge status offline when no live tunnel exists", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		envID := "env-edge-offline"
		edge.GetRegistry().Unregister(envID)
		t.Cleanup(func() { edge.GetRegistry().Unregister(envID) })

		env := envtypes.Environment{
			ID:     envID,
			Status: string(models.EnvironmentStatusOnline),
			IsEdge: true,
		}

		handler.applyEdgeRuntimeState(&env)

		assert.Equal(t, string(models.EnvironmentStatusOffline), env.Status)
		assert.Nil(t, env.EdgeTransport)
		assert.Nil(t, env.EdgeSecurityMode)
		assert.Nil(t, env.EdgeSessionID)
		assert.Nil(t, env.EdgeAgentInstance)
		assert.Nil(t, env.EdgeCapabilities)
		if assert.NotNil(t, env.Connected) {
			assert.False(t, *env.Connected)
		}
		assert.Nil(t, env.ConnectedAt)
		assert.Nil(t, env.LastHeartbeat)
	})

	t.Run("preserves pending edge environments until they connect", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		envID := "env-edge-pending"
		edge.GetRegistry().Unregister(envID)
		t.Cleanup(func() { edge.GetRegistry().Unregister(envID) })

		env := envtypes.Environment{
			ID:     envID,
			Status: string(models.EnvironmentStatusPending),
			IsEdge: true,
		}

		handler.applyEdgeRuntimeState(&env)

		assert.Equal(t, string(models.EnvironmentStatusPending), env.Status)
		assert.Nil(t, env.EdgeTransport)
		assert.Nil(t, env.EdgeSecurityMode)
		assert.Nil(t, env.EdgeSessionID)
		assert.Nil(t, env.EdgeAgentInstance)
		assert.Nil(t, env.EdgeCapabilities)
		if assert.NotNil(t, env.Connected) {
			assert.False(t, *env.Connected)
		}
		assert.Nil(t, env.ConnectedAt)
		assert.Nil(t, env.LastHeartbeat)
	})

	t.Run("reports live tunnel status as online", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		envID := "env-edge-live"
		edge.GetRegistry().Unregister(envID)
		t.Cleanup(func() { edge.GetRegistry().Unregister(envID) })

		tunnel := edge.NewAgentTunnelWithConn(envID, edge.NewGRPCManagerTunnelConn(nil))
		tunnel.SessionID = "session-live"
		tunnel.AgentInstance = "agent-live"
		tunnel.SecurityMode = "mtls"
		tunnel.Capabilities = []string{"container.list", "container.inspect"}
		edge.GetRegistry().Register(envID, tunnel)

		env := envtypes.Environment{
			ID:     envID,
			Status: string(models.EnvironmentStatusOffline),
			IsEdge: true,
		}

		handler.applyEdgeRuntimeState(&env)

		assert.Equal(t, string(models.EnvironmentStatusOnline), env.Status)
		if assert.NotNil(t, env.EdgeTransport) {
			assert.Equal(t, edge.EdgeTransportGRPC, *env.EdgeTransport)
		}
		if assert.NotNil(t, env.EdgeSecurityMode) {
			assert.Equal(t, "mtls", *env.EdgeSecurityMode)
		}
		if assert.NotNil(t, env.EdgeSessionID) {
			assert.Equal(t, "session-live", *env.EdgeSessionID)
		}
		if assert.NotNil(t, env.EdgeAgentInstance) {
			assert.Equal(t, "agent-live", *env.EdgeAgentInstance)
		}
		assert.Equal(t, []string{"container.list", "container.inspect"}, env.EdgeCapabilities)
		if assert.NotNil(t, env.Connected) {
			assert.True(t, *env.Connected)
		}
		assert.NotNil(t, env.ConnectedAt)
		assert.NotNil(t, env.LastHeartbeat)
	})

	t.Run("marks recently polled edge environments standby without a live tunnel", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		envID := "env-edge-polled"
		edge.GetRegistry().Unregister(envID)
		t.Cleanup(func() { edge.GetRegistry().Unregister(envID) })

		edge.GetPollRuntimeRegistry().Update(envID, edge.DefaultTunnelPollInterval, time.Now())

		env := envtypes.Environment{
			ID:     envID,
			Status: string(models.EnvironmentStatusOffline),
			IsEdge: true,
		}

		handler.applyEdgeRuntimeState(&env)

		assert.Equal(t, string(models.EnvironmentStatusStandby), env.Status)
		if assert.NotNil(t, env.Connected) {
			assert.False(t, *env.Connected)
		}
		assert.Nil(t, env.EdgeTransport)
		assert.Nil(t, env.EdgeSecurityMode)
		assert.Nil(t, env.EdgeSessionID)
		assert.Nil(t, env.EdgeAgentInstance)
		assert.Nil(t, env.EdgeCapabilities)
		assert.Nil(t, env.ConnectedAt)
		assert.Nil(t, env.LastHeartbeat)
		assert.NotNil(t, env.LastPollAt)
	})
}
