package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	envtypes "github.com/getarcaneapp/arcane/types/v2/env"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"
)

func setupVariableServiceTest(t *testing.T) (*VariableService, *database.DB, string) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.GlobalVariable{},
		&models.Environment{},
		&models.KVEntry{},
		&models.SettingVariable{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
		Environment:   "test",
	})

	projectsDir := t.TempDir()
	dbWrap := &database.DB{DB: db}
	settingsSvc, err := NewSettingsService(context.Background(), dbWrap)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.UpdateSetting(context.Background(), "projectsDirectory", projectsDir))

	service := NewVariableService(dbWrap, nil, settingsSvc, NewKVService(dbWrap))
	return service, dbWrap, projectsDir
}

func createVariableTestEnvironment(t *testing.T, db *database.DB, id string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Environment{
		BaseModel: models.BaseModel{ID: id},
		Name:      "env-" + id,
		ApiUrl:    "http://env-" + id,
		Status:    "online",
		Enabled:   true,
	}).Error)
}

func TestCreateVariable_SecretEncryptedAtRestAndRedactedOnList(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()

	created, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key:             "API_TOKEN",
		Value:           "super-secret",
		IsSecret:        true,
		AllEnvironments: true,
	})
	require.NoError(t, err)
	require.Empty(t, created.Value, "mutation response must not echo the secret value")

	var stored models.GlobalVariable
	require.NoError(t, db.First(&stored, "id = ?", created.ID).Error)
	require.NotEqual(t, "super-secret", stored.Value, "secret must not be stored as plaintext")
	decrypted, err := crypto.Decrypt(stored.Value)
	require.NoError(t, err)
	require.Equal(t, "super-secret", decrypted)

	listed, err := service.ListVariables(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].IsSecret)
	require.Empty(t, listed[0].Value)
}

func TestResolveEffectiveVariables_EnvScopedOverridesAllEnv(t *testing.T) {
	service, db, _ := setupVariableServiceTest(t)
	ctx := context.Background()
	createVariableTestEnvironment(t, db, "env-a")
	createVariableTestEnvironment(t, db, "env-b")

	_, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "default", AllEnvironments: true,
	})
	require.NoError(t, err)
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "eu-west", EnvironmentIDs: []string{"env-a"},
	})
	require.NoError(t, err)

	// A second variable with the same key and an overlapping scope is rejected.
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "REGION", Value: "dup", EnvironmentIDs: []string{"env-a", "env-b"},
	})
	require.Error(t, err)

	// A specific scope without environments must not widen to all environments.
	_, err = service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "NO_SCOPE", Value: "x", AllEnvironments: false, EnvironmentIDs: []string{},
	})
	require.True(t, common.IsGlobalVariableScopeRequiredError(err), "expected GlobalVariableScopeRequiredError, got %v", err)

	forA, err := service.resolveEffectiveVariablesInternal(ctx, "env-a")
	require.NoError(t, err)
	require.Equal(t, []envtypes.Variable{{Key: "REGION", Value: "eu-west"}}, forA)

	forB, err := service.resolveEffectiveVariablesInternal(ctx, "env-b")
	require.NoError(t, err)
	require.Equal(t, []envtypes.Variable{{Key: "REGION", Value: "default"}}, forB)
}

func TestWriteLocalEnvFile_RejectsNewlineInjectionKey(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t)

	err := service.WriteLocalEnvFile(context.Background(), []envtypes.Variable{
		{Key: "BENIGN\nINJECTED", Value: "x"},
	})
	require.True(t, common.IsInvalidEnvKeyError(err), "expected InvalidEnvKeyError, got %v", err)

	_, statErr := os.Stat(filepath.Join(projectsDir, ".env.global"))
	require.True(t, os.IsNotExist(statErr), ".env.global must not be written on validation failure")
}

func TestSyncEnvironment_LocalWritesEnvGlobalFile(t *testing.T) {
	service, _, projectsDir := setupVariableServiceTest(t)
	ctx := context.Background()

	_, err := service.CreateVariable(ctx, envtypes.CreateGlobalVariableRequest{
		Key: "DB_PASSWORD", Value: "hunter2", IsSecret: true, AllEnvironments: true,
	})
	require.NoError(t, err)

	require.NoError(t, service.SyncEnvironment(ctx, "0"))

	content, err := os.ReadFile(filepath.Join(projectsDir, ".env.global"))
	require.NoError(t, err)
	require.Contains(t, string(content), "DB_PASSWORD=hunter2", "materialized file must contain the decrypted secret")

	statuses := service.SyncStatuses()
	require.Len(t, statuses, 1)
	require.Equal(t, "synced", statuses[0].Status)
}
