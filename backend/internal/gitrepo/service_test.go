package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

func setupGitRepositoryServiceTest(t *testing.T) *GitRepositoryService {
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
	settingsService, err := newSettingsServiceForTest(t, t.Context(), wrappedDB)
	require.NoError(t, err)

	eventService := event.NewEventService(wrappedDB, &config.Config{}, nil)

	return NewGitRepositoryService(wrappedDB, t.TempDir(), eventService, settingsService)
}

func newSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db, &config.Config{})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func createGitRepositoryServiceTestRepo(t *testing.T, svc *GitRepositoryService, req types.GitRepository) *GitRepository {
	t.Helper()

	repo, err := svc.CreateRepository(t.Context(), req, nil, types.Actor{
		ID:       "admin-1",
		Username: "admin",
	})
	require.NoError(t, err)
	mapped, err := mapping.MapOne[*GitRepository, types.GitRepository](repo)
	require.NoError(t, err)
	assert.Equal(t, req.Token != "", mapped.HasToken)
	assert.Equal(t, req.SSHKey != "", mapped.HasSshKey)
	repositories, _, err := svc.GetRepositoriesPaginated(t.Context(), pagination.QueryParams{Limit: -1})
	require.NoError(t, err)
	require.Len(t, repositories, 1)
	assert.Equal(t, mapped.HasToken, repositories[0].HasToken)
	assert.Equal(t, mapped.HasSshKey, repositories[0].HasSshKey)
	return repo
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenWouldBeReused(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	_, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL: "https://attacker.tld/repo.git",
	}, map[string]bool{"url": true}, types.Actor{})
	require.Error(t, err)

	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*types.FieldError](err)
	require.True(t, ok)
	assert.Equal(t, "token", fieldErr.Field)
	assert.Contains(t, err.Error(), "repository URL")

	stored, loadErr := svc.GetRepositoryByID(t.Context(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "https://github.com/acme/private.git", stored.URL)
	assert.Equal(t, repo.Token, stored.Token)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredSSHKeyWouldBeReused(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "infra-repo",
		URL:      "git@github.com:acme/private.git",
		AuthType: "ssh",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL: "git@attacker.tld:acme/private.git",
	}, map[string]bool{"url": true}, types.Actor{})
	require.Error(t, err)

	require.ErrorIs(t, err, common.ErrValidation)
	fieldErr, ok := errors.AsType[*types.FieldError](err)
	require.True(t, ok)
	assert.Equal(t, "sshKey", fieldErr.Field)
	assert.Contains(t, err.Error(), "repository URL")

	stored, loadErr := svc.GetRepositoryByID(t.Context(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "git@github.com:acme/private.git", stored.URL)
	assert.Equal(t, repo.SSHKey, stored.SSHKey)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenAndSSHKeyWouldBeReused(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "hybrid-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL: "https://attacker.tld/repo.git",
	}, map[string]bool{"url": true}, types.Actor{})
	require.Error(t, err)

	var apiErr *common.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, common.APIErrorCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Message, "repository URL")
	assert.Equal(t, map[string]any{"fields": []string{"sshKey", "token"}}, apiErr.Details)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsResupplied(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL:   "https://github.com/acme/private-rotated.git",
		Token: "ghp_new_token",
	}, map[string]bool{"url": true, "token": true}, types.Actor{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/private-rotated.git", updated.URL)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_new_token", decryptedToken)
	mapped, err := mapping.MapOne[*GitRepository, types.GitRepository](updated)
	require.NoError(t, err)
	assert.True(t, mapped.HasToken)
	assert.False(t, mapped.HasSshKey)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsCleared(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL:   "https://github.com/acme/public.git",
		Token: "",
	}, map[string]bool{"url": true, "token": true}, types.Actor{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/public.git", updated.URL)
	assert.Empty(t, updated.Token)
	mapped, err := mapping.MapOne[*GitRepository, types.GitRepository](updated)
	require.NoError(t, err)
	assert.False(t, mapped.HasToken)
	assert.False(t, mapped.HasSshKey)
}

func TestGitRepositoryService_UpdateRepository_AllowsSameURLWithoutCredentialResupply(t *testing.T) {
	svc := setupGitRepositoryServiceTest(t)
	repo := createGitRepositoryServiceTestRepo(t, svc, types.GitRepository{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(t.Context(), repo.ID, types.GitRepository{
		URL:      "https://github.com/acme/private.git",
		Username: "deploy-bot",
	}, map[string]bool{"url": true, "username": true}, types.Actor{})
	require.NoError(t, err)

	assert.Equal(t, "deploy-bot", updated.Username)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_old_token", decryptedToken)
}
