package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
)

func setupGitRepositoryServiceTestInternal(t *testing.T) (*GitRepositoryService, *database.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(glsqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.GitRepository{},
		&models.GitOpsSync{},
		&models.Event{},
		&models.SettingVariable{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-git-repos-32bytes-min",
		Environment:   "test",
	})

	wrappedDB := &database.DB{DB: db}
	settingsService, err := NewSettingsService(context.Background(), wrappedDB)
	require.NoError(t, err)

	eventService := NewEventService(wrappedDB, &config.Config{}, nil)

	return NewGitRepositoryService(wrappedDB, t.TempDir(), eventService, settingsService), wrappedDB
}

func createGitRepositoryServiceTestRepoInternal(t *testing.T, svc *GitRepositoryService, req models.CreateGitRepositoryRequest) *models.GitRepository {
	t.Helper()

	repo, err := svc.CreateRepository(context.Background(), req, models.User{
		BaseModel: models.BaseModel{ID: "admin-1"},
		Username:  "admin",
	})
	require.NoError(t, err)
	return repo
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL: new("https://attacker.tld/repo.git"),
	}, models.User{})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "token", validationErr.Field)
	assert.Contains(t, validationErr.Message, "re-supplying or clearing the token")

	stored, loadErr := svc.GetRepositoryByID(context.Background(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "https://github.com/acme/private.git", stored.URL)
	assert.Equal(t, repo.Token, stored.Token)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredSSHKeyWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "infra-repo",
		URL:      "git@github.com:acme/private.git",
		AuthType: "ssh",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL: new("git@attacker.tld:acme/private.git"),
	}, models.User{})
	require.Error(t, err)

	var validationErr *models.ValidationError
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "sshKey", validationErr.Field)
	assert.Contains(t, validationErr.Message, "re-supplying or clearing the SSH key")

	stored, loadErr := svc.GetRepositoryByID(context.Background(), repo.ID)
	require.NoError(t, loadErr)
	assert.Equal(t, "git@github.com:acme/private.git", stored.URL)
	assert.Equal(t, repo.SSHKey, stored.SSHKey)
}

func TestGitRepositoryService_UpdateRepository_RejectsURLChangeWhenStoredTokenAndSSHKeyWouldBeReused(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "hybrid-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
		SSHKey:   "-----BEGIN OPENSSH PRIVATE KEY-----\nkey-material\n-----END OPENSSH PRIVATE KEY-----",
	})

	_, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL: new("https://attacker.tld/repo.git"),
	}, models.User{})
	require.Error(t, err)

	var apiErr *models.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, models.APIErrorCodeValidationError, apiErr.Code)
	assert.Contains(t, apiErr.Message, "token and SSH key")
	assert.Equal(t, map[string]any{"fields": []string{"token", "sshKey"}}, apiErr.Details)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsResupplied(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL:   new("https://github.com/acme/private-rotated.git"),
		Token: new("ghp_new_token"),
	}, models.User{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/private-rotated.git", updated.URL)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_new_token", decryptedToken)
}

func TestGitRepositoryService_UpdateRepository_AllowsURLChangeWhenTokenIsCleared(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL:   new("https://github.com/acme/public.git"),
		Token: new(""),
	}, models.User{})
	require.NoError(t, err)

	assert.Equal(t, "https://github.com/acme/public.git", updated.URL)
	assert.Empty(t, updated.Token)
}

func TestGitRepositoryService_UpdateRepository_AllowsSameURLWithoutCredentialResupply(t *testing.T) {
	svc, _ := setupGitRepositoryServiceTestInternal(t)
	repo := createGitRepositoryServiceTestRepoInternal(t, svc, models.CreateGitRepositoryRequest{
		Name:     "prod-repo",
		URL:      "https://github.com/acme/private.git",
		AuthType: "http",
		Username: "deploy",
		Token:    "ghp_old_token",
	})

	updated, err := svc.UpdateRepository(context.Background(), repo.ID, models.UpdateGitRepositoryRequest{
		URL:      new("https://github.com/acme/private.git"),
		Username: new("deploy-bot"),
	}, models.User{})
	require.NoError(t, err)

	assert.Equal(t, "deploy-bot", updated.Username)
	decryptedToken, decryptErr := crypto.Decrypt(updated.Token)
	require.NoError(t, decryptErr)
	assert.Equal(t, "ghp_old_token", decryptedToken)
}
