package build

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/image"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/builds/types"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

func TestBuildService_ResolveBuildRequest_PassesThroughLocalContext(t *testing.T) {
	contextDir := t.TempDir()
	svc := &BuildService{
		gitCloneFn: func(context.Context, string, string, git.AuthConfig) (string, error) {
			require.FailNow(t, "git clone should not run for local contexts")
			return "", nil
		},
	}

	req := types.BuildRequest{
		ContextDir: contextDir,
		Dockerfile: "Dockerfile",
	}

	resolvedReq, cleanup, err := svc.resolveBuildRequestInternal(t.Context(), req, nil, "")
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	assert.Equal(t, req, resolvedReq)
	require.NoError(t, cleanup())
}

func TestNewBuildService_NilRegistryServiceKeepsNilBuildRegistryProvider(t *testing.T) {
	svc := NewBuildService(nil, nil, nil, nil, nil, nil)
	require.NotNil(t, svc.builder)

	builderValue := reflect.ValueOf(svc.builder)
	require.Equal(t, reflect.Pointer, builderValue.Kind())

	registryProvider := builderValue.Elem().FieldByName("registryAuthProvider")
	require.True(t, registryProvider.IsValid())
	require.True(t, registryProvider.IsNil())
}

func TestBuildService_ResolveBuildRequest_ClonesRemoteGitContext(t *testing.T) {
	repoPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "docker", "app"), 0o755))

	cleanupCalled := false
	svc := &BuildService{
		gitProbeFn: func(context.Context, string, git.AuthConfig) error {
			require.FailNow(t, "git remote probe should not run for .git URLs")
			return nil
		},
		gitCloneFn: func(_ context.Context, repositoryURL, ref string, auth git.AuthConfig) (string, error) {
			assert.Equal(t, "https://github.com/getarcaneapp/arcane.git", repositoryURL)
			assert.Equal(t, "main", ref)
			assert.Equal(t, git.AuthConfig{}, auth)
			return repoPath, nil
		},
		gitCleanupFn: func(path string) error {
			cleanupCalled = true
			assert.Equal(t, repoPath, path)
			return nil
		},
	}

	req := types.BuildRequest{
		ContextDir: "https://github.com/getarcaneapp/arcane.git#main:docker/app",
		Dockerfile: "Dockerfile",
	}

	resolvedReq, cleanup, err := svc.resolveBuildRequestInternal(t.Context(), req, nil, "manual")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(repoPath, "docker", "app"), resolvedReq.ContextDir)
	require.NoError(t, cleanup())
	assert.True(t, cleanupCalled)
}

func TestBuildService_ResolveBuildRequest_ProbesAndClonesRemoteGitContextWithoutGitSuffix(t *testing.T) {
	repoPath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "docker", "app"), 0o755))

	probeCalled := false
	cloneCalled := false
	svc := &BuildService{
		gitProbeFn: func(_ context.Context, repositoryURL string, auth git.AuthConfig) error {
			probeCalled = true
			assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts", repositoryURL)
			assert.Equal(t, git.AuthConfig{}, auth)
			return nil
		},
		gitCloneFn: func(_ context.Context, repositoryURL, ref string, auth git.AuthConfig) (string, error) {
			cloneCalled = true
			assert.True(t, probeCalled)
			assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts", repositoryURL)
			assert.Equal(t, "main", ref)
			assert.Equal(t, git.AuthConfig{}, auth)
			return repoPath, nil
		},
		gitCleanupFn: func(string) error { return nil },
	}

	resolvedReq, cleanup, err := svc.resolveBuildRequestInternal(
		t.Context(),
		types.BuildRequest{ContextDir: "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app"},
		nil,
		"",
	)
	require.NoError(t, err)
	assert.True(t, probeCalled)
	assert.True(t, cloneCalled)
	assert.Equal(t, filepath.Join(repoPath, "docker", "app"), resolvedReq.ContextDir)
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_RequiresGitRepositoryServiceForRemoteContext(t *testing.T) {
	svc := &BuildService{}

	_, cleanup, err := svc.resolveBuildRequestInternal(
		t.Context(),
		types.BuildRequest{ContextDir: "https://github.com/getarcaneapp/arcane.git#main"},
		nil,
		"",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git repository service not available")
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_RejectsNonGitHTTPContextViaProbeFailure(t *testing.T) {
	svc := &BuildService{
		gitProbeFn: func(_ context.Context, repositoryURL string, auth git.AuthConfig) error {
			assert.Equal(t, "https://example.com/archive.tar.gz", repositoryURL)
			assert.Equal(t, git.AuthConfig{}, auth)
			return assert.AnError
		},
		gitCloneFn: func(context.Context, string, string, git.AuthConfig) (string, error) {
			require.FailNow(t, "git clone should not run when remote probe fails")
			return "", nil
		},
	}

	_, cleanup, err := svc.resolveBuildRequestInternal(
		t.Context(),
		types.BuildRequest{ContextDir: "https://example.com/archive.tar.gz"},
		nil,
		"",
	)
	require.Error(t, err)
	// HTTP(S) URLs without a .git suffix now reach the remote probe step instead of failing suffix validation.
	assert.Contains(t, err.Error(), "failed to verify remote git repository")
	assert.Contains(t, err.Error(), "archive.tar.gz")
	require.NoError(t, cleanup())
}

func TestBuildService_ResolveBuildRequest_UsesSavedGitCredentials(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&gitrepo.GitRepository{}))
	crypto.InitEncryption(&crypto.Config{
		Environment:   string(config.AppEnvironmentTest),
		EncryptionKey: "test-encryption-key-for-testing-32bytes-min",
	})
	db := &database.DB{DB: gormDB}

	repoService := gitrepo.NewGitRepositoryService(db, t.TempDir(), nil, nil)
	createTestGitRepository(t, db, gitrepo.GitRepository{
		Name:                   "private-http",
		URL:                    "https://github.com/getarcaneapp/private-build.git",
		AuthType:               "http",
		Username:               "builder",
		Token:                  encryptSecretForTest(t, "token-123"),
		SSHHostKeyVerification: "accept_new",
		Enabled:                true,
	})
	createTestGitRepository(t, db, gitrepo.GitRepository{
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
			gitProbeFn: func(_ context.Context, repositoryURL string, auth git.AuthConfig) error {
				assert.Equal(t, "https://github.com/getarcaneapp/private-build", repositoryURL)
				assert.Equal(t, "http", auth.AuthType)
				assert.Equal(t, "builder", auth.Username)
				assert.Equal(t, "token-123", auth.Token)
				return nil
			},
			gitCloneFn: func(_ context.Context, repositoryURL, _ string, auth git.AuthConfig) (string, error) {
				assert.Equal(t, "https://github.com/getarcaneapp/private-build", repositoryURL)
				assert.Equal(t, "http", auth.AuthType)
				assert.Equal(t, "builder", auth.Username)
				assert.Equal(t, "token-123", auth.Token)
				return t.TempDir(), nil
			},
			gitCleanupFn: func(string) error { return nil },
		}

		_, cleanup, resolveBuildRequestErr := svc.resolveBuildRequestInternal(
			t.Context(),
			types.BuildRequest{ContextDir: "https://github.com/getarcaneapp/private-build#main"},
			nil,
			"",
		)
		require.NoError(t, resolveBuildRequestErr)
		require.NoError(t, cleanup())
	})

	t.Run("ssh auth", func(t *testing.T) {
		svc := &BuildService{
			gitRepository: repoService,
			gitProbeFn: func(context.Context, string, git.AuthConfig) error {
				require.FailNow(t, "git remote probe should not run for ssh URLs")
				return nil
			},
			gitCloneFn: func(_ context.Context, _, _ string, auth git.AuthConfig) (string, error) {
				assert.Equal(t, "ssh", auth.AuthType)
				assert.Equal(t, "ssh-private-key", auth.SSHKey)
				assert.Equal(t, "strict", auth.SSHHostKeyVerification)
				return t.TempDir(), nil
			},
			gitCleanupFn: func(string) error { return nil },
		}

		_, cleanup, resolveBuildRequestErr := svc.resolveBuildRequestInternal(
			t.Context(),
			types.BuildRequest{ContextDir: "git@github.com:getarcaneapp/private-ssh.git#main"},
			nil,
			"",
		)
		require.NoError(t, resolveBuildRequestErr)
		require.NoError(t, cleanup())
	})
}

func TestBuildService_BuildImage_PreservesRemoteSourceInHistory(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)

	repoPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "Dockerfile"), []byte("FROM alpine:3.20\n"), 0o644))

	captured := types.BuildRequest{}
	svc := &BuildService{
		db: db,
		builder: testBuildRecorder{
			onBuild: func(req types.BuildRequest) {
				captured = req
			},
		},
		gitCloneFn: func(_ context.Context, _, _ string, _ git.AuthConfig) (string, error) {
			return repoPath, nil
		},
		gitCleanupFn: func(string) error { return nil },
	}

	req := types.BuildRequest{
		ContextDir: "https://github.com/getarcaneapp/arcane.git#main",
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
		Load:       true,
	}

	_, err = svc.BuildImage(t.Context(), "0", req, nil, "manual", nil)
	require.NoError(t, err)
	assert.Equal(t, repoPath, captured.ContextDir)

	var record ImageBuild
	require.NoError(t, db.WithContext(t.Context()).First(&record).Error)
	assert.Equal(t, req.ContextDir, record.ContextDir)
}

func TestBuildService_BuildImage_FailureRecordsHistoryAndEvent(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)

	buildErr := errors.New("BuildKit could not load the image with the docker exporter")
	svc := &BuildService{
		db:           db,
		eventService: event.NewEventService(db, nil, nil),
		builder: testBuildRecorder{
			err: buildErr,
		},
	}
	user := &usertypes.Actor{
		ID:       "user-1",
		Username: "tester",
	}

	req := types.BuildRequest{
		ContextDir: "/builds/demo",
		Dockerfile: "Dockerfile",
		Tags:       []string{"arcane.local/demo:test"},
		Provider:   "local",
		Load:       true,
	}

	_, err = svc.BuildImage(t.Context(), "0", req, nil, "web", user)
	require.ErrorIs(t, err, buildErr)

	var record ImageBuild
	require.NoError(t, db.WithContext(t.Context()).First(&record).Error)
	assert.Equal(t, ImageBuildStatusFailed, record.Status)
	require.NotNil(t, record.ErrorMessage)
	assert.Contains(t, *record.ErrorMessage, "docker exporter")

	var evt event.Event
	require.NoError(t, db.WithContext(t.Context()).First(&evt, "type = ?", event.EventTypeImageError).Error)
	assert.Equal(t, event.EventSeverityError, evt.Severity)
	require.NotNil(t, evt.ResourceName)
	assert.Equal(t, "arcane.local/demo:test", *evt.ResourceName)
	assert.Equal(t, "build", evt.Metadata["action"])
	assert.Equal(t, "local", evt.Metadata["provider"])
	assert.Equal(t, "/builds/demo", evt.Metadata["contextDir"])
	assert.Contains(t, evt.Metadata["error"], "docker exporter")
	require.NotEmpty(t, evt.Metadata["buildRecordId"])
	assert.Equal(t, record.ID, evt.Metadata["buildRecordId"])
}

func TestBuildService_BuildImage_FailureExporterErrorsAppearInOutputHistoryAndEvent(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)

	buildErr := &types.BuildKitImageExporterError{
		ProviderName: "depot",
		Err:          errors.New(`failed to solve: failed to solve: exporter "image" could not be found`),
	}
	progress := &bytes.Buffer{}

	svc := &BuildService{
		db:           db,
		eventService: event.NewEventService(db, nil, nil),
		builder: testBuildRecorder{
			err: buildErr,
			onProgress: func(_ types.BuildRequest, w io.Writer) {
				_, _ = w.Write([]byte(buildErr.Error()))
			},
		},
	}

	user := &usertypes.Actor{
		ID:       "user-2",
		Username: "registry-test",
	}

	req := types.BuildRequest{
		ContextDir: "/builds/demo",
		Dockerfile: "Dockerfile",
		Tags:       []string{"ghcr.io/getarcaneapp/arcane:test"},
		Provider:   "depot",
		Load:       true,
	}

	_, err = svc.BuildImage(t.Context(), "0", req, progress, "web", user)
	require.ErrorIs(t, err, buildErr)

	var record ImageBuild
	require.NoError(t, db.WithContext(t.Context()).First(&record).Error)
	assert.Equal(t, ImageBuildStatusFailed, record.Status)
	require.NotNil(t, record.Output)
	assert.Contains(t, *record.Output, buildErr.Error())
	require.NotNil(t, record.ErrorMessage)
	assert.Contains(t, *record.ErrorMessage, "exporter \"image\"")

	var evt event.Event
	require.NoError(t, db.WithContext(t.Context()).First(&evt, "type = ?", event.EventTypeImageError).Error)
	assert.Equal(t, event.EventSeverityError, evt.Severity)
	require.NotNil(t, evt.ResourceName)
	assert.Equal(t, "ghcr.io/getarcaneapp/arcane:test", *evt.ResourceName)
	assert.Equal(t, "build", evt.Metadata["action"])
	assert.Equal(t, "depot", evt.Metadata["provider"])
	assert.Equal(t, "/builds/demo", evt.Metadata["contextDir"])
	assert.Equal(t, buildErr.Error(), evt.Metadata["error"])
	// The progress writer is the wire transport: raw builder text arrives framed
	// as {"log":...} NDJSON lines.
	var frame map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(progress.String())), &frame))
	assert.Equal(t, buildErr.Error(), frame["log"])
	assert.Equal(t, buildErr.Error(), *record.ErrorMessage)
	assert.NotEmpty(t, evt.Metadata["buildRecordId"])
	assert.Equal(t, record.ID, evt.Metadata["buildRecordId"])
}

func TestSanitizeBuildContextForEventInternal_RedactsURLCredentials(t *testing.T) {
	assert.Equal(
		t,
		"https://redacted@github.com/zeroZshadow/secretproject.git#main:app",
		sanitizeBuildContextForEventInternal("https://myaccesstoken@github.com/zeroZshadow/secretproject.git#main:app"),
	)
	assert.Equal(
		t,
		"[unparseable URL]",
		sanitizeBuildContextForEventInternal("https://user:pass@[bad-host/repo.git"),
	)
	assert.Equal(t, "/builds/demo", sanitizeBuildContextForEventInternal("/builds/demo"))
}

type testBuildRecorder struct {
	onBuild    func(types.BuildRequest)
	onProgress func(types.BuildRequest, io.Writer)
	err        error
}

func (b testBuildRecorder) BuildImage(_ context.Context, req types.BuildRequest, writer io.Writer, _ string) (*types.BuildResult, error) {
	if b.onBuild != nil {
		b.onBuild(req)
	}
	if b.err != nil {
		if b.onProgress != nil {
			b.onProgress(req, writer)
		}
		return nil, b.err
	}
	return &types.BuildResult{Provider: "local"}, nil
}

func TestBuildService_ListImageBuilds_OmitsOutputColumn(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)
	svc := &BuildService{db: db}

	output := "large build output"
	require.NoError(t, db.WithContext(t.Context()).Create(&ImageBuild{
		ID:            "build-omit-output",
		EnvironmentID: "0",
		Status:        ImageBuildStatusSuccess,
		ContextDir:    "/ctx",
		Output:        &output,
	}).Error)

	records, _, err := svc.ListImageBuildsByEnvironmentPaginated(t.Context(), "0", pagination.QueryParams{
		Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Nil(t, records[0].Output)

	// The detail endpoint must still return the stored output.
	detail, err := svc.GetImageBuildByID(t.Context(), "0", "build-omit-output")
	require.NoError(t, err)
	require.NotNil(t, detail.Output)
	assert.Equal(t, output, *detail.Output)
}

func TestBuildService_PruneHistory_DeletesOnlyOldTerminalBuilds(t *testing.T) {
	db, err := setupBuildHistoryTestDB()
	require.NoError(t, err)
	svc := &BuildService{db: db}

	old := time.Now().Add(-40 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	for _, build := range []ImageBuild{
		{
			ID:            "old-success",
			CreatedAt:     old,
			EnvironmentID: "0",
			Status:        ImageBuildStatusSuccess,
			CompletedAt:   &old,
		},
		{
			ID:            "old-failed",
			CreatedAt:     old,
			EnvironmentID: "0",
			Status:        ImageBuildStatusFailed,
			CompletedAt:   &old,
		},
		{
			ID:            "old-running",
			CreatedAt:     old,
			EnvironmentID: "0",
			Status:        ImageBuildStatusRunning,
		},
		{
			ID:            "recent-success",
			CreatedAt:     recent,
			EnvironmentID: "0",
			Status:        ImageBuildStatusSuccess,
			CompletedAt:   &recent,
		},
	} {
		require.NoError(t, db.WithContext(t.Context()).Create(&build).Error)
	}

	deleted, err := svc.PruneHistory(t.Context(), 30)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	var remaining []string
	require.NoError(t, db.WithContext(t.Context()).Model(&ImageBuild{}).Order("id").Pluck("id", &remaining).Error)
	assert.Equal(t, []string{"old-running", "recent-success"}, remaining)

	deleted, err = svc.PruneHistory(t.Context(), 0)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

func setupBuildHistoryTestDB() (*database.DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if autoMigrateErr := db.AutoMigrate(&ImageBuild{}, &event.Event{}); autoMigrateErr != nil {
		return nil, autoMigrateErr
	}
	return &database.DB{DB: db}, nil
}

func createTestGitRepository(t *testing.T, db *database.DB, repository gitrepo.GitRepository) {
	t.Helper()
	require.NoError(t, db.WithContext(t.Context()).Create(&repository).Error)
}

func encryptSecretForTest(t *testing.T, value string) string {
	t.Helper()
	encrypted, err := crypto.Encrypt(value)
	require.NoError(t, err)
	return encrypted
}

func buildScopeContext(t *testing.T, target string, bodies map[string]any) (context.Context, *sync.Map) {
	t.Helper()
	var queries sync.Map
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, target, http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]
			queries.Store(id, r.URL.Query())
			body, ok := bodies[id]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.MarshalWrite(w, body)
		}),
		Resolve: func(_ context.Context, id string) (base.EnvironmentRef, error) {
			return base.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	}), &queries
}

func TestAggregateImageBuildsMergesEnvironments(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	bodies := map[string]any{
		"a": base.Paginated[image.BuildRecord]{Success: true, Data: []image.BuildRecord{
			{ID: "a1", EnvironmentID: "0", Status: "success", CreatedAt: start.Add(time.Hour)},
			{ID: "a2", EnvironmentID: "0", Status: "failed", CreatedAt: start.Add(3 * time.Hour)},
		}, Pagination: base.PaginationResponse{TotalItems: 2}},
		"b": base.Paginated[image.BuildRecord]{Success: true, Data: []image.BuildRecord{
			{ID: "b1", EnvironmentID: "0", Status: "success", CreatedAt: start.Add(2 * time.Hour)},
		}, Pagination: base.PaginationResponse{TotalItems: 1}},
	}
	rowLabels := func(items []image.BuildRecord) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.ID)
		}
		return out
	}
	svc := &BuildService{}

	t.Run("defaults to newest first across environments and pages", func(t *testing.T) {
		ctx, queries := buildScopeContext(t, "/api/environments/a,b,c/images/builds?start=0&limit=2&order=desc", bodies)
		body, err := svc.AggregateImageBuilds(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Order: pagination.SortDesc}, Params: pagination.Params{Limit: 2}})
		require.NoError(t, err)

		assert.True(t, body.Success)
		assert.Equal(t, []string{"a/a2", "b/b1"}, rowLabels(body.Data))
		assert.Equal(t, "Env b", body.Data[1].EnvironmentName)
		assert.Equal(t, int64(3), body.Pagination.TotalItems)
		assert.Equal(t, int64(3), body.Pagination.GrandTotalItems)
		assert.Equal(t, int64(2), body.Pagination.TotalPages)
		require.Len(t, body.FailedEnvironments, 1)
		assert.Equal(t, "c", body.FailedEnvironments[0].Environment.ID)
		assert.Equal(t, "environment offline", body.FailedEnvironments[0].Message)
		for _, id := range []string{"a", "b"} {
			query, ok := queries.Load(id)
			require.True(t, ok)
			assert.Equal(t, "0", query.(url.Values).Get("start"))
			assert.Equal(t, "-1", query.(url.Values).Get("limit"))
		}
	})

	t.Run("sorts by status", func(t *testing.T) {
		ctx, _ := buildScopeContext(t, "/api/environments/a,b,c/images/builds?sort=status&order=asc", bodies)
		body, err := svc.AggregateImageBuilds(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "status", Order: pagination.SortAsc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		assert.Equal(t, []string{"a/a2", "a/a1", "b/b1"}, rowLabels(body.Data))
	})

	t.Run("sorts by environment", func(t *testing.T) {
		ctx, _ := buildScopeContext(t, "/api/environments/a,b,c/images/builds?sort=environment&order=desc", bodies)
		body, err := svc.AggregateImageBuilds(ctx, pagination.QueryParams{SortParams: pagination.SortParams{Sort: "environment", Order: pagination.SortDesc}, Params: pagination.Params{Limit: -1}})
		require.NoError(t, err)
		assert.Equal(t, []string{"b/b1", "a/a1", "a/a2"}, rowLabels(body.Data))
	})

	t.Run("all environments failing returns empty data", func(t *testing.T) {
		ctx, _ := buildScopeContext(t, "/api/environments/a,b,c/images/builds", map[string]any{})
		body, err := svc.AggregateImageBuilds(ctx, pagination.QueryParams{Params: pagination.Params{Limit: 20}})
		require.NoError(t, err)
		assert.Empty(t, body.Data)
		assert.Len(t, body.FailedEnvironments, 3)
	})
}
