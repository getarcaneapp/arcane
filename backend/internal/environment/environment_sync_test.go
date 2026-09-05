package environment_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func setupSyncDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sync.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&environment.Environment{}, &registry.ContainerRegistry{}, &gitrepo.GitRepository{}, &s3domain.S3Destination{}, &activity.Activity{}, &activity.ActivityMessage{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	crypto.InitEncryption(&crypto.Config{EncryptionKey: "test-encryption-key-for-testing-32bytes-min", Environment: "test"})
	return &database.DB{DB: db}
}

func TestSyncResourcesToEnvironmentOutcomes(t *testing.T) {
	paths := []string{"/api/container-registries/sync", "/api/backups/s3/sync", "/api/git-repositories/sync"}
	cases := []struct {
		name     string
		failures map[string]string
		groups   string
	}{
		{name: "success"},
		{name: "registry failure", failures: map[string]string{paths[0]: "status"}, groups: "container registries"},
		{name: "S3 failure", failures: map[string]string{paths[1]: "status"}, groups: "S3 destinations"},
		{name: "repository failure", failures: map[string]string{paths[2]: "status"}, groups: "git repositories"},
		{name: "multiple failures", failures: map[string]string{paths[0]: "status", paths[2]: "status"}, groups: "container registries, git repositories"},
		{name: "all fail", failures: map[string]string{paths[0]: "status", paths[1]: "status", paths[2]: "status"}, groups: "container registries, S3 destinations, git repositories"},
		{name: "malformed response", failures: map[string]string{paths[0]: "malformed"}, groups: "container registries"},
		{name: "agent reports failure", failures: map[string]string{paths[0]: "false"}, groups: "container registries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupSyncDBInternal(t)
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.URL.Path)
				mu.Unlock()
				switch tc.failures[r.URL.Path] {
				case "status":
					http.Error(w, "private-agent-response", http.StatusUnauthorized)
				case "malformed":
					_, _ = w.Write([]byte("private-agent-response"))
				case "false":
					_, _ = w.Write([]byte(`{"success":false,"data":{"message":"private-agent-response"}}`))
				default:
					_, _ = w.Write([]byte(`{"success":true}`))
				}
			}))
			defer server.Close()
			require.NoError(t, db.Create(&environment.Environment{BaseModel: database.BaseModel{ID: "remote"}, Name: "Remote", ApiUrl: server.URL, AccessToken: new("agent-token"), Enabled: true}).Error)
			service := environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)
			activityService := activity.NewActivityService(db, nil)
			id, err := service.SyncResourcesToEnvironment(t.Context(), "remote", &common.User{ID: "operator", Username: "operator"}, activityService)
			require.NotEmpty(t, id)
			var recorded activity.Activity
			require.NoError(t, db.First(&recorded, "id = ?", id).Error)
			require.NotNil(t, recorded.EndedAt)
			require.NotNil(t, recorded.DurationMs)
			require.Equal(t, "remote", recorded.EnvironmentID)
			if tc.groups == "" {
				require.NoError(t, err)
				require.Equal(t, activitytypes.StatusSuccess, recorded.Status)
				require.Equal(t, "Environment synced successfully", recorded.LatestMessage)
				require.Nil(t, recorded.Error)
			} else {
				require.EqualError(t, err, "Failed to sync "+tc.groups+". Other resource groups may have synced successfully. Check the manager logs, correct the failed sync, and retry.")
				require.Equal(t, activitytypes.StatusFailed, recorded.Status)
				require.NotNil(t, recorded.Error)
				require.Equal(t, err.Error(), *recorded.Error)
				require.Equal(t, err.Error(), recorded.LatestMessage)
				require.NotContains(t, err.Error(), "private-agent-response")
				require.NotContains(t, err.Error(), "agent-token")
			}
			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, paths, calls)
		})
	}
}

func TestSyncCredentialDecryptionAbortsSnapshot(t *testing.T) {
	for _, kind := range []string{"registry token", "ECR secret", "repository token", "repository SSH key", "S3 secret"} {
		t.Run(kind, func(t *testing.T) {
			db := setupSyncDBInternal(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()
			require.NoError(t, db.Create(&environment.Environment{BaseModel: database.BaseModel{ID: "remote"}, ApiUrl: server.URL, AccessToken: new("agent-token")}).Error)
			service := environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)
			var syncResource func(context.Context, string) error
			switch kind {
			case "registry token", "ECR secret":
				row := registry.ContainerRegistry{BaseModel: database.BaseModel{ID: "bad-credential"}, URL: "registry.example.com", RegistryType: registry.RegistryTypeGeneric, Token: "invalid-ciphertext"}
				if kind == "ECR secret" {
					row.RegistryType = registry.RegistryTypeECR
					row.AWSSecretAccessKey = "invalid-ciphertext"
				}
				require.NoError(t, db.Create(&row).Error)
				syncResource = service.SyncRegistriesToEnvironment
			case "repository token", "repository SSH key":
				row := gitrepo.GitRepository{BaseModel: database.BaseModel{ID: "bad-credential"}, Name: "Repository", URL: "https://example.com/repo.git"}
				if kind == "repository token" {
					row.Token = "invalid-ciphertext"
				} else {
					row.SSHKey = "invalid-ciphertext"
				}
				require.NoError(t, db.Create(&row).Error)
				syncResource = service.SyncRepositoriesToEnvironment
			case "S3 secret":
				require.NoError(t, db.Create(&s3domain.S3Destination{BaseModel: database.BaseModel{ID: "bad-credential"}, Name: "Backup", SecretAccessKey: "invalid-ciphertext"}).Error)
				syncResource = service.SyncS3DestinationsToEnvironment
			}
			err := syncResource(t.Context(), "remote")
			require.Error(t, err)
			require.Contains(t, err.Error(), "bad-credential")
			require.NotNil(t, errors.Unwrap(err))
			require.Zero(t, calls.Load(), "an incomplete snapshot must never reach the agent")
		})
	}
}

func TestSyncRepositoriesPreservesEmptyCredentials(t *testing.T) {
	db := setupSyncDBInternal(t)
	require.NoError(t, db.Create(&gitrepo.GitRepository{BaseModel: database.BaseModel{ID: "public-repo"}, Name: "Public", URL: "https://example.com/public.git", AuthType: "none"}).Error)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload gitops.RepositorySyncRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if rows := payload.Repositories; len(rows) != 1 || rows[0].ID != "public-repo" {
			t.Errorf("missing public repository: %v", payload)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()
	require.NoError(t, db.Create(&environment.Environment{BaseModel: database.BaseModel{ID: "remote"}, ApiUrl: server.URL, AccessToken: new("token")}).Error)
	service := environment.NewEnvironmentService(db, server.Client(), nil, nil, nil, nil)
	require.NoError(t, service.SyncRepositoriesToEnvironment(t.Context(), "remote"))
}
