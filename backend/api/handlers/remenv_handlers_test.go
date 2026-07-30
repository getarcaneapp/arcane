package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamiddleware "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// adminTestContextInternal returns a context with a sudo PermissionSet attached,
// suitable for unit-testing handlers that gate via RequirePermission middleware.
func adminTestContextInternal() context.Context {
	return context.WithValue(context.Background(), humamiddleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
}

func setupRemoteHandlerEnvironmentServiceInternal(t *testing.T, server *httptest.Server) *services.EnvironmentService {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Environment{}, &models.S3Destination{}))

	now := time.Now()
	env := &models.Environment{
		BaseModel: models.BaseModel{
			ID:        "env-remote",
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:    "Remote Env",
		ApiUrl:  server.URL,
		Status:  string(models.EnvironmentStatusOnline),
		Enabled: true,
		IsEdge:  false,
	}
	require.NoError(t, db.WithContext(context.Background()).Create(env).Error)

	return services.NewEnvironmentService(&database.DB{DB: db}, server.Client(), nil, nil, nil, nil)
}

func TestProxyRemoteJSONInternal_MapsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request from remote", http.StatusBadRequest)
	}))
	defer server.Close()

	envSvc := setupRemoteHandlerEnvironmentServiceInternal(t, server)

	_, err := proxyRemoteJSONInternal[jobschedule.JobListResponse](context.Background(), envSvc, "env-remote", http.MethodGet, "/api/environments/0/jobs", nil)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), "bad request from remote")
}

func TestProxyRemoteJSONInternal_MapsDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"broken"`))
	}))
	defer server.Close()

	envSvc := setupRemoteHandlerEnvironmentServiceInternal(t, server)

	_, err := proxyRemoteJSONInternal[jobschedule.JobListResponse](context.Background(), envSvc, "env-remote", http.MethodGet, "/api/environments/0/jobs", nil)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), "failed to decode environment response")
}

func TestJobSchedulesHandler_ListJobs_RemoteSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/environments/0/jobs", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(jobschedule.JobListResponse{
			Jobs: []jobschedule.JobStatus{{ID: "job-1", Name: "Test Job"}},
		}))
	}))
	defer server.Close()

	handler := &JobSchedulesHandler{
		jobService:         &services.JobService{},
		environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server),
	}

	output, err := handler.ListJobs(context.Background(), &ListJobsInput{ID: "env-remote"})
	require.NoError(t, err)
	require.Len(t, output.Body.Jobs, 1)
	require.Equal(t, "job-1", output.Body.Jobs[0].ID)
}

func TestVolumeHandler_UpdateBackupPolicy_SyncsS3BeforeRemoteUpdate(t *testing.T) {
	var requestPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths = append(requestPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/s3-destinations/sync":
			require.Equal(t, http.MethodPost, r.Method)
			_, _ = w.Write([]byte(`{"success":true,"data":{"message":"ok"}}`))
		case "/api/environments/0/volumes/app-data/backup-policy":
			require.Equal(t, http.MethodPut, r.Method)
			var request volumetypes.UpdateBackupPolicies
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Len(t, request.Policies, 2)
			policy := request.Policies[0]
			require.True(t, policy.S3Enabled)
			require.True(t, policy.StopContainers)
			require.Equal(t, "s3-1", policy.S3DestinationID)
			require.NoError(t, json.NewEncoder(w).Encode(base.ApiResponse[volumetypes.BackupPolicyCollection]{
				Success: true,
				Data: volumetypes.BackupPolicyCollection{Policies: []volumetypes.BackupPolicy{
					{VolumeName: "app-data", S3Enabled: true, S3DestinationID: "s3-1"},
					{VolumeName: "app-data", LocalEnabled: true},
				}},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	handler := &VolumeHandler{environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server)}
	output, err := handler.UpdateBackupPolicy(context.Background(), &UpdateVolumeBackupPolicyInput{
		EnvironmentID: "env-remote",
		VolumeName:    "app-data",
		Body: volumetypes.UpdateBackupPolicies{Policies: []volumetypes.UpdateBackupPolicy{{
			Schedule:        "0 0 2 * * *",
			RetentionCount:  7,
			StopContainers:  true,
			LocalEnabled:    true,
			S3Enabled:       true,
			S3DestinationID: "s3-1",
		}, {Schedule: "0 0 14 * * *", RetentionCount: 2, LocalEnabled: true}}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"/api/s3-destinations/sync",
		"/api/environments/0/volumes/app-data/backup-policy",
	}, requestPaths)
	require.Equal(t, "s3-1", output.Body.Data.Policies[0].S3DestinationID)
}

func TestVolumeHandler_UpdateBackupPolicyCreatesActivity(t *testing.T) {
	db := setupActivityHandlerTestDBInternal(t)
	require.NoError(t, db.AutoMigrate(&models.VolumeBackupPolicy{}, &models.VolumeBackup{}))
	activityService := services.NewActivityService(db)
	volumeService := services.NewVolumeService(db, nil, nil, activityService, nil, nil, nil, nil, "", "test-encryption-key")
	handler := &VolumeHandler{volumeService: volumeService, activityService: activityService}

	output, err := handler.UpdateBackupPolicy(context.Background(), &UpdateVolumeBackupPolicyInput{
		EnvironmentID: "0",
		VolumeName:    "app-data",
		Body: volumetypes.UpdateBackupPolicies{Policies: []volumetypes.UpdateBackupPolicy{
			{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 7, LocalEnabled: true},
			{Enabled: true, Schedule: "0 0 14 * * *", RetentionCount: 2, LocalEnabled: true},
		}},
	})
	require.NoError(t, err)
	require.Len(t, output.Body.Data.Policies, 2)

	var activity models.Activity
	require.NoError(t, db.Where("resource_type = ?", "volume_backup_policy").First(&activity).Error)
	require.Equal(t, models.ActivityStatusSuccess, activity.Status)
	require.Equal(t, "update_volume_backup_policy", activity.Metadata["action"])
}

func TestSettingsHandler_GetPublicSettings_RemoteSuccess(t *testing.T) {
	expected := []settingstypes.PublicSetting{{Key: "theme", Type: "string", Value: "dark"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/environments/0/settings/public", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(expected))
	}))
	defer server.Close()

	handler := &SettingsHandler{
		settingsService:    &services.SettingsService{},
		environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server),
	}

	output, err := handler.GetPublicSettings(context.Background(), &GetPublicSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, expected, output.Body)
}

func TestSettingsHandler_GetSettings_RemoteFiltersNonAdminVisibility(t *testing.T) {
	remoteSettings := []settingstypes.PublicSetting{
		{Key: "dockerHost", Type: "string", Value: "unix:///var/run/docker.sock"},
		{Key: "baseServerUrl", Type: "string", Value: "https://manager.example"},
		{Key: "defaultShell", Type: "string", Value: "/bin/bash"},
		{Key: "futureAdminSetting", Type: "string", Value: "hidden"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/environments/0/settings", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(remoteSettings))
	}))
	defer server.Close()

	db := setupActivityHandlerTestDBInternal(t)
	settingsService, err := services.NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	handler := &SettingsHandler{
		settingsService:    settingsService,
		environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server),
	}
	ps := authz.NewPermissionSet()
	ps.AddEnv("env-remote", authz.PermSettingsRead)
	ctx := context.WithValue(context.Background(), humamiddleware.ContextKeyUserPermissions, ps)

	output, err := handler.GetSettings(ctx, &GetSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, []settingstypes.PublicSetting{remoteSettings[0]}, output.Body)
}

func TestSettingsHandler_GetSettings_RemotePreservesAdminResponse(t *testing.T) {
	remoteSettings := []settingstypes.PublicSetting{
		{Key: "baseServerUrl", Type: "string", Value: "https://manager.example"},
		{Key: "futureAdminSetting", Type: "string", Value: "visible-to-admin"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(remoteSettings))
	}))
	defer server.Close()

	db := setupActivityHandlerTestDBInternal(t)
	settingsService, err := services.NewSettingsService(context.Background(), db)
	require.NoError(t, err)
	handler := &SettingsHandler{
		settingsService:    settingsService,
		environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server),
	}

	output, err := handler.GetSettings(adminTestContextInternal(), &GetSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, remoteSettings, output.Body)
}

func TestVariableHandler_GetMaterializedVariables_RemoteSuccess(t *testing.T) {
	expected := base.ApiResponse[[]env.Variable]{
		Success: true,
		Data:    []env.Variable{{Key: "FOO", Value: "bar"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/environments/0/templates/variables", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(expected))
	}))
	defer server.Close()

	handler := &VariableHandler{
		variableService:    &services.VariableService{},
		environmentService: setupRemoteHandlerEnvironmentServiceInternal(t, server),
	}

	output, err := handler.GetMaterializedVariables(adminTestContextInternal(), &GetGlobalVariablesInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, expected, output.Body)
}
