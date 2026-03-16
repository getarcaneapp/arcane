package services

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	buildgit "github.com/getarcaneapp/arcane/backend/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/types/image"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildService_GetRegistryAuthForHost_UsesDatabaseCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db, nil)}

	auth, err := svc.GetRegistryAuthForHost(context.Background(), "registry-1.docker.io")
	require.NoError(t, err)
	require.NotEmpty(t, auth)

	cfg := decodeRegistryAuth(t, auth)
	assert.Equal(t, "docker-user", cfg.Username)
	assert.Equal(t, "docker-token", cfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", cfg.ServerAddress)
}

func TestBuildService_GetRegistryAuthForImage_UsesHostLookup(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://ghcr.io", "gh-user", "gh-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db, nil)}

	auth, err := svc.GetRegistryAuthForImage(context.Background(), "ghcr.io/getarcaneapp/arcane:latest")
	require.NoError(t, err)
	require.NotEmpty(t, auth)

	cfg := decodeRegistryAuth(t, auth)
	assert.Equal(t, "gh-user", cfg.Username)
	assert.Equal(t, "gh-token", cfg.Password)
	assert.Equal(t, "ghcr.io", cfg.ServerAddress)
}

func TestBuildService_GetAllRegistryAuthConfigs_UsesDatabaseCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	createTestPullRegistry(t, db, "https://index.docker.io/v1/", "docker-user", "docker-token")

	svc := &BuildService{registryService: NewContainerRegistryService(db, nil)}

	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	require.NotNil(t, authConfigs)

	dockerCfg, ok := authConfigs["docker.io"]
	require.True(t, ok)
	assert.Equal(t, "docker-user", dockerCfg.Username)
	assert.Equal(t, "docker-token", dockerCfg.Password)
	assert.Equal(t, "https://index.docker.io/v1/", dockerCfg.ServerAddress)
	assert.Equal(t, dockerCfg, authConfigs["registry-1.docker.io"])
	assert.Equal(t, dockerCfg, authConfigs["index.docker.io"])
}

func TestBuildService_GetAllRegistryAuthConfigs_NoRegistryService(t *testing.T) {
	svc := &BuildService{}

	authConfigs, err := svc.GetAllRegistryAuthConfigs(context.Background())
	require.NoError(t, err)
	assert.Nil(t, authConfigs)
}

func TestBuildService_ResolveBuildRequest_PassesThroughLocalContext(t *testing.T) {
	contextDir := t.TempDir()
	svc := &BuildService{
		gitCloneFn: func(context.Context, string, string, buildgit.AuthConfig) (string, error) {
			t.Fatal("git clone should not run for local contexts")
			return "", nil
		},
	}

	req := image.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
	}

	resolvedReq, cleanup, err := svc.resolveBuildRequestInternal(context.Background(), req, nil, "")
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	assert.Equal(t, req, resolvedReq)
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_ClonesRemoteGitContext(t *testing.T) {
	repoPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "docker", "app"), 0o755))

	cleanupCalled := false
	svc := &BuildService{
		gitCloneFn: func(_ context.Context, repositoryURL, ref string, auth buildgit.AuthConfig) (string, error) {
			assert.Equal(t, "https://github.com/getarcaneapp/arcane.git", repositoryURL)
			assert.Equal(t, "main", ref)
			assert.Equal(t, buildgit.AuthConfig{}, auth)
			return repoPath, nil
		},
		gitCleanupFn: func(path string) error {
			cleanupCalled = true
			assert.Equal(t, repoPath, path)
			return nil
		},
	}

	req := image.BuildRequest{
		ContextDir: "https://github.com/getarcaneapp/arcane.git#main:docker/app",
		Dockerfile: "Dockerfile",
	}

	resolvedReq, cleanup, err := svc.resolveBuildRequestInternal(context.Background(), req, nil, "manual")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repoPath, "docker", "app"), resolvedReq.ContextDir)
	require.NoError(t, cleanup())
	assert.True(t, cleanupCalled)
}

func TestBuildService_ResolveBuildRequest_RejectsUnsupportedRemoteContext(t *testing.T) {
	svc := &BuildService{}

	_, cleanup, err := svc.resolveBuildRequestInternal(
		context.Background(),
		image.BuildRequest{ContextDir: "https://example.com/archive.tar.gz"},
		nil,
		"",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only git repository URLs are supported")
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_RequiresGitRepositoryServiceForRemoteContext(t *testing.T) {
	svc := &BuildService{}

	_, cleanup, err := svc.resolveBuildRequestInternal(
		context.Background(),
		image.BuildRequest{ContextDir: "https://github.com/getarcaneapp/arcane.git#main"},
		nil,
		"",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git repository service not available")
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_UsesSavedGitCredentials(t *testing.T) {
	_, db := setupImageServiceAuthTest(t)
	require.NoError(t, db.AutoMigrate(&models.GitRepository{}))

	repoService := NewGitRepositoryService(db, t.TempDir(), nil, nil)
	createTestGitRepository(t, db, models.GitRepository{
		Name:                   "private-http",
		URL:                    "https://github.com/getarcaneapp/private-build.git",
		AuthType:               "http",
		Username:               "builder",
		Token:                  encryptSecretForTest(t, "token-123"),
		SSHHostKeyVerification: "accept_new",
		Enabled:                true,
	})
	createTestGitRepository(t, db, models.GitRepository{
		Name:                   "private-ssh",
		URL:                    "git@github.com:getarcaneapp/private-ssh.git",
		AuthType:               "ssh",
		SSHKey:                 encryptSecretForTest(t, "ssh-private-key"),
		SSHHostKeyVerification: "strict",
		Enabled:                true,
	})

	t.Run("http auth", func(t *testing.T) {
		svc := &BuildService{
			gitRepository: repoService,
			gitCloneFn: func(_ context.Context, _ string, _ string, auth buildgit.AuthConfig) (string, error) {
				assert.Equal(t, "http", auth.AuthType)
				assert.Equal(t, "builder", auth.Username)
				assert.Equal(t, "token-123", auth.Token)
				return t.TempDir(), nil
			},
			gitCleanupFn: func(string) error { return nil },
		}

		_, cleanup, err := svc.resolveBuildRequestInternal(
			context.Background(),
			image.BuildRequest{ContextDir: "https://github.com/getarcaneapp/private-build.git#main"},
			nil,
			"",
		)
		require.NoError(t, err)
		require.NoError(t, cleanup())
	})

	t.Run("ssh auth", func(t *testing.T) {
		svc := &BuildService{
			gitRepository: repoService,
			gitCloneFn: func(_ context.Context, _ string, _ string, auth buildgit.AuthConfig) (string, error) {
				assert.Equal(t, "ssh", auth.AuthType)
				assert.Equal(t, "ssh-private-key", auth.SSHKey)
				assert.Equal(t, "strict", auth.SSHHostKeyVerification)
				return t.TempDir(), nil
			},
			gitCleanupFn: func(string) error { return nil },
		}

		_, cleanup, err := svc.resolveBuildRequestInternal(
			context.Background(),
			image.BuildRequest{ContextDir: "git@github.com:getarcaneapp/private-ssh.git#main"},
			nil,
			"",
		)
		require.NoError(t, err)
		require.NoError(t, cleanup())
	})
}

func TestBuildService_BuildImage_PreservesRemoteSourceInHistory(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)

	repoPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "Dockerfile"), []byte("FROM alpine:3.20\n"), 0o644))

	captured := image.BuildRequest{}
	svc := &BuildService{
		db: db,
		builder: testBuildRecorder{
			onBuild: func(req image.BuildRequest) {
				captured = req
			},
		},
		gitCloneFn: func(_ context.Context, _ string, _ string, _ buildgit.AuthConfig) (string, error) {
			return repoPath, nil
		},
		gitCleanupFn: func(string) error { return nil },
	}

	req := image.BuildRequest{
		ContextDir: "https://github.com/getarcaneapp/arcane.git#main",
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
		Load:       true,
	}

	_, err = svc.BuildImage(context.Background(), "0", req, nil, "manual", nil)
	require.NoError(t, err)
	assert.Equal(t, repoPath, captured.ContextDir)

	var record models.ImageBuild
	require.NoError(t, db.WithContext(context.Background()).First(&record).Error)
	assert.Equal(t, req.ContextDir, record.ContextDir)
}

type testBuildRecorder struct {
	onBuild func(image.BuildRequest)
}

func (b testBuildRecorder) BuildImage(_ context.Context, req image.BuildRequest, _ io.Writer, _ string) (*image.BuildResult, error) {
	if b.onBuild != nil {
		b.onBuild(req)
	}
	return &image.BuildResult{Provider: "local"}, nil
}

func setupBuildHistoryTestDB() (*database.DB, error) {
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.ImageBuild{}); err != nil {
		return nil, err
	}
	return &database.DB{DB: db}, nil
}

func createTestGitRepository(t *testing.T, db *database.DB, repository models.GitRepository) {
	t.Helper()
	require.NoError(t, db.WithContext(context.Background()).Create(&repository).Error)
}

func encryptSecretForTest(t *testing.T, value string) string {
	t.Helper()
	encrypted, err := crypto.Encrypt(value)
	require.NoError(t, err)
	return encrypted
}
