package enrollment

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kit.getarcane.app"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/types/v3"
)

type (
	enrollmentTestScheduler struct{}

	enrollmentTestHarness struct {
		db          *database.DB
		tunnels     *agent.TunnelRegistry
		environment *environment.EnvironmentService
		swarm       *swarm.SwarmService
		events      *event.EventService
		mtls        *agent.MTLS
		service     *EnrollmentService
	}
)

func (enrollmentTestScheduler) Submit(context.Context, types.SchedulerRequest) (types.SchedulerRun, error) {
	return types.SchedulerRun{}, nil
}

func (enrollmentTestScheduler) AddJob(context.Context, types.SchedulerJob) error {
	return nil
}

func (enrollmentTestScheduler) RemoveJob(context.Context, string) {}

func (enrollmentTestScheduler) HasJob(string) bool {
	return false
}

func newEnrollmentTestHarness(t *testing.T) enrollmentTestHarness {
	t.Helper()
	ctx := t.Context()
	db, err := database.Initialize(ctx, "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared", database.MigrationOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	settingsSvc, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    ctx,
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context())))
	})

	roleSvc := role.NewRoleService(db)
	tunnels := agent.NewTunnelRegistry()
	eventSvc := event.NewEventService(db, &config.Config{}, nil)
	envSvc := environment.NewEnvironmentService(environment.EnvironmentServiceParams{
		DB:              db,
		Tunnels:         tunnels,
		EventService:    eventSvc,
		SettingsService: settingsSvc,
		APIKeyService:   apikey.NewApiKeyService(db, user.NewUserService(db, roleSvc, session.RevokeAllUserSessionsExceptInDB), roleSvc),
		RemoteTransport: environment.NewRemoteTransport(db, settingsSvc, tunnels),
		Config:          &config.Config{},
		Ctx:             ctx,
		Scheduler:       enrollmentTestScheduler{},
	})

	swarmSvc := swarm.NewSwarmService(swarm.SwarmServiceParams{
		SettingsService:    settingsSvc,
		KVService:          kv.NewKVService(db),
		EnvironmentService: envSvc,
		Tunnels:            tunnels,
	})
	crypto.InitEncryption(crypto.Config{
		EncryptionKey: "test-encryption-key-for-agent-mtls-32bytes-min",
		Environment:   "test",
	})
	mtls := agent.NewMTLS(agent.MTLSConfig{
		Mode:      agent.MTLSModeRequired,
		AssetsDir: t.TempDir(),
	})
	return enrollmentTestHarness{
		db:          db,
		tunnels:     tunnels,
		environment: envSvc,
		swarm:       swarmSvc,
		events:      eventSvc,
		mtls:        mtls,
		service: NewEnrollmentService(EnrollmentServiceParams{
			EnvironmentService: envSvc,
			SwarmService:       swarmSvc,
			EventService:       eventSvc,
			MTLS:               mtls,
		}),
	}
}

func (h enrollmentTestHarness) createAgentEnvironment(t *testing.T, id, name, token string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, h.db.Create(&environment.Environment{
		BaseModel: database.BaseModel{
			ID:        id,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        name,
		ApiUrl:      "agent://" + id,
		Status:      string(environment.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: new(token),
	}).Error)
}

func (h enrollmentTestHarness) mtlsEvents(t *testing.T, envID string) []event.Event {
	t.Helper()
	var events []event.Event
	require.NoError(t, h.db.WithContext(t.Context()).
		Where("type LIKE ?", "environment.mtls.%").
		Where("environment_id = ? OR environment_id IS NULL", envID).
		Order("timestamp ASC").
		Find(&events).Error)
	return events
}

func (h enrollmentTestHarness) createToken(t *testing.T, maxUses *int) *types.EnrollmentTokenCreated {
	t.Helper()
	created, err := h.environment.CreateEnrollmentToken(t.Context(), environment.CreateEnrollmentTokenParams{
		Request: types.CreateEnrollmentTokenRequest{
			MaxUses: maxUses,
			Name:    "hosts",
		},
	})
	require.NoError(t, err)
	return created
}

func (h enrollmentTestHarness) token(t *testing.T, match func(types.EnrollmentToken) bool) types.EnrollmentToken {
	t.Helper()
	tokens, err := h.environment.ListEnrollmentTokens(t.Context())
	require.NoError(t, err)
	index := slices.IndexFunc(tokens, match)
	require.NotEqual(t, -1, index)
	return tokens[index]
}

func (h enrollmentTestHarness) useCount(t *testing.T, id string) int {
	t.Helper()
	return h.token(t, func(token types.EnrollmentToken) bool { return token.ID == id }).UseCount
}

func startEnrollmentTestAgent(t *testing.T, tunnels *agent.TunnelRegistry, envID string, handler http.Handler) {
	t.Helper()
	manager := echo.New()
	manager.GET("/api/tunnel/connect", agent.NewTunnelServer(agent.TunnelServerOptions{
		Registry: tunnels,
		Resolver: func(context.Context, string) (string, error) {
			return envID, nil
		},
		MTLS: agent.NewMTLS(agent.MTLSConfig{}),
	}).HandleConnect)
	managerServer := httptest.NewServer(manager)
	t.Cleanup(managerServer.Close)
	agentCtx, cancelAgent := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancelAgent()
		tunnels.Unregister(envID)
	})
	_, startErr := agent.NewTunnelClient(agent.TunnelClientConfig{
		Config: &agent.Config{
			MTLS:           agent.NewMTLS(agent.MTLSConfig{ManagerURL: managerServer.URL}),
			AgentEnabled:   true,
			AgentTransport: agent.TransportWebSocket,
			AgentToken:     "agent-token",
		},
		Handler: handler,
	}).Start(agentCtx)
	require.NoError(t, startErr)
	require.True(t, tunnels.WaitForActive(t.Context(), envID, 5*time.Second).IsPresent())
}

func TestEnrollmentService_EnrollPlainAgent(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	created := h.createToken(t, nil)

	_, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: "arc_etkn_wrong",
		Request:         types.AgentEnrollRequest{Hostname: "host-1"},
	})
	require.ErrorIs(t, err, common.ErrUnauthorized)

	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request:         types.AgentEnrollRequest{Hostname: "host-1"},
	})
	require.NoError(t, err)
	env, err := h.environment.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, "host-1", env.Name)
	assert.Equal(t, string(environment.EnvironmentStatusPending), env.Status)
	assert.True(t, env.Enabled)
	assert.Empty(t, kit.FromPtr(env.ParentEnvironmentID))
	assert.Equal(t, enrolled.Token, kit.FromPtr(env.AccessToken))
	assert.Equal(t, 1, h.useCount(t, created.ID))

	restarted, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		AgentToken:      enrolled.Token,
		Request:         types.AgentEnrollRequest{Hostname: "host-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, enrolled, restarted, "a restarted agent keeps its environment and token")
	assert.Equal(t, 1, h.useCount(t, created.ID), "a restart consumes no use")

	unmatched, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request: types.AgentEnrollRequest{
			Hostname:       "host-2",
			SwarmNodeID:    "node2",
			SwarmClusterID: "unknown-cluster",
		},
	})
	require.NoError(t, err)
	assert.NotEqual(t, enrolled.EnvironmentID, unmatched.EnvironmentID)
	unmatchedEnv, err := h.environment.GetEnvironmentByID(ctx, unmatched.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, "host-2", unmatchedEnv.Name)
	assert.Empty(t, kit.FromPtr(unmatchedEnv.SwarmNodeID), "a node of an unknown cluster enrolls as a plain environment")
	assert.Equal(t, 2, h.useCount(t, created.ID))

	require.NoError(t, h.environment.DeleteEnrollmentToken(ctx, created.ID, types.Actor{}))
	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		AgentToken:      enrolled.Token,
		Request:         types.AgentEnrollRequest{Hostname: "host-1"},
	})
	require.ErrorIs(t, err, common.ErrUnauthorized, "a revoked token enrolls nothing, even for a known agent")
}

func TestEnrollmentService_EnforcesMaxUses(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	created := h.createToken(t, new(1))

	_, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request:         types.AgentEnrollRequest{Hostname: "host-1"},
	})
	require.NoError(t, err)
	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request:         types.AgentEnrollRequest{Hostname: "host-2"},
	})
	require.ErrorIs(t, err, common.ErrUnauthorized)
	assert.Equal(t, 1, h.useCount(t, created.ID))
}

func TestEnrollmentService_BoundTokenEnrollsIntoItsEnvironment(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	agentCfg := agent.NewMTLS(agent.MTLSConfig{AppURL: "https://arcane.example.com"})
	env, err := h.environment.CreateEnvironment(ctx, &environment.Environment{
		Name:    "Production",
		Status:  string(environment.EnvironmentStatusPending),
		Enabled: true,
	}, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, env.AccessToken, "a new environment holds no API key until its agent enrolls")

	first, err := h.environment.MintDeploymentSnippets(ctx, env.ID, agentCfg)
	require.NoError(t, err)
	deployment, err := h.environment.MintDeploymentSnippets(ctx, env.ID, agentCfg)
	require.NoError(t, err)
	assert.Contains(t, deployment.DockerRun, "ARCANE_AGENT_TOKEN="+deployment.EnrollmentToken)

	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: first.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "prod-host"},
	})
	require.ErrorIs(t, err, common.ErrUnauthorized, "minting new snippets revokes the previous token")

	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: deployment.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "prod-host"},
	})
	require.NoError(t, err)
	assert.Equal(t, env.ID, enrolled.EnvironmentID)
	assert.True(t, strings.HasPrefix(enrolled.Token, "arc_"))
	assert.False(t, strings.HasPrefix(enrolled.Token, types.EnrollmentTokenPrefix))
	updated, err := h.environment.GetEnvironmentByID(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, enrolled.Token, kit.FromPtr(updated.AccessToken))
	assert.NotEmpty(t, kit.FromPtr(updated.ApiKeyID))
	assert.Equal(t, "Production", updated.Name)

	retried, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: deployment.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "prod-host"},
	})
	require.NoError(t, err, "a lost enrollment reply can be retried until the agent connects")
	assert.Equal(t, enrolled, retried)

	require.NoError(t, h.environment.GetDB().WithContext(ctx).Model(&environment.Environment{}).
		Where("id = ?", env.ID).
		Update("status", string(environment.EnvironmentStatusOnline)).Error)
	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: deployment.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "other-host"},
	})
	require.ErrorIs(t, err, common.ErrUnauthorized, "a deployment token is single-use once its agent connected")

	again, err := h.environment.MintDeploymentSnippets(ctx, env.ID, agentCfg)
	require.NoError(t, err)
	rotated, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: again.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "prod-host"},
	})
	require.NoError(t, err)
	assert.Equal(t, env.ID, rotated.EnvironmentID)
	assert.NotEqual(t, enrolled.Token, rotated.Token, "re-enrolling a bound environment rotates its API key")
}

func TestEnrollmentService_RefundsUseWhenEnrollmentFails(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	env, err := h.environment.CreateEnvironment(ctx, &environment.Environment{
		Name:    "Gone",
		Status:  string(environment.EnvironmentStatusPending),
		Enabled: true,
	}, nil, nil)
	require.NoError(t, err)
	deployment, err := h.environment.MintDeploymentSnippets(ctx, env.ID, agent.NewMTLS(agent.MTLSConfig{AppURL: "https://arcane.example.com"}))
	require.NoError(t, err)
	require.NoError(t, h.db.WithContext(ctx).Exec("DELETE FROM environments WHERE id = ?", env.ID).Error)

	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: deployment.EnrollmentToken,
		Request:         types.AgentEnrollRequest{Hostname: "host"},
	})
	require.Error(t, err)

	token := h.token(t, func(token types.EnrollmentToken) bool { return kit.FromPtr(token.EnvironmentID) == env.ID })
	assert.Zero(t, token.UseCount, "a failed enrollment gives its use back")
}

func TestEnrollmentService_EnrollSwarmNodeOfKnownCluster(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	now := time.Now()
	require.NoError(t, h.db.Create(&environment.Environment{
		BaseModel: database.BaseModel{
			ID:        "enroll-manager",
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        "manager",
		ApiUrl:      "agent://enroll-manager",
		Status:      string(environment.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: new("manager-token"),
	}).Error)
	startEnrollmentTestAgent(t, h.tunnels, "enroll-manager", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/environments/0/swarm/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"enabled":true,"clusterId":"cluster1"}}`))
		case "/api/environments/0/swarm/nodes/workernode1":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"workernode1","hostname":"worker-1","role":"worker","agent":{"state":"none"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	created := h.createToken(t, nil)

	request := types.AgentEnrollRequest{
		Hostname:       "worker-1",
		SwarmNodeID:    "workernode1",
		SwarmClusterID: "cluster1",
	}
	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request:         request,
	})
	require.NoError(t, err)
	env, err := h.environment.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, "enroll-manager", kit.FromPtr(env.ParentEnvironmentID))
	assert.Equal(t, "workernode1", kit.FromPtr(env.SwarmNodeID))
	assert.Equal(t, enrolled.Token, kit.FromPtr(env.AccessToken))

	restarted, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		AgentToken:      enrolled.Token,
		Request:         request,
	})
	require.NoError(t, err)
	assert.Equal(t, enrolled, restarted)

	_, err = h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request:         request,
	})
	require.ErrorIs(t, err, common.ErrConflict, "an agent without the node environment's key cannot take it over")
	kept, err := h.environment.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Equal(t, enrolled.Token, kit.FromPtr(kept.AccessToken))
	assert.Equal(t, 1, h.useCount(t, created.ID), "a refused enrollment gives its use back")
}

func TestEnrollmentService_SwarmNodeDeploymentTokenEnrollsIntoNodeEnvironment(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	env, err := h.environment.EnsureSwarmNodeAgentEnvironment(ctx, environment.SwarmNodeAgentEnvironmentParams{
		ParentEnvironmentID: "swarm-manager",
		NodeID:              "nodedeploy1",
		Hostname:            "worker-2",
		UserID:              types.SystemUser.ID,
		Username:            types.SystemUser.Username,
	})
	require.NoError(t, err)

	deployment, err := h.environment.MintDeploymentSnippets(ctx, env.ID, agent.NewMTLS(agent.MTLSConfig{AppURL: "https://arcane.example.com"}))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(deployment.EnrollmentToken, types.EnrollmentTokenPrefix))
	assert.Contains(t, deployment.DockerRun, "ARCANE_AGENT_TOKEN="+deployment.EnrollmentToken)

	token := h.token(t, func(token types.EnrollmentToken) bool { return kit.FromPtr(token.EnvironmentID) == env.ID })
	assert.Equal(t, 1, kit.FromPtr(token.MaxUses))
	var apiKeyCount int64
	require.NoError(t, h.db.WithContext(ctx).Model(&apikey.ApiKey{}).Where("environment_id = ?", env.ID).Count(&apiKeyCount).Error)
	assert.Zero(t, apiKeyCount, "generating a node deployment creates no API key")

	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: deployment.EnrollmentToken,
		Request: types.AgentEnrollRequest{
			Hostname:    "worker-2",
			SwarmNodeID: "nodedeploy1",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, env.ID, enrolled.EnvironmentID)
	enrolledEnv, err := h.environment.GetEnvironmentByID(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, "swarm-manager", kit.FromPtr(enrolledEnv.ParentEnvironmentID))
	assert.Equal(t, "nodedeploy1", kit.FromPtr(enrolledEnv.SwarmNodeID))
	assert.Equal(t, enrolled.Token, kit.FromPtr(enrolledEnv.AccessToken))
	assert.NotEmpty(t, kit.FromPtr(enrolledEnv.ApiKeyID))
}

func TestEnrollmentService_UnreachableManagerIsRetryable(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	now := time.Now()
	require.NoError(t, h.db.Create(&environment.Environment{
		BaseModel: database.BaseModel{
			ID:        "flaky-manager",
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        "flaky",
		ApiUrl:      "agent://flaky-manager",
		Status:      string(environment.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: new("flaky-token"),
	}).Error)
	startEnrollmentTestAgent(t, h.tunnels, "flaky-manager", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "docker is busy", http.StatusInternalServerError)
	}))
	created := h.createToken(t, nil)

	_, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request: types.AgentEnrollRequest{
			Hostname:       "worker-3",
			SwarmNodeID:    "workernode3",
			SwarmClusterID: "cluster3",
		},
	})
	require.ErrorIs(t, err, common.ErrUnavailable, "a failed probe is retried instead of enrolling a plain environment")
	assert.Zero(t, h.useCount(t, created.ID))
	environments, err := h.environment.ListActiveRemoteEnvironments(ctx)
	require.NoError(t, err)
	assert.Len(t, environments, 1, "no environment is created while a manager is unreachable")
}

func TestEnrollmentService_ManagerRejectingSwarmProbeIsNotTheCluster(t *testing.T) {
	ctx := t.Context()
	h := newEnrollmentTestHarness(t)
	h.createAgentEnvironment(t, "older-agent", "older", "older-token")
	startEnrollmentTestAgent(t, h.tunnels, "older-agent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	created := h.createToken(t, nil)

	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: created.Token,
		Request: types.AgentEnrollRequest{
			Hostname:       "worker-4",
			SwarmNodeID:    "workernode4",
			SwarmClusterID: "cluster4",
		},
	})
	require.NoError(t, err, "an agent without the swarm status route is not the cluster's manager")
	env, err := h.environment.GetEnvironmentByID(ctx, enrolled.EnvironmentID)
	require.NoError(t, err)
	assert.Empty(t, kit.FromPtr(env.ParentEnvironmentID))
}

func TestEnrollmentService_EnrollMTLS_Unavailable(t *testing.T) {
	h := newEnrollmentTestHarness(t)
	h.createAgentEnvironment(t, "env-disabled", "Disabled", "disabled-token")
	service := NewEnrollmentService(EnrollmentServiceParams{
		EnvironmentService: h.environment,
		SwarmService:       h.swarm,
		EventService:       h.events,
		MTLS:               agent.NewMTLS(agent.MTLSConfig{}),
	})

	_, err := service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "disabled-token",
		RemoteAddr: "192.0.2.1",
	})
	require.ErrorIs(t, err, common.ErrNotFound)
	assert.Equal(t, "agent mTLS enrollment is not available", err.Error())
}

func TestEnrollmentService_EnrollMTLS_Unauthorized(t *testing.T) {
	h := newEnrollmentTestHarness(t)

	_, err := h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		RemoteAddr: "192.0.2.1",
	})
	require.ErrorIs(t, err, common.ErrUnauthorized)
	assert.Equal(t, "agent token required", err.Error())

	_, err = h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "wrong-token",
		RemoteAddr: "192.0.2.1",
	})
	require.ErrorIs(t, err, common.ErrUnauthorized)
	assert.Equal(t, "invalid agent token", err.Error())
}

func TestEnrollmentService_EnrollMTLS_IssuesClientAssets(t *testing.T) {
	h := newEnrollmentTestHarness(t)
	h.createAgentEnvironment(t, "env-mtls", "Lab Server", "valid-token")

	files, err := h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "valid-token",
		RemoteAddr: "192.0.2.1",
	})
	require.NoError(t, err)
	require.Len(t, files, 3)
	assert.Equal(t, "ca.crt", files[0].Name)
	assert.Contains(t, files[0].Content, "BEGIN CERTIFICATE")
	assert.Equal(t, "agent.key", files[2].Name)
	assert.Contains(t, files[2].Content, "BEGIN PRIVATE KEY")

	certBlock, _ := pem.Decode([]byte(files[1].Content))
	require.NotNil(t, certBlock)
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	assert.Equal(t, "Lab-Server-env-mtls", cert.Subject.CommonName)

	events := h.mtlsEvents(t, "env-mtls")
	require.Len(t, events, 3)
	assert.Equal(t, event.EventTypeEnvironmentMTLSEnroll, events[0].Type)
	assert.Equal(t, event.EventSeverityInfo, events[0].Severity)
	assert.Equal(t, "Lab Server", kit.FromPtr(events[0].ResourceName))
	assert.Equal(t, event.EventTypeEnvironmentMTLSCAGenerated, events[1].Type)
	assert.Equal(t, event.EventTypeEnvironmentMTLSCertIssued, events[2].Type)
	assert.Equal(t, "192.0.2.1", events[2].Metadata["remoteAddr"])
}

func TestEnrollmentService_EnrollMTLS_ServesCachedAssetsDuringCooldown(t *testing.T) {
	h := newEnrollmentTestHarness(t)
	h.createAgentEnvironment(t, "env-repeat", "", "valid-token")

	first, err := h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "valid-token",
		RemoteAddr: "192.0.2.1",
	})
	require.NoError(t, err)
	require.Len(t, first, 3)

	second, err := h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "valid-token",
		RemoteAddr: "192.0.2.1",
	})
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Len(t, h.mtlsEvents(t, "env-repeat"), 3, "a cached enrollment records no events")
}

func TestEnrollmentService_EnrollMTLS_AllowsRepeatAfterCooldownAndMarksReenrollment(t *testing.T) {
	h := newEnrollmentTestHarness(t)
	h.createAgentEnvironment(t, "env-repeat-old", "", "valid-token")

	_, err := h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "valid-token",
		RemoteAddr: "192.0.2.1",
	})
	require.NoError(t, err)

	markerPath := filepath.Join(h.mtls.AssetsDir, "clients", "env-repeat-old", ".enrolled")
	oldEnrollment := time.Now().Add(-16*time.Minute).UTC().Format(time.RFC3339Nano) + "\n"
	require.NoError(t, os.WriteFile(markerPath, []byte(oldEnrollment), 0o600))

	_, err = h.service.EnrollMTLS(t.Context(), EnrollMTLSParams{
		AgentToken: "valid-token",
		RemoteAddr: "192.0.2.1",
	})
	require.NoError(t, err)

	events := h.mtlsEvents(t, "env-repeat-old")
	require.Len(t, events, 4)
	assert.Equal(t, event.EventTypeEnvironmentMTLSEnroll, events[3].Type)
	assert.Equal(t, event.EventSeverityWarning, events[3].Severity)
	assert.Equal(t, true, events[3].Metadata["reenrollment"])
}
