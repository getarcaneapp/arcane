package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	envtypes "github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentHandlerTestConnectionRequiresUpdatePermissionForCustomURLInternal(t *testing.T) {
	permissions := authz.NewPermissionSet()
	permissions.AddGlobal(authz.PermEnvironmentsRead)
	ctx := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, permissions)

	output, err := (&EnvironmentHandler{}).TestConnection(ctx, &TestConnectionInput{
		ID: "0",
		Body: &envtypes.TestConnectionRequest{
			ApiUrl: new("http://10.0.0.2:3553"),
		},
	})

	require.Nil(t, output)
	require.ErrorContains(t, err, "permission denied: "+authz.PermEnvironmentsUpdate)
}

func TestEnvironmentHandlerTestConnectionReturnsOpaqueBadRequestForCustomURLInternal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)

	db := setupActivityHandlerTestDBInternal(t)
	env := &models.Environment{Name: "Test", ApiUrl: "http://stored.example", Enabled: true}
	env.ID = "env-1"
	require.NoError(t, db.Create(env).Error)

	permissions := authz.NewPermissionSet()
	permissions.AddGlobal(authz.PermEnvironmentsUpdate)
	ctx := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, permissions)
	handler := &EnvironmentHandler{
		environmentService: services.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil),
	}

	output, err := handler.TestConnection(ctx, &TestConnectionInput{
		ID: "env-1",
		Body: &envtypes.TestConnectionRequest{
			ApiUrl: new(server.URL),
		},
	})

	require.NotNil(t, output)
	require.False(t, output.Body.Success)
	require.Nil(t, output.Body.Data.Message)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Equal(t, "Environment connection test failed", statusErr.Error())
}

func TestEnvironmentSecretDeploymentRoutesRequirePairPermission(t *testing.T) {
	testCases := []struct {
		name string
		path string
	}{
		{name: "deployment snippets", path: "/api/environments/env-1/deployment"},
		{name: "mtls bundle", path: "/api/environments/env-1/deployment/mtls/bundle"},
		{name: "mtls file", path: "/api/environments/env-1/deployment/mtls/agent.key"},
		{name: "edge ca", path: "/api/edge-mtls/ca"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name+" read denied", func(t *testing.T) {
			ps := authz.NewPermissionSet()
			ps.AddGlobal(authz.PermEnvironmentsRead)
			router, api := newPermissionGatingRouterInternal(t, ps)
			RegisterEnvironments(api, nil, nil, nil, nil, nil)

			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Contains(t, rec.Body.String(), authz.PermEnvironmentsPair)
		})

	}
}

func TestEnvironmentMTLSAssetDownloadName(t *testing.T) {
	env := &models.Environment{Name: "Lab Server"}
	env.ID = "env-123"

	assert.Equal(t, "Lab-Server-env-123.pem", environmentMTLSAssetDownloadNameInternal(env, "agent.crt"))
	assert.Equal(t, "Lab-Server-env-123.key", environmentMTLSAssetDownloadNameInternal(env, "agent.key"))
	assert.Equal(t, "ca.crt", environmentMTLSAssetDownloadNameInternal(env, "ca.crt"))
}

func TestEnvironmentMTLSAssetFileModeInternal(t *testing.T) {
	assert.Equal(t, os.FileMode(0o600), environmentMTLSAssetFileModeInternal(services.DeploymentSnippetFile{
		Name:        "agent.key",
		Permissions: "0600",
	}))
	assert.Equal(t, os.FileMode(0o644), environmentMTLSAssetFileModeInternal(services.DeploymentSnippetFile{
		Name: "ca.crt",
	}))
	assert.Equal(t, os.FileMode(0o600), environmentMTLSAssetFileModeInternal(services.DeploymentSnippetFile{
		Name: "agent.key",
	}))
}

func TestEnvironmentHandlerApplyEdgeRuntimeState(t *testing.T) {
	t.Run("leaves non-edge environments unchanged", func(t *testing.T) {
		handler := &EnvironmentHandler{}
		env := envtypes.Environment{
			ID:     "0",
			Status: string(models.EnvironmentStatusOnline),
			IsEdge: false,
		}

		handler.applyEdgeRuntimeStateInternal(&env)

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

		handler.applyEdgeRuntimeStateInternal(&env)

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

		handler.applyEdgeRuntimeStateInternal(&env)

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

		handler.applyEdgeRuntimeStateInternal(&env)

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

		handler.applyEdgeRuntimeStateInternal(&env)

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
