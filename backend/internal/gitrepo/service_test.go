package gitrepo

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"go.getarcane.app/sys/crypto"
)

func setupGitRepositoryServiceTestInternal(t *testing.T) (*GitRepositoryService, *database.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&GitRepository{},
		&event.Event{},
		&settings.SettingVariable{},
	))
	// DeleteRepository only counts gitops_syncs rows; the model lives in the
	// project domain, which this package cannot import from an in-package test.
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS gitops_syncs (id text PRIMARY KEY, repository_id text)").Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-git-repos-32bytes-min",
		Environment:   "test",
	})

	wrappedDB := &database.DB{DB: db}
	settingsService, err := newSettingsServiceForTestInternal(t, context.Background(), wrappedDB)
	require.NoError(t, err)

	eventService := event.NewEventService(wrappedDB, &config.Config{}, nil)

	return NewGitRepositoryService(wrappedDB, t.TempDir(), eventService, settingsService), wrappedDB
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "gitrepo-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "gitrepo-settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, executor, effects)
}

func createGitRepositoryServiceTestRepoInternal(t *testing.T, svc *GitRepositoryService, req CreateGitRepositoryRequest) *GitRepository {
	t.Helper()

	repo, err := svc.CreateRepository(context.Background(), req, common.User{
		BaseModel: database.BaseModel{ID: "admin-1"},
		Username:  "admin",
	})
	require.NoError(t, err)
	return repo
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL: new("https://attacker.tld/repo.git"),
	}, common.User{})
	require.Error(t, err)

	require.ErrorIs(t, err, common.ErrValidation)
	assert.Contains(t, errors.GetDetails(err), "token")
	assert.Contains(t, err.Error(), "repository URL")

	stored, loadErr := svc.GetRepositoryByID(context.Background(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "https://github.com/acme/private.git", stored.URL)
	assert.Equal(t, repo.Token, stored.Token)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredSSHKeyWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "infra-repo",
		URL:      "git@github.com:acme/private.git",
		AuthType: "ssh",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL: new("git@attacker.tld:acme/private.git"),
	}, common.User{})
	require.Error(t, err)

	require.ErrorIs(t, err, common.ErrValidation)
	assert.Contains(t, errors.GetDetails(err), "sshKey")
	assert.Contains(t, err.Error(), "repository URL")

	stored, loadErr := svc.GetRepositoryByID(context.Background(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "git@github.com:acme/private.git", stored.URL)
	assert.Equal(t, repo.SSHKey, stored.SSHKey)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenAndSSHKeyWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "hybrid-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL: new("https://attacker.tld/repo.git"),
	}, common.User{})
	require.Error(t, err)

	var apiErr *common.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, common.APIErrorCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Message, "repository URL")
	assert.Equal(t, map[string]any{"fields": []string{"sshKey", "token"}}, apiErr.Details)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsResupplied(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL:   new("https://github.com/acme/private-rotated.git"),
		Token: new("ghp_new_token"),
	}, common.User{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/private-rotated.git", updated.URL)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_new_token", decryptedToken)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsCleared(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL:   new("https://github.com/acme/public.git"),
		Token: new(""),
	}, common.User{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/public.git", updated.URL)
	assert.Empty(t, updated.Token)
}

func TestGitRepositoryService_UpdateRepository_AllowsSameURLWithoutCredentialResupply(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, UpdateGitRepositoryRequest{
		URL:      new("https://github.com/acme/private.git"),
		Username: new("deploy-bot"),
	}, common.User{})
	require.NoError(t, err)

	assert.Equal(t, "deploy-bot", updated.Username)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_old_token", decryptedToken)
}
