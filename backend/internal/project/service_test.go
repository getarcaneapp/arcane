package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	composeapi "github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/libtnb/sqlite"
	dockerauthconfig "github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/opencontainers/go-digest"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	buildtypes "go.getarcane.app/builds/types"
	"go.getarcane.app/updater/labels"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type testBuildBuilder struct {
	err error
}

func (b testBuildBuilder) BuildImage(_ context.Context, _ string, _ buildtypes.BuildRequest, _ io.Writer, _ string, _ *common.User) (*buildtypes.BuildResult, error) {
	if b.err != nil {
		return nil, b.err
	}
	return &buildtypes.BuildResult{Provider: "local"}, nil
}

func (testBuildBuilder) BuildSettings() buildtypes.BuildSettings {
	return buildtypes.BuildSettings{}
}

var _ buildServiceInternal = testBuildBuilder{}

func setupProjectTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Project{}, &ProjectTag{}, &settings.SettingVariable{}, &imageupdate.ImageUpdateRecord{}, &event.Event{}))
	return &database.DB{DB: db}
}

func TestUpdateProjectWorkspaceRejectsInvalidManifestBeforeProjectLookup(t *testing.T) {
	service := &ProjectService{}

	_, err := service.UpdateProjectWorkspace(context.Background(), "missing", projecttypes.WorkspaceUpdateManifest{}, nil, common.User{})
	require.ErrorIs(t, err, common.ErrProjectWorkspaceBadRequest)
	require.ErrorContains(t, err, "revision")
}

func TestValidateWorkspaceChangesAgainstGitOps(t *testing.T) {
	owned := map[string]struct{}{"compose.yaml": {}, "conf/app.conf": {}}

	err := validateWorkspaceChangesAgainstGitOpsInternal([]projecttypes.WorkspaceFileChange{
		{Operation: projecttypes.FileOpUpdateFile, RelativePath: "conf/app.conf"},
	}, owned)
	require.ErrorIs(t, err, common.ErrProjectWorkspaceForbidden)
	require.ErrorContains(t, err, "conf/app.conf")

	// Recursively deleting a folder that still holds a sync-owned file must
	// also be rejected.
	err = validateWorkspaceChangesAgainstGitOpsInternal([]projecttypes.WorkspaceFileChange{
		{Operation: projecttypes.FileOpDelete, RelativePath: "conf", Recursive: true},
	}, owned)
	require.ErrorIs(t, err, common.ErrProjectWorkspaceForbidden)

	// The operator overlay (e.g. a secret env file the repo cannot carry)
	// stays editable.
	err = validateWorkspaceChangesAgainstGitOpsInternal([]projecttypes.WorkspaceFileChange{
		{Operation: projecttypes.FileOpCreateFile, RelativePath: "app.env"},
	}, owned)
	require.NoError(t, err)
}

func newSettingsServiceForTestInternal(t *testing.T, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "project-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "project-settings-effects-test", t.Name(), 3)
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

func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func decodeRegistryAuthInternal(t *testing.T, encoded string) dockerregistry.AuthConfig {
	t.Helper()
	cfg, err := dockerauthconfig.Decode(encoded)
	require.NoError(t, err)
	return *cfg
}

func newImagePullServerWithObserverInternal(t *testing.T, inspectByRef map[string]dockertypesimage.InspectResponse, onPull func(fullRef string, authHeader string)) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/create"):
			fullRef := strings.TrimSpace(r.URL.Query().Get("fromImage"))
			tag := strings.TrimSpace(r.URL.Query().Get("tag"))
			if fullRef != "" && tag != "" {
				lastSlash := strings.LastIndex(fullRef, "/")
				lastColon := strings.LastIndex(fullRef, ":")
				if lastColon <= lastSlash {
					fullRef += ":" + tag
				}
			}
			if onPull != nil {
				onPull(fullRef, strings.TrimSpace(r.Header.Get("X-Registry-Auth")))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "Pulled", "id": fullRef})
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			path := r.URL.Path
			imagePathIndex := strings.Index(path, "/images/")
			if !assert.NotEqual(t, -1, imagePathIndex) {
				return
			}
			encodedRef := strings.TrimSuffix(path[imagePathIndex+len("/images/"):], "/json")
			imageRef, err := url.PathUnescape(encodedRef)
			if !assert.NoError(t, err) {
				return
			}
			inspect, ok := inspectByRef[imageRef]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(inspect)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProjectService_RefreshProjectImageRefs_PersistsBuildMetadata(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(`services:
  explicit:
    build: .
    image: test2:latest
  worker:
    build:
      context: .
      dockerfile: Dockerfile
  regular:
    image: postgres:17
`), 0o644))

	proj := &Project{
		ID:   "project-build-image-refs",
		Name: "demo",
		Path: projectPath,
	}
	require.NoError(t, db.Create(proj).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	svc.refreshProjectImageRefsInternal(ctx, proj)

	var saved Project
	require.NoError(t, db.First(&saved, "id = ?", proj.ID).Error)
	assert.ElementsMatch(t, []string{"test2:latest", "postgres:17"}, projects.ParseImageRefsJSON(saved.ImageRefsJSON))
	require.NotNil(t, saved.BuildImageRefsJSON)
	assert.ElementsMatch(t, []string{"test2:latest", "demo-worker"}, projects.ParseImageRefsJSON(*saved.BuildImageRefsJSON))
}

func TestProjectService_BackfillProjectImageRefs_RetriesOnlyMissingMetadata(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	validPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(validPath, "compose.yaml"), []byte(`services:
  app:
    build: .
    image: local-app:latest
`), 0o644))
	invalidPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(invalidPath, "compose.yaml"), []byte("services: [\n"), 0o644))
	regularPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(regularPath, "compose.yaml"), []byte(`services:
  app:
    image: nginx:latest
`), 0o644))

	valid := &Project{ID: "backfill-valid", Name: "valid", Path: validPath}
	invalid := &Project{ID: "backfill-invalid", Name: "invalid", Path: invalidPath}
	regular := &Project{ID: "backfill-regular", Name: "regular", Path: regularPath}
	require.NoError(t, db.Create(valid).Error)
	require.NoError(t, db.Create(invalid).Error)
	require.NoError(t, db.Create(regular).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	count, err := svc.BackfillProjectImageRefs(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count)

	var savedValid Project
	require.NoError(t, db.First(&savedValid, "id = ?", valid.ID).Error)
	require.NotNil(t, savedValid.BuildImageRefsJSON)
	assert.Equal(t, []string{"local-app:latest"}, projects.ParseImageRefsJSON(*savedValid.BuildImageRefsJSON))

	var savedInvalid Project
	require.NoError(t, db.First(&savedInvalid, "id = ?", invalid.ID).Error)
	assert.Nil(t, savedInvalid.BuildImageRefsJSON)

	var savedRegular Project
	require.NoError(t, db.First(&savedRegular, "id = ?", regular.ID).Error)
	require.NotNil(t, savedRegular.BuildImageRefsJSON)
	assert.Equal(t, "[]", *savedRegular.BuildImageRefsJSON)

	require.NoError(t, os.Remove(filepath.Join(validPath, "compose.yaml")))
	require.NoError(t, os.Remove(filepath.Join(regularPath, "compose.yaml")))
	count, err = svc.BackfillProjectImageRefs(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, db.First(&savedValid, "id = ?", valid.ID).Error)
	require.NotNil(t, savedValid.BuildImageRefsJSON)
	assert.Equal(t, []string{"local-app:latest"}, projects.ParseImageRefsJSON(*savedValid.BuildImageRefsJSON))
	require.NoError(t, db.First(&savedRegular, "id = ?", regular.ID).Error)
	require.NotNil(t, savedRegular.BuildImageRefsJSON)
	assert.Equal(t, "[]", *savedRegular.BuildImageRefsJSON)
}

func setupProjectDestroyTestServiceInternal(t *testing.T) (*ProjectService, *database.DB, string) {
	t.Helper()

	ctx := context.Background()
	db := setupProjectTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, config.Load(), nil)
	return NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load()), db, projectsDir
}

func TestProjectService_DestroyProject_RemovesFilesWhenRequested(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupProjectDestroyTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-remove")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project-data.txt"), []byte("keep until destroy\n"), 0o644))

	dirName := "demo-remove"
	project := &Project{
		ID:      "project-destroy-remove-files",
		Name:    "demo-remove",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, svc.DestroyProject(ctx, project.ID, true, false, common.User{}))

	_, statErr := os.Stat(projectPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProjectService_DestroyProject_PreservesFilesWhenRequested(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupProjectDestroyTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-preserve")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	projectDataPath := filepath.Join(projectPath, "project-data.txt")
	require.NoError(t, os.WriteFile(projectDataPath, []byte("preserve on destroy\n"), 0o644))

	dirName := "demo-preserve"
	project := &Project{
		ID:      "project-destroy-preserve-files",
		Name:    "demo-preserve",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, svc.DestroyProject(ctx, project.ID, false, false, common.User{}))

	assert.NoDirExists(t, projectPath)
	assert.NoFileExists(t, projectDataPath)

	entries, err := os.ReadDir(projectsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	trashName := entries[0].Name()
	require.True(t, strings.HasPrefix(trashName, ".arcane-trash-demo-preserve-"), "trash dir name: %s", trashName)
	assert.FileExists(t, filepath.Join(projectsDir, trashName, "project-data.txt"))
}

func TestProjectService_GetProjectFromDatabaseByID(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	// Setup dependencies
	settingsService, _ := newSettingsServiceForTestInternal(t, ctx, db)
	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	// Create test project
	proj := &Project{
		ID:   "p1",
		Name: "test-project",
		Path: "/tmp/test-project",
	}
	require.NoError(t, db.Create(proj).Error)

	// Test success
	found, err := svc.GetProjectFromDatabaseByID(ctx, "p1")
	require.NoError(t, err)
	assert.Equal(t, "test-project", found.Name)

	// Test not found
	_, err = svc.GetProjectFromDatabaseByID(ctx, "non-existent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project not found")
}

func TestProjectService_GetServiceCounts(t *testing.T) {
	tests := []struct {
		name        string
		services    []ProjectServiceInfo
		wantTotal   int
		wantRunning int
	}{
		{
			name: "mixed status",
			services: []ProjectServiceInfo{
				{Name: "s1", Status: "running"},
				{Name: "s2", Status: "exited"},
				{Name: "s3", Status: "up"},
			},
			wantTotal:   3,
			wantRunning: 2,
		},
		{
			name: "all stopped",
			services: []ProjectServiceInfo{
				{Name: "s1", Status: "exited"},
			},
			wantTotal:   1,
			wantRunning: 0,
		},
		{
			name:        "empty",
			services:    []ProjectServiceInfo{},
			wantTotal:   0,
			wantRunning: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, running := getServiceCounts(tt.services)
			assert.Equal(t, tt.wantTotal, total)
			assert.Equal(t, tt.wantRunning, running)
		})
	}
}

func TestProjectService_CalculateProjectStatus(t *testing.T) {
	tests := []struct {
		name     string
		services []ProjectServiceInfo
		want     ProjectStatus
	}{
		{
			name:     "empty",
			services: []ProjectServiceInfo{},
			want:     ProjectStatusUnknown,
		},
		{
			name: "all running",
			services: []ProjectServiceInfo{
				{Status: "running"},
				{Status: "up"},
			},
			want: ProjectStatusRunning,
		},
		{
			name: "all stopped",
			services: []ProjectServiceInfo{
				{Status: "exited"},
				{Status: "stopped"},
			},
			want: ProjectStatusStopped,
		},
		{
			name: "partial",
			services: []ProjectServiceInfo{
				{Status: "running"},
				{Status: "exited"},
			},
			want: ProjectStatusPartiallyRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateProjectStatus(tt.services)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProjectService_UpdateProjectStatusInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

	proj := &Project{
		ID:     "p1",
		Status: ProjectStatusUnknown,
	}
	require.NoError(t, db.Create(proj).Error)

	err := svc.updateProjectStatusInternal(ctx, "p1", ProjectStatusRunning)
	require.NoError(t, err)

	var updated Project
	require.NoError(t, db.First(&updated, "id = ?", "p1").Error)
	assert.Equal(t, ProjectStatusRunning, updated.Status)
	if updated.UpdatedAt != nil {
		assert.WithinDuration(t, time.Now(), *updated.UpdatedAt, time.Second)
	} else {
		assert.Fail(t, "UpdatedAt should not be nil")
	}
}

func TestProjectService_IncrementStatusCounts(t *testing.T) {
	running := 0
	stopped := 0

	incrementStatusCounts(ProjectStatusRunning, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 0, stopped)

	incrementStatusCounts(ProjectStatusStopped, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 1, stopped)

	incrementStatusCounts(ProjectStatusUnknown, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 1, stopped)
}

func TestProjectService_GetProjectByComposeName(t *testing.T) {
	ctx := context.Background()

	t.Run("exact match", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

		proj := &Project{
			ID:   "p1",
			Name: "myproject",
			Path: "/tmp/myproject",
		}
		require.NoError(t, db.Create(proj).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, proj.ID, found.ID)
	})

	t.Run("normalized fallback", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

		proj := &Project{
			ID:   "p1",
			Name: "myproject",
			Path: "/tmp/myproject",
		}
		require.NoError(t, db.Create(proj).Error)

		found, err := svc.GetProjectByComposeName(ctx, "My Project!")
		require.NoError(t, err)
		assert.Equal(t, proj.ID, found.ID)
	})

	t.Run("display name in db, normalized compose label input", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

		display := &Project{
			ID:   "p2",
			Name: "My Project!",
			Path: "/tmp/my-project",
		}
		require.NoError(t, db.Create(display).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, display.ID, found.ID)
	})

	t.Run("invalidates stale normalized cache entries after deletion", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

		original := &Project{
			ID:   "p3",
			Name: "My Project!",
			Path: "/tmp/my-project",
		}
		require.NoError(t, db.Create(original).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, original.ID, found.ID)

		cachedProjectID, cached := svc.composeNames.projectID("myproject").Get()
		require.True(t, cached)
		assert.Equal(t, original.ID, cachedProjectID)

		require.NoError(t, db.Delete(&Project{}, "id = ?", original.ID).Error)

		replacement := &Project{
			ID:   "p4",
			Name: "My Project!",
			Path: "/tmp/my-project-recreated",
		}
		require.NoError(t, db.Create(replacement).Error)

		found, err = svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, replacement.ID, found.ID)

		cachedProjectID, cached = svc.composeNames.projectID("myproject").Get()
		require.True(t, cached)
		assert.Equal(t, replacement.ID, cachedProjectID)
	})

	t.Run("invalidates stale normalized cache entries after rename", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load())

		original := &Project{
			ID:   "p5",
			Name: "My App!",
			Path: "/tmp/my-app",
		}
		require.NoError(t, db.Create(original).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myapp")
		require.NoError(t, err)
		assert.Equal(t, original.ID, found.ID)

		cachedProjectID, cached := svc.composeNames.projectID("myapp").Get()
		require.True(t, cached)
		assert.Equal(t, original.ID, cachedProjectID)

		require.NoError(t, db.Model(&Project{}).Where("id = ?", original.ID).Updates(map[string]any{
			"name": "New Service",
			"path": "/tmp/new-service",
		}).Error)

		_, err = svc.GetProjectByComposeName(ctx, "myapp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "project not found")

		_, cached = svc.composeNames.projectID("myapp").Get()
		assert.False(t, cached)
	})
}

func TestProjectService_PullProjectImages_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageRef := "registry.example.com/team/app:1.2.3"
	repository := "registry.example.com/team/app"
	imageID := "sha256:team-app"
	imageDigest := digest.FromString("team-app-digest").String()
	buildRepository := "arcane.local/demo-builder/service"

	server := newImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	}, nil)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	eventService := event.NewEventService(db, nil, nil)
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, "compose-pull")
	composeContent := fmt.Sprintf("services:\n  app:\n    image: %s\n  builder:\n    build: .\n", imageRef)
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(composeContent), 0o644))

	dirName := "compose-pull"
	projectRecord := &Project{
		ID:      "project-pull",
		Name:    "compose-pull",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	now := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:old-full",
		Repository:     repository,
		Tag:            "1.2.3",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "1.2.3",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:old-short",
		Repository:     "team/app",
		Tag:            "1.2.3",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "1.2.3",
		CheckTime:      now.Add(time.Minute),
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:build-only",
		Repository:     buildRepository,
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      now.Add(2 * time.Minute),
	}).Error)

	require.NoError(t, svc.PullProjectImages(ctx, projectRecord.ID, io.Discard, common.SystemUser, nil))

	// sha256:old-* records represent update records for OTHER containers still running
	// the old image. Pulling the image for one container must not mark them as up-to-date
	// (#2453: updating one container was incorrectly removing others from the update list).
	var fullRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-full").First(&fullRecord).Error)
	assert.True(t, fullRecord.HasUpdate)

	var shortRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-short").First(&shortRecord).Error)
	assert.True(t, shortRecord.HasUpdate)

	var buildRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:build-only").First(&buildRecord).Error)
	assert.True(t, buildRecord.HasUpdate)

	// The newly pulled image itself is correctly marked as up-to-date.
	var currentRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, repository, currentRecord.Repository)
	assert.Equal(t, "1.2.3", currentRecord.Tag)
	assert.Equal(t, imageDigest, mo.PointerToOption(currentRecord.CurrentDigest).OrEmpty())
	assert.Equal(t, imageDigest, mo.PointerToOption(currentRecord.LatestDigest).OrEmpty())
}

func TestProjectService_EnsureImagesPresent_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/api:2.0.0"
	repository := "registry.example.com/team/api"
	imageID := "sha256:team-api"
	imageDigest := digest.FromString("team-api-digest").String()

	server := newImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	}, nil)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	eventService := event.NewEventService(db, nil, nil)
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:old-api",
		Repository:     repository,
		Tag:            "2.0.0",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "2.0.0",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	require.NoError(t, svc.ensureImagesPresent(ctx, map[string]projects.ImagePullStep{
		imageRef: {Mode: projects.ImagePullModeAlways},
	}, io.Discard, nil, common.SystemUser))

	// sha256:old-api may still be in use by another container — pulling for one container
	// must not clear it (fixes #2453).
	var oldRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-api").First(&oldRecord).Error)
	assert.True(t, oldRecord.HasUpdate)

	var currentRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, mo.PointerToOption(currentRecord.LatestDigest).OrEmpty())
}

func TestProjectService_PullImageForService_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/worker:3.1.4"
	repository := "registry.example.com/team/worker"
	imageID := "sha256:team-worker"
	imageDigest := digest.FromString("team-worker-digest").String()

	server := newImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	}, nil)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	eventService := event.NewEventService(db, nil, nil)
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:old-worker",
		Repository:     repository,
		Tag:            "3.1.4",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "3.1.4",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	require.NoError(t, svc.pullAndReconcileImageInternal(ctx, imageRef, io.Discard, common.SystemUser, nil))

	// sha256:old-worker may still be in use by another container — must not be cleared (fixes #2453).
	var oldRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-worker").First(&oldRecord).Error)
	assert.True(t, oldRecord.HasUpdate)

	var currentRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, mo.PointerToOption(currentRecord.LatestDigest).OrEmpty())
}

func TestProjectService_ComposePullSelectedServicesInternal_ReconcilesOnlyOnSuccess(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	privateImageRef := "registry.example.com/team/app:9.9.9"
	privateRepository := "registry.example.com/team/app"
	privateImageID := "sha256:team-app-compose"
	privateImageDigest := digest.FromString("team-app-compose-digest").String()
	publicImageRef := "docker.io/library/nginx:1.27"
	publicRepository := "docker.io/library/nginx"
	publicImageID := "sha256:nginx-compose"
	publicImageDigest := digest.FromString("nginx-compose-digest").String()

	pullsByRef := map[string]int{}
	authHeadersByRef := map[string]string{}
	server := newImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
		privateImageRef: {
			ID:          privateImageID,
			RepoTags:    []string{privateImageRef},
			RepoDigests: []string{privateRepository + "@" + privateImageDigest},
		},
		publicImageRef: {
			ID:          publicImageID,
			RepoTags:    []string{publicImageRef},
			RepoDigests: []string{publicRepository + "@" + publicImageDigest},
		},
	}, func(fullRef string, authHeader string) {
		pullsByRef[fullRef]++
		authHeadersByRef[fullRef] = authHeader
	})

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	eventService := event.NewEventService(db, nil, nil)
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())

	projectDef := &composetypes.Project{
		Name: "compose-selected",
		Services: composetypes.Services{
			"app": {
				Name:  "app",
				Image: privateImageRef,
			},
			"app-copy": {
				Name:  "app-copy",
				Image: privateImageRef,
			},
			"sidecar": {
				Name:  "sidecar",
				Image: publicImageRef,
			},
			"builder": {
				Name:  "builder",
				Build: &composetypes.BuildConfig{Context: "."},
			},
		},
	}

	now := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     privateRepository,
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:sidecar-old",
		Repository:     publicRepository,
		Tag:            "1.27",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "1.27",
		CheckTime:      now,
	}).Error)

	credentials := []containerregistry.Credential{{
		URL:      "https://registry.example.com",
		Username: "arcane-user",
		Token:    "arcane-token",
		Enabled:  true,
	}}

	require.NoError(t, svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app", "app-copy", "sidecar", "builder"}, common.SystemUser, credentials))
	assert.Equal(t, 1, pullsByRef[privateImageRef], "duplicate service refs should only be pulled once")
	assert.Equal(t, 1, pullsByRef[publicImageRef], "selected public image should still be pulled")
	assert.Len(t, pullsByRef, 2, "build-backed services should not trigger image pulls")

	privateAuth := decodeRegistryAuthInternal(t, authHeadersByRef[privateImageRef])
	assert.Equal(t, "arcane-user", privateAuth.Username)
	assert.Equal(t, "arcane-token", privateAuth.Password)
	assert.Equal(t, "registry.example.com", privateAuth.ServerAddress)
	assert.Empty(t, authHeadersByRef[publicImageRef], "public image pull should not receive unrelated registry auth")

	// sha256:selected-old may still be used by another container — must not be cleared (fixes #2453).
	var selectedRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.True(t, selectedRecord.HasUpdate)

	var sidecarRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:sidecar-old").First(&sidecarRecord).Error)
	assert.True(t, sidecarRecord.HasUpdate)

	var privateCurrentRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", privateImageID).First(&privateCurrentRecord).Error)
	assert.False(t, privateCurrentRecord.HasUpdate)
	assert.Equal(t, privateImageDigest, mo.PointerToOption(privateCurrentRecord.LatestDigest).OrEmpty())

	var publicCurrentRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", publicImageID).First(&publicCurrentRecord).Error)
	assert.False(t, publicCurrentRecord.HasUpdate)
	assert.Equal(t, publicImageDigest, mo.PointerToOption(publicCurrentRecord.LatestDigest).OrEmpty())
}

func TestProjectService_ComposePullSelectedServicesInternal_LeavesRecordsWhenPullFails(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/app:9.9.9"
	repository := "registry.example.com/team/app"

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/images/create") {
			http.Error(w, "pull failed", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(failingServer.Close)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, failingServer)}
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, event.NewEventService(db, nil, nil))
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())

	projectDef := &composetypes.Project{
		Name: "compose-selected",
		Services: composetypes.Services{
			"app": {
				Name:  "app",
				Image: imageRef,
			},
		},
	}

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     repository,
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	err = svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app"}, common.SystemUser, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to pull image")

	var selectedRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.True(t, selectedRecord.HasUpdate)

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&imageupdate.ImageUpdateRecord{}).Where("id = ?", "sha256:team-app-compose").Count(&count).Error)
	assert.Zero(t, count)
}

func TestProjectService_UpdateProjectServicesHardFailsWhenPullFailsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageRef := "registry.example.com/team/app:9.9.9"
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/images/create") {
			http.Error(w, "compose pull failed", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(failingServer.Close)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, failingServer)}
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, event.NewEventService(db, nil, nil))

	projectPath := createComposeProjectDir(t, projectsDir, "compose-update-pull-fail")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: "+imageRef+"\n"), 0o644))

	projectRecord := &Project{
		ID:      "project-update-pull-fail",
		Name:    "compose-update-pull-fail",
		DirName: new("compose-update-pull-fail"),
		Path:    projectPath,
		Status:  ProjectStatusRunning,
	}
	require.NoError(t, db.Create(projectRecord).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     "registry.example.com/team/app",
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	originalComposeUp := composeUpProjectServicesInternal
	t.Cleanup(func() {
		composeUpProjectServicesInternal = originalComposeUp
	})
	upCalled := false
	composeUpProjectServicesInternal = func(context.Context, *composetypes.Project, []string, bool, bool, bool, map[string]dockerregistry.AuthConfig, time.Duration) error {
		upCalled = true
		return errors.New("compose up should not run")
	}

	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())
	err = svc.UpdateProjectServices(ctx, projectRecord.ID, []string{"app"}, common.SystemUser)
	require.Error(t, err)
	require.ErrorContains(t, err, "pull updated service images")
	assert.False(t, upCalled, "compose up must not run after a pull failure")

	var persistedProject Project
	require.NoError(t, db.WithContext(ctx).Where("id = ?", projectRecord.ID).First(&persistedProject).Error)
	assert.Equal(t, ProjectStatusRunning, persistedProject.Status)

	var persistedRecord imageupdate.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&persistedRecord).Error)
	assert.True(t, persistedRecord.HasUpdate)
}

func TestProjectService_UpdateProjectServicesForcesRecreateInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageRef := "registry.example.com/team/app:9.9.9"
	repository := "registry.example.com/team/app"
	imageID := "sha256:project-update-force"
	imageDigest := digest.FromString("project-update-force-digest").String()

	server := newImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	}, nil)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	imageUpdateService := imageupdate.NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := image.NewImageService(db, dockerService, nil, imageUpdateService, nil, event.NewEventService(db, nil, nil))

	projectPath := createComposeProjectDir(t, projectsDir, "compose-update-force")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: "+imageRef+"\n"), 0o644))

	projectRecord := &Project{
		ID:      "project-update-force",
		Name:    "compose-update-force",
		DirName: new("compose-update-force"),
		Path:    projectPath,
		Status:  ProjectStatusRunning,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	originalComposeStop := composeStopProjectServicesInternal
	originalComposeUp := composeUpProjectServicesInternal
	t.Cleanup(func() {
		composeStopProjectServicesInternal = originalComposeStop
		composeUpProjectServicesInternal = originalComposeUp
	})
	composeStopProjectServicesInternal = func(context.Context, *composetypes.Project, []string) error {
		return nil
	}
	upCalled := false
	forceRecreate := false
	composeUpProjectServicesInternal = func(_ context.Context, _ *composetypes.Project, services []string, removeOrphans bool, force bool, _ bool, _ map[string]dockerregistry.AuthConfig, _ time.Duration) error {
		upCalled = true
		forceRecreate = force
		assert.Equal(t, []string{"app"}, services)
		assert.False(t, removeOrphans)
		return errors.New("compose up failed after assertion")
	}

	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, nil, config.Load())
	err = svc.UpdateProjectServices(ctx, projectRecord.ID, []string{"app"}, common.SystemUser)
	require.Error(t, err)
	assert.True(t, upCalled)
	assert.True(t, forceRecreate, "service updates must force recreate after pulling the updated image")
}

type fakeProjectVolumeRenameMigrationInternal struct {
	applyCalled    bool
	commitCalled   bool
	rollbackCalled bool
	applyErr       error
	commitErr      error
	rollbackErr    error
}

func (m *fakeProjectVolumeRenameMigrationInternal) Apply(context.Context) error {
	m.applyCalled = true
	return m.applyErr
}

func (m *fakeProjectVolumeRenameMigrationInternal) Rollback(context.Context) error {
	m.rollbackCalled = true
	return m.rollbackErr
}

func (m *fakeProjectVolumeRenameMigrationInternal) Commit(context.Context) error {
	m.commitCalled = true
	return m.commitErr
}

func TestProjectService_UpdateProject_RenameFailsWhenVolumeMigrationPreparationFails(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	dockerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = io.WriteString(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/version"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"ApiVersion":    "1.41",
				"MinAPIVersion": "1.24",
				"Version":       "24.0.0",
			})) {
				return
			}
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/bar_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name": "bar_data",
			})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(dockerServer.Close)
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, dockerServer.URL))

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, dockerServer)}
	svc := NewProjectService(db, settingsService, eventService, nil, dockerService, nil, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	require.NoError(t, os.WriteFile(filepath.Join(originalPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"), 0o644))

	project := &Project{
		ID:      "proj-volume-conflict",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "target volume already exists")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
}

func TestProjectService_ApplyProjectUpdateWithRenameJournal_AppliesVolumeMigrationWhenNameChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{}

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &Project{
		ID:      "proj-volume-success",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
	require.NoError(t, err)

	assert.True(t, migration.applyCalled)
	assert.True(t, migration.commitCalled)
	assert.False(t, migration.rollbackCalled)
	assert.True(t, projectStateCommitted)
	assert.Equal(t, "bar", projectForUpdate.Name)
	assert.DirExists(t, filepath.Join(projectsDir, "bar"))
	assert.NoDirExists(t, originalPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "bar", fromDB.Name)
	assert.Equal(t, filepath.Join(projectsDir, "bar"), fromDB.Path)
}

func TestProjectService_PrepareProjectRenameVolumeMigrationForUpdate_UsesComposePreview(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	dockerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name":   "nginx_data",
				"Driver": "local",
				"Labels": map[string]string{
					composeapi.ProjectLabel: "nginx",
					composeapi.VolumeLabel:  "data",
				},
			})) {
				return
			}
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(dockerServer.Close)

	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, dockerServer)}
	svc := NewProjectService(db, settingsService, nil, nil, dockerService, nil, nil, nil, config.Load())

	projectPath := filepath.Join(projectsDir, "nginx")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	oldCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(oldCompose), 0o644))

	project := &Project{
		ID:      "proj-preview-volume-rename",
		Name:    "nginx",
		DirName: new("nginx"),
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}

	t.Run("skips volume made explicit in pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: fixed-data\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil)

		require.NoError(t, err)
		require.Nil(t, migration)
		bytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
		require.NoError(t, readErr)
		require.Equal(t, oldCompose, string(bytes))
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans unchanged auto-managed volume from pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		journalVolumes := journalSource.JournalVolumes()
		require.Len(t, journalVolumes, 1)
		require.Equal(t, "data", journalVolumes[0].Key)
		require.Equal(t, "nginx_data", journalVolumes[0].OldName)
		require.Equal(t, "web_data", journalVolumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans auto-managed volume when pending compose name renames project", func(t *testing.T) {
		newCompose := "name: web\nservices:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		journalVolumes := journalSource.JournalVolumes()
		require.Len(t, journalVolumes, 1)
		require.Equal(t, "data", journalVolumes[0].Key)
		require.Equal(t, "nginx_data", journalVolumes[0].OldName)
		require.Equal(t, "web_data", journalVolumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans interpolated explicit name from pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: ${DATA_VOLUME:-nginx_data}\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		journalVolumes := journalSource.JournalVolumes()
		require.Len(t, journalVolumes, 1)
		require.Equal(t, "data", journalVolumes[0].Key)
		require.Equal(t, "nginx_data", journalVolumes[0].OldName)
		require.Equal(t, "web_data", journalVolumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})
}

func TestProjectService_ApplyProjectUpdateWithRenameJournal_RollsBackVolumeMigrationWhenProjectSaveFails(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{}

	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("arcane_test_project_save_failure", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Project" {
			_ = tx.AddError(errors.New("forced project save failure"))
		}
	}))

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &Project{
		ID:      "proj-volume-save-fail",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = withProjectRenameRollback(ctx, &projectForUpdate, &projectStateCommitted, func() error {
		return svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forced project save failure")
	assert.True(t, migration.applyCalled)
	assert.False(t, migration.commitCalled)
	assert.True(t, migration.rollbackCalled)
	assert.False(t, projectStateCommitted)
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))
}

func TestProjectService_ApplyProjectUpdateWithRenameJournal_SucceedsCommittedRenameWhenSourceCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{
		commitErr: errors.New("source cleanup failed"),
	}

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &Project{
		ID:      "proj-volume-cleanup-fail",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = withProjectRenameRollback(ctx, &projectForUpdate, &projectStateCommitted, func() error {
		return svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
	})
	require.NoError(t, err)
	require.True(t, migration.applyCalled)
	require.True(t, migration.commitCalled)
	require.False(t, migration.rollbackCalled)
	require.True(t, projectStateCommitted)
	require.NoDirExists(t, originalPath)
	require.DirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "bar", fromDB.Name)
	require.Equal(t, filepath.Join(projectsDir, "bar"), fromDB.Path)
}

func TestProjectService_UpdateProject_ClearsJournalForNonRenameWhenRecoveryDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)

	oldDir := "nginx"
	projectPath := createComposeProjectDir(t, projectsDir, oldDir)
	project := &Project{
		ID:      "proj-non-rename-recovery-docker-unavailable",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    projectPath,
		NewPath:    filepath.Join(projectsDir, "web"),
		OldDirName: &oldDir,
		NewDirName: "web",
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	envContent := "FOO=bar\n"
	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, &envContent, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.Equal(t, "nginx", updated.Name)

	envBytes, err := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, err)
	require.Equal(t, envContent, string(envBytes))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_UpdateProject_AllowsRenameAfterJournalRecoveryDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)

	oldDir := "nginx"
	projectPath := createComposeProjectDir(t, projectsDir, oldDir)
	project := &Project{
		ID:      "proj-rename-recovery-docker-unavailable",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    projectPath,
		NewPath:    filepath.Join(projectsDir, "web"),
		OldDirName: &oldDir,
		NewDirName: "web",
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	updated, err := svc.UpdateProject(ctx, project.ID, new("web"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "web", updated.Name)
	require.NoDirExists(t, projectPath)
	require.DirExists(t, filepath.Join(projectsDir, "web"))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_UpdateProject_RenamesDirectoryWhenNameChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &Project{
		ID:      "proj-1",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)

	expectedPath := filepath.Join(projectsDir, "bar")
	assert.Equal(t, "bar", updated.Name)
	assert.Equal(t, expectedPath, updated.Path)
	require.NotNil(t, updated.DirName)
	assert.Equal(t, "bar", *updated.DirName)
	assert.NoDirExists(t, originalPath)
	assert.DirExists(t, expectedPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "bar", fromDB.Name)
	assert.Equal(t, expectedPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	assert.Equal(t, "bar", *fromDB.DirName)
}

func TestProjectService_UpdateProject_RenameFailsWhenTargetDirectoryExists(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	targetPath := filepath.Join(projectsDir, "bar")
	require.NoError(t, os.MkdirAll(targetPath, 0o755))

	project := &Project{
		ID:      "proj-2",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project directory already exists")
	assert.DirExists(t, originalPath)
	assert.DirExists(t, targetPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	assert.Equal(t, "Foo", *fromDB.DirName)
}

func TestProjectService_UpdateProject_RenameFailsWhenProjectRunning(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := filepath.Join(projectsDir, originalDirName)
	require.NoError(t, os.MkdirAll(originalPath, 0o755))

	project := &Project{
		ID:      "proj-3",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusRunning,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	assert.Equal(t, "Foo", *fromDB.DirName)
}

func TestProjectService_UpdateProject_RenameRejectsStaleStoppedWhenRuntimeIsRunning(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, []container.Summary{
		{
			ID:     "app-container",
			Names:  []string{"/foo-app-1"},
			Image:  "nginx:alpine",
			State:  container.StateRunning,
			Status: "Up 30 seconds",
			Labels: map[string]string{
				composeapi.ProjectLabel:    "foo",
				composeapi.ServiceLabel:    "app",
				composeapi.ConfigHashLabel: "app-hash",
				composeapi.WorkingDirLabel: filepath.Join(projectsDir, "Foo"),
			},
		},
	})

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &Project{
		ID:      "proj-stale-stopped-running-rename",
		Name:    "Foo",
		DirName: &originalDirName,
		Path:    originalPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	assert.Equal(t, ProjectStatusStopped, fromDB.Status)
}

func TestProjectService_UpdateProject_RenameResolvesUnknownStoppedStatusBeforeVolumeMigration(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	server := newProjectRuntimeDockerServerInternal(t, nil)
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	statusReason := "stale runtime status"

	project := &Project{
		ID:           "proj-unknown-stopped-rename",
		Name:         "Foo",
		DirName:      &originalDirName,
		Path:         originalPath,
		Status:       ProjectStatusUnknown,
		StatusReason: &statusReason,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)

	expectedPath := filepath.Join(projectsDir, "bar")
	assert.Equal(t, "bar", updated.Name)
	assert.Equal(t, expectedPath, updated.Path)
	assert.Equal(t, ProjectStatusStopped, updated.Status)
	assert.Nil(t, updated.StatusReason)
	assert.Equal(t, 1, updated.ServiceCount)
	assert.Equal(t, 0, updated.RunningCount)
	assert.NoDirExists(t, originalPath)
	assert.DirExists(t, expectedPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "bar", fromDB.Name)
	assert.Equal(t, expectedPath, fromDB.Path)
	assert.Equal(t, ProjectStatusStopped, fromDB.Status)
	assert.Nil(t, fromDB.StatusReason)
	assert.Equal(t, 1, fromDB.ServiceCount)
	assert.Equal(t, 0, fromDB.RunningCount)
}

func TestProjectService_UpdateProject_RenameRejectsUnknownWhenRuntimeIsRunning(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	server := newProjectRuntimeDockerServerInternal(t, []container.Summary{
		{
			ID:     "app-container",
			Names:  []string{"/foo-app-1"},
			Image:  "nginx:alpine",
			State:  container.StateRunning,
			Status: "Up 30 seconds",
			Labels: map[string]string{
				composeapi.ProjectLabel:    "foo",
				composeapi.ServiceLabel:    "app",
				composeapi.ConfigHashLabel: "app-hash",
				composeapi.WorkingDirLabel: "/host/path/projects/Foo",
			},
		},
	})
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	statusReason := "stale runtime status"

	project := &Project{
		ID:           "proj-unknown-running-rename",
		Name:         "Foo",
		DirName:      &originalDirName,
		Path:         originalPath,
		Status:       ProjectStatusUnknown,
		StatusReason: &statusReason,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	assert.Equal(t, ProjectStatusUnknown, fromDB.Status)
	require.NotNil(t, fromDB.StatusReason)
	assert.Equal(t, statusReason, *fromDB.StatusReason)
}

func TestProjectService_UpdateProject_ValidatesComposeUsingExistingProjectName(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "demo"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &Project{
		ID:      "proj-compose-name",
		Name:    "demo",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `name: ${COMPOSE_PROJECT_NAME}
services:
  app:
    image: nginx:alpine
`
	env := "COMPOSE_PROJECT_NAME=\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "demo", updated.Name)
}

func TestProjectService_UpdateProject_AllowsMissingEnvFileDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "env-required"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &Project{
		ID:      "proj-env-file",
		Name:    "env-required",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	_, statErr := os.Stat(filepath.Join(projectPath, ".env"))
	require.NoError(t, statErr)
}

func TestProjectService_UpdateProject_EnvRetargetWritesExplicitIdenticalComposeToSelectedBase(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "retarget-explicit"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	baseContent := "services:\n  base:\n    image: nginx:alpine\n"
	altContent := "services:\n  alt:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(baseContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "alt.yaml"), []byte(altContent), 0o644))

	project := &Project{
		ID:      "proj-retarget-explicit",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	// An explicit composeContent is authoritative even when it is
	// byte-identical to the previous base: the caller intends the retargeted
	// base to carry this content.
	explicitCompose := baseContent
	_, err = svc.UpdateProject(ctx, project.ID, nil, &explicitCompose, new("COMPOSE_FILE=alt.yaml\n"), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	altOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "alt.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, baseContent, string(altOnDisk), "an explicit submission must be written to the newly selected base")

	baseOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, baseContent, string(baseOnDisk))

	envOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(envOnDisk), "COMPOSE_FILE=alt.yaml")
}

func TestProjectService_UpdateProject_EnvRetargetWithoutComposePayloadPreservesNewBase(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "retarget-omit"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	baseContent := "services:\n  base:\n    image: nginx:alpine\n"
	altContent := "services:\n  alt:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(baseContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "alt.yaml"), []byte(altContent), 0o644))

	project := &Project{
		ID:      "proj-retarget-omit",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	// Clients omit composeContent when the compose editor is unchanged: an
	// env-only retarget must switch the selection without touching either file.
	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, new("COMPOSE_FILE=alt.yaml\n"), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	altOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "alt.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, altContent, string(altOnDisk), "the newly selected base must keep its own content")

	baseOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, baseContent, string(baseOnDisk))

	envOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(envOnDisk), "COMPOSE_FILE=alt.yaml")
}

func TestProjectService_UpdateProject_EnvRetargetWritesEditedComposeToSelectedBase(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "retarget-edit"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	baseContent := "services:\n  base:\n    image: nginx:alpine\n"
	altContent := "services:\n  alt:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(baseContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "alt.yaml"), []byte(altContent), 0o644))

	project := &Project{
		ID:      "proj-retarget-edit",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	editedCompose := "services:\n  alt:\n    image: redis:alpine\n"
	_, err = svc.UpdateProject(ctx, project.ID, nil, &editedCompose, new("COMPOSE_FILE=alt.yaml\n"), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	altOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "alt.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, editedCompose, string(altOnDisk), "an edited compose must be written to the newly selected base")

	baseOnDisk, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, baseContent, string(baseOnDisk))
}

func TestProjectService_UpdateProject_AllowsMissingLocalIncludeDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "include-new"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &Project{
		ID:      "proj-missing-include",
		Name:    "include-new",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - metadata.yaml
services:
  app:
    image: nginx:alpine
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	includePath := filepath.Join(projectPath, "metadata.yaml")
	assert.NoFileExists(t, includePath)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
	require.NoError(t, err)
	require.Len(t, details.IncludeFiles, 1)
	assert.Equal(t, "metadata.yaml", details.IncludeFiles[0].RelativePath)

}

func TestProjectService_CreateProject_AllowsExternalInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "shared.yaml"), []byte("services: {}\n"), 0o644))

	compose := `include:
  - ../shared.yaml
services:
  app:
    image: nginx:alpine
`

	project, err := svc.CreateProject(ctx, "with-external-include", compose, nil, projecttypes.CreateProjectWorkspaceManifest{}, nil, nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.DirExists(t, filepath.Join(projectsDir, "with-external-include"))
	assert.FileExists(t, filepath.Join(projectsDir, "shared.yaml"))
}

func TestProjectService_UpdateProject_AllowsExternalInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "external-include"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "shared.yaml"), []byte("services: {}\n"), 0o644))

	project := &Project{
		ID:      "proj-external-include",
		Name:    "external-include",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	user := common.User{ID: "u1", Username: "tester"}

	compose := `include:
  - ../shared.yaml
services:
  app:
    image: nginx:alpine
`
	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, user)
	require.NoError(t, err)
	require.NotNil(t, updated)

	override := `include:
  - ../shared.yaml
`
	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, nil, new(override), user)
	require.NoError(t, err)
}

func TestProjectService_CreateProject_CommitsWorkspaceAndConfigurationTogether(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	svc := NewProjectService(db, settingsService, event.NewEventService(db, nil, nil), nil, nil, nil, nil, nil, config.Load())
	uploadIndex := 0
	manifest := projecttypes.CreateProjectWorkspaceManifest{FileChanges: []projecttypes.WorkspaceFileChange{{
		Operation: projecttypes.FileOpCreateFile, RelativePath: "config/app.txt", UploadIndex: &uploadIndex,
	}}}

	created, err := svc.CreateProject(
		ctx,
		"atomic-workspace",
		"services:\n  app:\n    image: nginx:alpine\n",
		new("VALUE=one\n"),
		manifest,
		map[int][]byte{0: []byte("workspace content\n")},
		nil,
		nil,
		common.User{ID: "u1", Username: "tester"},
	)
	require.NoError(t, err)
	require.NotNil(t, created)
	require.FileExists(t, filepath.Join(created.Path, projects.DefaultComposeFileName))
	require.FileExists(t, filepath.Join(created.Path, ".env"))
	require.FileExists(t, filepath.Join(created.Path, "config", "app.txt"))
	content, err := os.ReadFile(filepath.Join(created.Path, "config", "app.txt"))
	require.NoError(t, err)
	require.Equal(t, "workspace content\n", string(content))
}

func TestProjectService_CreateProject_RollsBackInvalidWorkspaceManifest(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	svc := NewProjectService(db, settingsService, event.NewEventService(db, nil, nil), nil, nil, nil, nil, nil, config.Load())
	manifest := projecttypes.CreateProjectWorkspaceManifest{FileChanges: []projecttypes.WorkspaceFileChange{{
		Operation: projecttypes.FileOpDelete, RelativePath: "missing.txt",
	}}}

	created, err := svc.CreateProject(
		ctx,
		"invalid-workspace",
		"services:\n  app:\n    image: nginx:alpine\n",
		nil,
		manifest,
		nil,
		nil,
		nil,
		common.User{ID: "u1", Username: "tester"},
	)
	require.Error(t, err)
	require.Nil(t, created)
	require.NoDirExists(t, filepath.Join(projectsDir, "invalid-workspace"))

	var count int64
	require.NoError(t, db.Model(&Project{}).Where("name = ?", "invalid-workspace").Count(&count).Error)
	require.Zero(t, count)
}

func TestProjectService_UpdateProject_UsesExistingEnvFileDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "env-existing"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("FOO=bar\n"), 0o600))

	project := &Project{
		ID:      "proj-existing-env-file",
		Name:    "env-existing",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	envBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "FOO=bar\n", string(envBytes))
}

// newProjectServiceForOverrideTestInternal builds a project with a base compose
// file on disk and returns the service, the project record, and the project path.
func newProjectServiceForOverrideTestInternal(t *testing.T, dirName, baseCompose string) (*ProjectService, *Project, string) {
	t.Helper()
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(baseCompose), 0o600))

	project := &Project{
		ID:      "proj-" + dirName,
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	return svc, project, projectPath
}

func TestProjectService_UpdateProject_CreatesOverrideWithDefaultName(t *testing.T) {
	ctx := context.Background()
	svc, project, projectPath := newProjectServiceForOverrideTestInternal(t, "override-create", "services:\n  app:\n    image: nginx:alpine\n")

	override := "services:\n  app:\n    image: busybox:latest\n"
	_, err := svc.UpdateProject(ctx, project.ID, nil, nil, nil, new(override), common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	// A project with no override on disk gets the .yaml default, matching Arcane's
	// compose.yaml base default (not compose.override.yml, candidates[0]).
	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.override.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, override, string(overrideBytes))

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{IncludeComposeContent: true})
	require.NoError(t, err)
	assert.Equal(t, "compose.override.yaml", details.OverrideFileName)
	assert.Equal(t, override, details.OverrideContent)
}

func TestProjectService_UpdateProject_PreservesExistingOverrideName(t *testing.T) {
	ctx := context.Background()
	svc, project, projectPath := newProjectServiceForOverrideTestInternal(t, "override-preserve", "services:\n  app:\n    image: nginx:alpine\n")

	// An existing docker-compose.override.yml must keep its name on edit rather
	// than being rewritten to the default compose.override.yaml.
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "docker-compose.override.yml"), []byte("services:\n  app:\n    image: alpine:3\n"), 0o600))

	override := "services:\n  app:\n    image: busybox:latest\n"
	_, err := svc.UpdateProject(ctx, project.ID, nil, nil, nil, new(override), common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "docker-compose.override.yml"))
	require.NoError(t, readErr)
	assert.Equal(t, override, string(overrideBytes))
	assert.NoFileExists(t, filepath.Join(projectPath, "compose.override.yaml"))
}

func TestProjectService_UpdateProject_DeletesOverrideOnBlank(t *testing.T) {
	ctx := context.Background()
	svc, project, projectPath := newProjectServiceForOverrideTestInternal(t, "override-delete", "services:\n  app:\n    image: nginx:alpine\n")

	overridePath := filepath.Join(projectPath, "compose.override.yaml")
	require.NoError(t, os.WriteFile(overridePath, []byte("services:\n  app:\n    image: busybox:latest\n"), 0o600))

	// A non-nil blank override deletes the file so the deploy stops merging it.
	_, err := svc.UpdateProject(ctx, project.ID, nil, nil, nil, new(""), common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)

	assert.NoFileExists(t, overridePath)
}

func TestProjectService_UpdateProject_MergedValidationFailureLeavesDiskUnchanged(t *testing.T) {
	ctx := context.Background()
	baseCompose := "services:\n  app:\n    image: nginx:alpine\n"
	svc, project, projectPath := newProjectServiceForOverrideTestInternal(t, "override-invalid", baseCompose)

	// A new base compose plus a malformed override must fail validation, and the
	// backup/restore must leave both the base and the override untouched on disk.
	newCompose := "services:\n  app:\n    image: nginx:1.27\n"
	badOverride := "services:\n  app:\n    image: \"unterminated\n"
	_, err := svc.UpdateProject(ctx, project.ID, nil, new(newCompose), nil, new(badOverride), common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.Error(t, err)

	baseBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, baseCompose, string(baseBytes))
	assert.NoFileExists(t, filepath.Join(projectPath, "compose.override.yaml"))
}

func TestProjectService_UpdateProject_UsesProvidedEnvContentDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "env-updated"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &Project{
		ID:      "proj-new-env-file",
		Name:    "env-updated",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`
	env := "FOO=updated\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	envBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, env, string(envBytes))
}

func TestProjectService_UpdateProject_ReturnsEnvParseErrorDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "env-invalid"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &Project{
		ID:      "proj-invalid-env-file",
		Name:    "env-invalid",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    environment:
      - REQUIRED=${REQUIRED}
`
	env := "BROKEN=${UNTERMINATED\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "invalid compose file: parse provided env content")
	assert.Contains(t, err.Error(), "parse env")
}

func TestProjectService_UpdateProject_UsesGlobalEnvDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "global-env-update"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=/srv/media\nMYPATH=/containers/\n"), 0o600))

	project := &Project{
		ID:      "proj-global-env-update",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  cats:
    image: mikesir87/cats:1.0
    volumes:
      - ${DATA_NAS_FOLDER}:/data
      - ${MYPATH}cats/templates:/app/templates
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	composeBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, compose, string(composeBytes))
}

func TestProjectService_UpdateProject_DoesNotResolveHostEnvThroughGlobalEnvDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	t.Setenv("HOST_ONLY_PATH", "/host/secret")

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "host-env-guard"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=${HOST_ONLY_PATH}\n"), 0o600))

	project := &Project{
		ID:      "proj-host-env-guard",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    volumes:
      - ${DATA_NAS_FOLDER}:/data
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "invalid compose file")
}

func TestProjectService_UpdateProject_DerivesProjectOverrideEnvWhenGitSourceExists(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "override-edit"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))

	project := &Project{
		ID:      "proj-override-edit",
		Name:    "override-edit",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nLOCAL_ONLY=example\n"), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "LOCAL_ONLY=example\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "BASE=git\n")
	assert.Contains(t, string(effectiveBytes), "LOCAL_ONLY=example\n")
}

func TestProjectService_UpdateProject_UnchangedGitEnvLeavesFilesUntouched(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "unchanged-git-env"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	gitContent := "# git formatting\nBASE=git\n"
	overrideContent := "# local formatting\nTOKEN='$pbkdf2-sha512$310000$XXX'\n"
	effectiveContent := gitContent + overrideContent
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte(effectiveContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte(gitContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte(overrideContent), 0o600))

	project := &Project{
		ID:      "proj-unchanged-git-env",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, &effectiveContent, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, effectiveContent, string(effectiveBytes))

	gitBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env.git"))
	require.NoError(t, readErr)
	assert.Equal(t, gitContent, string(gitBytes))

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, overrideContent, string(overrideBytes))
}

func TestProjectService_PersistEffectiveEnvContent_RemovesStaleOverrideWithoutGitSource(t *testing.T) {
	projectsDir := t.TempDir()
	projectPath := filepath.Join(projectsDir, "direct-env")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	effectiveContent := "TOKEN=current\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte(effectiveContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=stale\n"), 0o600))

	require.NoError(t, persistEffectiveEnvContentInternal(t.Context(), projectPath, projectsDir, effectiveContent))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, effectiveContent, string(effectiveBytes))

	_, statErr := os.Stat(filepath.Join(projectPath, "project.env"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProjectService_PersistEffectiveEnvContent_RemovesStaleGitOverride(t *testing.T) {
	projectsDir := t.TempDir()
	projectPath := filepath.Join(projectsDir, "git-env")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	effectiveContent := "BASE=git\nTOKEN=git\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte(effectiveContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte(effectiveContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=stale\n"), 0o600))

	require.NoError(t, persistEffectiveEnvContentInternal(t.Context(), projectPath, projectsDir, effectiveContent))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, effectiveContent, string(effectiveBytes))

	gitBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env.git"))
	require.NoError(t, readErr)
	assert.Equal(t, effectiveContent, string(gitBytes))

	_, statErr := os.Stat(filepath.Join(projectPath, "project.env"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestProjectService_UpdateProject_DeletingGitBackedKeyFallsBackToGit(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "override-delete"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=local\nLOCAL_ONLY=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nTOKEN=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=local\nLOCAL_ONLY=1\n"), 0o600))

	project := &Project{
		ID:      "proj-override-delete",
		Name:    "override-delete",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nLOCAL_ONLY=1\n"), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "LOCAL_ONLY=1\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "BASE=git\n")
	assert.Contains(t, string(effectiveBytes), "TOKEN=git\n")
	assert.Contains(t, string(effectiveBytes), "LOCAL_ONLY=1\n")
	assert.NotContains(t, string(overrideBytes), "TOKEN=")
}

func TestProjectService_ApplyGitSyncProjectFiles_MigratesDirectEnvIntoProjectOverride(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-migrate"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TOKEN=stale-local\nLOCAL_ONLY=1\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-migrate",
		Name:    "git-sync-migrate",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	gitEnv := "TOKEN=git\nREMOTE_ONLY=1\n"
	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", &gitEnv, nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	gitSourceBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env.git"))
	require.NoError(t, readErr)
	assert.Equal(t, gitEnv, string(gitSourceBytes))

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "LOCAL_ONLY=1\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "TOKEN=git\n")
	assert.Contains(t, string(effectiveBytes), "LOCAL_ONLY=1\n")
	assert.Contains(t, string(effectiveBytes), "REMOTE_ONLY=1\n")
}

func TestProjectService_ApplyGitSyncProjectFiles_PreservesGitEnvSyntax(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-env-syntax"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-env-syntax",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    environment:
      CLOUDFLARE_CLIENT_SECRET: ${CLOUDFLARE_CLIENT_SECRET:?set in env}
`
	gitEnv := "# keep git formatting\nZ_LAST=last\nCLOUDFLARE_CLIENT_SECRET=$$pbkdf2-sha512$$310000$$XXX\nQUOTED_SECRET='$pbkdf2-sha512$310000$XXX'\nA_FIRST=first"

	for range 2 {
		updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, compose, &gitEnv, nil, "", common.User{
			ID:       "u1",
			Username: "tester",
		})
		require.NoError(t, err)
		require.NotNil(t, updated)

		effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
		require.NoError(t, readErr)
		assert.Equal(t, gitEnv, string(effectiveBytes))

		gitBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env.git"))
		require.NoError(t, readErr)
		assert.Equal(t, gitEnv, string(gitBytes))

		_, statErr := os.Stat(filepath.Join(projectPath, "project.env"))
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}

	effectiveEnv, err := projects.ParseProjectEnvFile(filepath.Join(projectPath, ".env"), nil)
	require.NoError(t, err)
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", effectiveEnv["CLOUDFLARE_CLIENT_SECRET"])
	assert.Equal(t, "$pbkdf2-sha512$310000$XXX", effectiveEnv["QUOTED_SECRET"])
}

func TestProjectService_ApplyGitSyncProjectFiles_NormalizesStaleCopiedGitOverrides(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-normalize"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nSHARED=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("BASE=git\nSHARED=1\nTOKEN=local\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nSHARED=1\nTOKEN=local\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-normalize",
		Name:    "git-sync-normalize",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", new("BASE=git-updated\nSHARED=1\nREMOTE_ONLY=1\n"), nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "TOKEN=local\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "BASE=git-updated\n")
	assert.Contains(t, string(effectiveBytes), "REMOTE_ONLY=1\n")
	assert.Contains(t, string(effectiveBytes), "TOKEN=local\n")
}

func TestProjectService_ApplyGitSyncProjectFiles_RemovesLegacyDeletedGitMasks(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-delete-mask"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("TOKEN=git\nSHARED=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=\nLOCAL_ONLY=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("LOCAL_ONLY=1\nSHARED=1\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-delete-mask",
		Name:    "git-sync-delete-mask",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", new("TOKEN=git-updated\nSHARED=1\nREMOTE_ONLY=1\n"), nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "LOCAL_ONLY=1\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "TOKEN=git-updated\n")
	assert.Contains(t, string(effectiveBytes), "LOCAL_ONLY=1\n")
	assert.Contains(t, string(effectiveBytes), "REMOTE_ONLY=1\n")
	assert.NotContains(t, string(overrideBytes), "TOKEN=")
}

func TestProjectService_ApplyGitSyncProjectFiles_RemovesGitEnvSource(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-remove"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-remove",
		Name:    "git-sync-remove",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	_, statErr := os.Stat(filepath.Join(projectPath, ".env.git"))
	assert.True(t, os.IsNotExist(statErr))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "BASE=git\n", string(effectiveBytes))
}

func TestProjectService_ApplyGitSyncProjectFiles_WritesAndRemovesComposeOverride(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-override"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-override",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	overrideContent := "services:\n  app:\n    image: busybox:latest\n"
	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, new(overrideContent), "compose.override.yaml", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.override.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, overrideContent, string(overrideBytes))

	// A subsequent sync without an override removes the previously synced file.
	updated, err = svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	_, statErr := os.Stat(filepath.Join(projectPath, "compose.override.yaml"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestProjectService_ApplyGitSyncProjectFiles_UsesGlobalEnvDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-global-env"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=/srv/media\nMYPATH=/containers/\n"), 0o600))

	project := &Project{
		ID:      "proj-git-sync-global-env",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  cats:
    image: mikesir87/cats:1.0
    volumes:
      - ${DATA_NAS_FOLDER}:/data
      - ${MYPATH}cats/templates:/app/templates
`

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, compose, nil, nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	composeBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, compose, string(composeBytes))
}

// TestProjectService_ApplyGitSyncProjectFiles_TolerantOfUndefinedComposeVar verifies
// a git sync updating a compose file that references an undefined ${VAR} (with no
// .env supplying it yet) succeeds instead of failing compose validation with a
// typed-decode error like `strconv.ParseFloat: parsing "": invalid syntax`.
func TestProjectService_ApplyGitSyncProjectFiles_TolerantOfUndefinedComposeVar(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-undefined-var"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	compose := `services:
  app:
    image: nginx:alpine
    deploy:
      resources:
        limits:
          cpus: "${CPU}"
`

	project := &Project{
		ID:      "proj-git-sync-undefined-var",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, compose, nil, nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	composeBytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, compose, string(composeBytes))
}

func TestProjectService_PersistGitSyncEnvFiles_UsesPreparedState(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-prepared-state"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=local\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nTOKEN=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=local\n"), 0o600))

	update, err := svc.prepareGitSyncEnvUpdateInternal(projectPath, new("BASE=git-updated\nTOKEN=git\nREMOTE=1\n"))
	require.NoError(t, err)
	require.NotNil(t, update.effectiveContent)

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=unexpected\n"), 0o600))

	require.NoError(t, persistGitSyncEnvFilesInternal(t.Context(), projectPath, projectsDir, update))

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "TOKEN=local\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "BASE=git-updated\nTOKEN=local\nREMOTE=1\n", string(effectiveBytes))
}

func TestProjectService_GetProjectDetails_ReturnsEffectiveEnvContent(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "details-override"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=secret\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=secret\n"), 0o600))

	project := &Project{
		ID:      "proj-details-override",
		Name:    "details-override",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
	require.NoError(t, err)
	assert.Equal(t, "BASE=git\nTOKEN=secret\n", details.EnvContent)
}

func TestBuildProjectUpdateInfoSummaryInternal(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		imageRefs   []string
		updates     map[string]*imagetypes.UpdateInfo
		wantStatus  string
		wantCount   int
		wantChecked int
		wantErrors  int
		wantUpdates int
		wantError   string
	}{
		{
			name:        "unknown when no checks exist",
			imageRefs:   []string{"nginx:latest"},
			updates:     nil,
			wantStatus:  "unknown",
			wantCount:   1,
			wantChecked: 0,
			wantErrors:  0,
			wantUpdates: 0,
		},
		{
			name:      "has update when any image has update",
			imageRefs: []string{"nginx:latest", "redis:7"},
			updates: map[string]*imagetypes.UpdateInfo{
				"nginx:latest": {HasUpdate: true, CheckTime: now},
				"redis:7":      {HasUpdate: false, CheckTime: now.Add(-time.Minute)},
			},
			wantStatus:  "has_update",
			wantCount:   2,
			wantChecked: 2,
			wantErrors:  0,
			wantUpdates: 1,
		},
		{
			name:      "error when no updates but a check failed",
			imageRefs: []string{"nginx:latest"},
			updates: map[string]*imagetypes.UpdateInfo{
				"nginx:latest": {HasUpdate: false, CheckTime: now, Error: "rate limited"},
			},
			wantStatus:  "error",
			wantCount:   1,
			wantChecked: 1,
			wantErrors:  1,
			wantUpdates: 0,
			wantError:   "rate limited",
		},
		{
			name:      "up to date when all images checked without updates",
			imageRefs: []string{"nginx:latest", "redis:7"},
			updates: map[string]*imagetypes.UpdateInfo{
				"nginx:latest": {HasUpdate: false, CheckTime: now},
				"redis:7":      {HasUpdate: false, CheckTime: now.Add(-time.Minute)},
			},
			wantStatus:  "up_to_date",
			wantCount:   2,
			wantChecked: 2,
			wantErrors:  0,
			wantUpdates: 0,
		},
		{
			name:      "not pulled when an image is missing locally and nothing else needs attention",
			imageRefs: []string{"nginx:latest", "redis:7"},
			updates: map[string]*imagetypes.UpdateInfo{
				"nginx:latest": {HasUpdate: false, UpdateType: imageupdate.UpdateTypeNotPulled, CheckTime: now},
				"redis:7":      {HasUpdate: false, CheckTime: now.Add(-time.Minute)},
			},
			wantStatus:  "not_pulled",
			wantCount:   2,
			wantChecked: 2,
			wantErrors:  0,
			wantUpdates: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildProjectUpdateInfoSummaryInternal(tt.imageRefs, tt.updates)
			require.NotNil(t, summary)
			assert.Equal(t, tt.wantStatus, summary.Status)
			assert.Equal(t, tt.wantCount, summary.ImageCount)
			assert.Equal(t, tt.wantChecked, summary.CheckedImageCount)
			assert.Equal(t, tt.wantErrors, summary.ErrorCount)
			assert.Equal(t, tt.wantUpdates, summary.ImagesWithUpdates)
			assert.Equal(t, tt.wantUpdates > 0, summary.HasUpdate)
			assert.Equal(t, tt.imageRefs, summary.ImageRefs)
			if tt.wantError != "" {
				require.NotNil(t, summary.ErrorMessage)
				assert.Equal(t, tt.wantError, *summary.ErrorMessage)
			} else {
				assert.Nil(t, summary.ErrorMessage)
			}
			if tt.wantUpdates > 0 {
				assert.Equal(t, []string{tt.imageRefs[0]}, summary.UpdatedImageRefs)
			} else {
				assert.Empty(t, summary.UpdatedImageRefs)
			}
		})
	}
}

func TestProjectService_GetProjectDetails_IncludesUpdateInfo(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageService := image.NewImageService(db, nil, nil, nil, nil, nil)
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, nil, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, "updates-demo")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:latest\n"), 0o644))

	projectRecord := &Project{
		ID:      "proj-update-info",
		Name:    "updates-demo",
		DirName: new("updates-demo"),
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:update-demo",
		Repository:     "docker.io/library/nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     "digest",
		CurrentVersion: "latest",
		CheckTime:      time.Now().UTC(),
	}).Error)

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID, projecttypes.AllDetails())
	require.NoError(t, err)
	require.NotNil(t, details.UpdateInfo)
	assert.Equal(t, "has_update", details.UpdateInfo.Status)
	assert.True(t, details.UpdateInfo.HasUpdate)
	assert.Equal(t, 1, details.UpdateInfo.ImageCount)
	assert.Equal(t, 1, details.UpdateInfo.CheckedImageCount)
	assert.Equal(t, 1, details.UpdateInfo.ImagesWithUpdates)
	assert.Equal(t, []string{"nginx:latest"}, details.UpdateInfo.ImageRefs)
	assert.Equal(t, []string{"nginx:latest"}, details.UpdateInfo.UpdatedImageRefs)
}

func TestProjectService_GetProjectDetails_RefreshesRuntimeStatusWithoutRuntimeServices(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	projectPath := createComposeProjectDir(t, projectsDir, "projectA")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  server:\n    image: nginx:alpine\n  worker:\n    image: busybox:latest\n"), 0o644))

	server := newProjectRuntimeDockerServerInternal(t, []container.Summary{
		{
			ID:     "server-container",
			Names:  []string{"/projecta-server-1"},
			Image:  "nginx:alpine",
			State:  container.StateRunning,
			Status: "Up 30 seconds (healthy)",
			Ports: []container.PortSummary{
				{
					IP:          netip.MustParseAddr("0.0.0.0"),
					PrivatePort: 80,
					PublicPort:  8080,
					Type:        "tcp",
				},
			},
			Labels: map[string]string{
				composeapi.ProjectLabel:    "projecta",
				composeapi.ServiceLabel:    "server",
				composeapi.ConfigHashLabel: "server-hash",
				composeapi.WorkingDirLabel: "/host/path/projects/projectA",
			},
		},
		{
			ID:     "worker-container",
			Names:  []string{"/projecta-worker-1"},
			Image:  "busybox:latest",
			State:  container.StateRunning,
			Status: "Up 30 seconds",
			Labels: map[string]string{
				composeapi.ProjectLabel:    "projecta",
				composeapi.ServiceLabel:    "worker",
				composeapi.ConfigHashLabel: "worker-hash",
				composeapi.WorkingDirLabel: "/host/path/projects/projectA",
			},
		},
	})
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))

	projectRecord := &Project{
		ID:           "proj-runtime-refresh",
		Name:         "projectA",
		DirName:      new("projectA"),
		Path:         projectPath,
		Status:       ProjectStatusStopped,
		ServiceCount: 2,
		RunningCount: 0,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID, projecttypes.DetailsOptions{})
	require.NoError(t, err)
	assert.Equal(t, string(ProjectStatusRunning), details.Status)
	assert.Equal(t, 2, details.ServiceCount)
	assert.Equal(t, 2, details.RunningCount)
	assert.Empty(t, details.RuntimeServices)
}

func TestProjectService_GetProjectDetails_PopulatesRuntimeServicesFromComposePs(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	projectPath := createComposeProjectDir(t, projectsDir, "projectA")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  server:\n    image: nginx:alpine\n"), 0o644))

	server := newProjectRuntimeDockerServerInternal(t, []container.Summary{
		{
			ID:     "server-container",
			Names:  []string{"/projecta-server-1"},
			Image:  "nginx:alpine",
			State:  container.StateRunning,
			Status: "Up 30 seconds",
			Labels: map[string]string{
				composeapi.ProjectLabel:    "projecta",
				composeapi.ServiceLabel:    "server",
				composeapi.ConfigHashLabel: "server-hash",
				composeapi.WorkingDirLabel: "/host/path/projects/projectA",
			},
		},
	})
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))

	projectRecord := &Project{
		ID:           "proj-runtime-services",
		Name:         "projectA",
		DirName:      new("projectA"),
		Path:         projectPath,
		Status:       ProjectStatusStopped,
		ServiceCount: 1,
		RunningCount: 0,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID, projecttypes.DetailsOptions{IncludeRuntimeServices: true})
	require.NoError(t, err)
	require.Len(t, details.RuntimeServices, 1)
	assert.Equal(t, string(ProjectStatusRunning), details.Status)
	assert.Equal(t, "server", details.RuntimeServices[0].Name)
	assert.Equal(t, "running", details.RuntimeServices[0].Status)
	assert.Equal(t, "server-container", details.RuntimeServices[0].ContainerID)
	assert.Equal(t, "projecta-server-1", details.RuntimeServices[0].ContainerName)
}

func TestProjectService_ListProjects_FiltersByUpdateStatus(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageService := image.NewImageService(db, nil, nil, nil, nil, nil)
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, nil, nil, config.Load())

	updatedPath := createComposeProjectDir(t, projectsDir, "updated-demo")
	require.NoError(t, os.WriteFile(filepath.Join(updatedPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:latest\n"), 0o644))
	upToDatePath := createComposeProjectDir(t, projectsDir, "current-demo")
	require.NoError(t, os.WriteFile(filepath.Join(upToDatePath, "compose.yaml"), []byte("services:\n  app:\n    image: redis:7\n"), 0o644))
	errorPath := createComposeProjectDir(t, projectsDir, "error-demo")
	require.NoError(t, os.WriteFile(filepath.Join(errorPath, "compose.yaml"), []byte("services:\n  app:\n    image: busybox:latest\n"), 0o644))
	unknownPath := createComposeProjectDir(t, projectsDir, "unknown-demo")
	require.NoError(t, os.WriteFile(filepath.Join(unknownPath, "compose.yaml"), []byte("services:\n  app:\n    image: alpine:latest\n"), 0o644))

	require.NoError(t, db.Create(&Project{
		ID:      "project-updated",
		Name:    "updated-demo",
		DirName: new("updated-demo"),
		Path:    updatedPath,
		Status:  ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&Project{
		ID:      "project-current",
		Name:    "current-demo",
		DirName: new("current-demo"),
		Path:    upToDatePath,
		Status:  ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&Project{
		ID:      "project-error",
		Name:    "error-demo",
		DirName: new("error-demo"),
		Path:    errorPath,
		Status:  ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&Project{
		ID:      "project-unknown",
		Name:    "unknown-demo",
		DirName: new("unknown-demo"),
		Path:    unknownPath,
		Status:  ProjectStatusStopped,
	}).Error)

	now := time.Now().UTC()
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:updated-image",
		Repository:     "docker.io/library/nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     "digest",
		CurrentVersion: "latest",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:current-image",
		Repository:     "docker.io/library/redis",
		Tag:            "7",
		HasUpdate:      false,
		UpdateType:     "tag",
		CurrentVersion: "7",
		CheckTime:      now.Add(-time.Minute),
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:error-image",
		Repository:     "docker.io/library/busybox",
		Tag:            "latest",
		HasUpdate:      false,
		UpdateType:     "error",
		CurrentVersion: "latest",
		CheckTime:      now.Add(-2 * time.Minute),
		LastError:      new("registry timeout"),
	}).Error)

	tests := []struct {
		name     string
		filter   string
		expected []string
	}{
		{name: "has update", filter: "has_update", expected: []string{"updated-demo"}},
		{name: "up to date", filter: "up_to_date", expected: []string{"current-demo"}},
		{name: "error", filter: "error", expected: []string{"error-demo"}},
		{name: "unknown", filter: "unknown", expected: []string{"unknown-demo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
				Filters: map[string]string{
					"updates": tt.filter,
				},
				Limit: -1,
				Sort:  "name", Order: pagination.SortAsc,
			})
			require.NoError(t, err)
			require.EqualValues(t, len(tt.expected), page.TotalItems)

			names := make([]string, 0, len(items))
			for _, item := range items {
				names = append(names, item.Name)
			}
			assert.Equal(t, tt.expected, names)
		})
	}
}

func TestBuildDiscoveredComposeProjectUpdateRowsInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	imageService := image.NewImageService(db, nil, nil, nil, nil, nil)
	now := time.Now().UTC()

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:media-image",
		Repository:     "docker.io/library/nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:known-image",
		Repository:     "docker.io/library/redis",
		Tag:            "7",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "7",
		CheckTime:      now,
	}).Error)
	rows := buildDiscoveredComposeProjectUpdateRowsInternal(ctx, []container.Summary{
		{
			ID:      "media-web",
			Names:   []string{"/media-web-1"},
			Image:   "nginx:latest",
			ImageID: "sha256:media-image",
			State:   "running",
			Labels: map[string]string{
				"com.docker.compose.project": "media",
				"com.docker.compose.service": "web",
			},
		},
		{
			ID:      "known-cache",
			Image:   "redis:7",
			ImageID: "sha256:known-image",
			State:   "running",
			Labels: map[string]string{
				"com.docker.compose.project": "known",
				"com.docker.compose.service": "cache",
			},
		},
		{
			ID:      "plain-container",
			Image:   "busybox:latest",
			ImageID: "sha256:plain-image",
			State:   "running",
			Labels:  map[string]string{},
		},
	}, map[string]struct{}{"known": {}}, imageService, iconcatalog.DefaultCatalog)

	require.Len(t, rows, 1)
	row := rows[0]
	assert.Equal(t, "compose:media", row.ID)
	assert.Equal(t, "media", row.Name)
	assert.True(t, row.IsDiscovered)
	assert.Equal(t, string(ProjectStatusRunning), row.Status)
	require.NotNil(t, row.UpdateInfo)
	assert.True(t, row.UpdateInfo.HasUpdate)
	assert.Equal(t, []string{"nginx:latest"}, row.UpdateInfo.UpdatedImageRefs)
	require.Len(t, row.RuntimeServices, 1)
	assert.Equal(t, "web", row.RuntimeServices[0].Name)
}

func TestBuildDiscoveredComposeProjectUpdateRowsInternal_FallsBackToImageID(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	imageService := image.NewImageService(db, nil, nil, nil, nil, nil)

	require.NoError(t, db.Create(&imageupdate.ImageUpdateRecord{
		ID:             "sha256:media-image",
		Repository:     "registry.example.test/custom/media",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     imageupdate.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      time.Now().UTC(),
	}).Error)

	rows := buildDiscoveredComposeProjectUpdateRowsInternal(ctx, []container.Summary{
		{
			ID:      "media-web",
			Image:   "nginx:latest",
			ImageID: "sha256:media-image",
			State:   "running",
			Labels: map[string]string{
				"com.docker.compose.project": "media",
				"com.docker.compose.service": "web",
			},
		},
	}, map[string]struct{}{}, imageService, iconcatalog.DefaultCatalog)

	require.Len(t, rows, 1)
	assert.Equal(t, []string{"nginx:latest"}, rows[0].UpdateInfo.UpdatedImageRefs)
}

func TestProjectService_ListProjects_FiltersArchivedProjects(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	activePath := createComposeProjectDir(t, projectsRoot, "active-demo")
	archivedPath := createComposeProjectDir(t, projectsRoot, "archived-demo")
	require.NoError(t, db.Create(&Project{
		ID:      "project-active",
		Name:    "active-demo",
		DirName: new("active-demo"),
		Path:    activePath,
		Status:  ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&Project{
		ID:         "project-archived",
		Name:       "archived-demo",
		DirName:    new("archived-demo"),
		Path:       archivedPath,
		Status:     ProjectStatusStopped,
		IsArchived: true,
		ArchivedAt: new(time.Now().UTC()),
	}).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Limit: -1,
		Sort:  "name", Order: pagination.SortAsc,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "active-demo", items[0].Name)

	items, page, err = svc.ListProjects(ctx, pagination.QueryParams{
		Filters: map[string]string{"archived": "true"},
		Limit:   -1,
		Sort:    "name", Order: pagination.SortAsc,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "archived-demo", items[0].Name)
	assert.True(t, items[0].IsArchived)

	items, page, err = svc.ListProjects(ctx, pagination.QueryParams{
		Filters: map[string]string{"archived": "all"},
		Limit:   -1,
		Sort:    "name", Order: pagination.SortAsc,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, items, 2)
	assert.Equal(t, []string{"active-demo", "archived-demo"}, []string{items[0].Name, items[1].Name})
}

func TestProjectService_ArchiveProject_RequiresStoppedProject(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsRoot := t.TempDir()
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "running-demo")
	// DB row says stopped; the live Docker state below is what must block the archive.
	require.NoError(t, db.Create(&Project{
		ID:      "project-running",
		Name:    "running-demo",
		DirName: new("running-demo"),
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}).Error)

	configureProjectRuntimeDockerInternal(t, []container.Summary{
		{
			ID:     "app-container",
			Names:  []string{"/running-demo-app-1"},
			Image:  "nginx:alpine",
			State:  container.StateRunning,
			Status: "Up 30 seconds",
			Labels: map[string]string{
				composeapi.ProjectLabel:    "running-demo",
				composeapi.ServiceLabel:    "app",
				composeapi.ConfigHashLabel: "app-hash",
				composeapi.WorkingDirLabel: "/host/path/projects/running-demo",
			},
		},
	})

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	err = svc.ArchiveProject(ctx, "project-running", common.User{ID: "user-1", Username: "tester"})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrProjectMustBeStopped)

	var stored Project
	require.NoError(t, db.First(&stored, "id = ?", "project-running").Error)
	assert.False(t, stored.IsArchived)
	assert.Nil(t, stored.ArchivedAt)
}

func TestProjectService_ArchiveProject_TogglesArchiveFlag(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsRoot := t.TempDir()
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "stopped-demo")
	require.NoError(t, db.Create(&Project{
		ID:      "project-stopped",
		Name:    "stopped-demo",
		DirName: new("stopped-demo"),
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}).Error)

	configureProjectRuntimeDockerInternal(t, nil)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	user := common.User{ID: "user-1", Username: "tester"}

	require.NoError(t, svc.ArchiveProject(ctx, "project-stopped", user))
	var stored Project
	require.NoError(t, db.First(&stored, "id = ?", "project-stopped").Error)
	assert.True(t, stored.IsArchived)
	assert.NotNil(t, stored.ArchivedAt)

	require.NoError(t, svc.UnarchiveProject(ctx, "project-stopped", user))
	var unarchived Project
	require.NoError(t, db.First(&unarchived, "id = ?", "project-stopped").Error)
	assert.False(t, unarchived.IsArchived)
	assert.Nil(t, unarchived.ArchivedAt)
}

func TestProjectService_ArchiveProject_LiveVerificationErrorPolicy(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsRoot := t.TempDir()
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	unreachablePath := createComposeProjectDir(t, projectsRoot, "unreachable-demo")
	require.NoError(t, db.Create(&Project{
		ID:      "project-unreachable",
		Name:    "unreachable-demo",
		DirName: new("unreachable-demo"),
		Path:    unreachablePath,
		Status:  ProjectStatusStopped,
	}).Error)

	noComposePath := filepath.Join(projectsRoot, "no-compose-demo")
	require.NoError(t, os.MkdirAll(noComposePath, 0o755))
	require.NoError(t, db.Create(&Project{
		ID:      "project-no-compose",
		Name:    "no-compose-demo",
		DirName: new("no-compose-demo"),
		Path:    noComposePath,
		Status:  ProjectStatusStopped,
	}).Error)

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	user := common.User{ID: "user-1", Username: "tester"}

	err = svc.ArchiveProject(ctx, "project-unreachable", user)
	require.Error(t, err)
	var stored Project
	require.NoError(t, db.First(&stored, "id = ?", "project-unreachable").Error)
	assert.False(t, stored.IsArchived)

	require.NoError(t, svc.ArchiveProject(ctx, "project-no-compose", user))
	var archived Project
	require.NoError(t, db.First(&archived, "id = ?", "project-no-compose").Error)
	assert.True(t, archived.IsArchived)
}

func TestProjectService_MapProjectToDto_SetsRedeployDisabledFromRuntimeServices(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "arcane")
	now := time.Now()
	proj := Project{
		Name:         "arcane",
		Path:         projectPath,
		ServiceCount: 1,
		ID:           "project-arcane",
		CreatedAt:    now,
		UpdatedAt:    &now,
	}

	tests := []struct {
		name               string
		containerID        string
		currentContainerID string
		currentErr         error
		labels             map[string]string
		wantProject        bool
		wantService        bool
	}{
		{
			name:               "current Arcane server container disables project redeploy",
			containerID:        "arcane1234567890",
			currentContainerID: "arcane1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "server",
				labels.LabelArcane:           "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "current legacy Arcane server container disables project redeploy",
			containerID:        "arcane1234567890",
			currentContainerID: "arcane1234567890",
			labels: map[string]string{
				"com.docker.compose.project":   "arcane",
				"com.docker.compose.service":   "server",
				labels.LabelArcaneLegacyServer: "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:        "Arcane server container fails closed when current container is unavailable",
			containerID: "arcane1234567890",
			currentErr:  errors.New("not running in docker"),
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "server",
				labels.LabelArcane:           "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "Arcane agent container stays redeployable",
			containerID:        "agent1234567890",
			currentContainerID: "agent1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "agent",
				labels.LabelArcane:           "true",
				labels.LabelArcaneAgent:      "true",
			},
		},
		{
			name:               "non Arcane container stays redeployable",
			containerID:        "regular1234567890",
			currentContainerID: "regular1234567890",
			labels: map[string]string{
				"com.docker.compose.project": "arcane",
				"com.docker.compose.service": "postgres",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &ProjectService{}
			details := service.mapProjectToDto(context.Background(), filepath.Dir(projectPath), proj, map[string][]container.Summary{
				"arcane": {
					{
						ID:     tt.containerID,
						Image:  "ghcr.io/getarcaneapp/arcane:latest",
						State:  "running",
						Status: "Up",
						Names:  []string{"/arcane-server"},
						Labels: tt.labels,
					},
				},
			}, tt.currentContainerID, tt.currentErr, nil)

			require.Equal(t, tt.wantProject, details.RedeployDisabled)
			require.Len(t, details.RuntimeServices, 1)
			require.Equal(t, tt.wantService, details.RuntimeServices[0].RedeployDisabled)
		})
	}
}

func TestProjectService_ListProjects_WithDerivedStatusFilter_AllowsAllPageSizeSentinel(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	for i := range 25 {
		projectPath := createComposeProjectDir(t, projectsRoot, fmt.Sprintf("stopped-%02d", i))
		require.NoError(t, db.Create(&Project{
			ID:      fmt.Sprintf("project-%02d", i),
			Name:    fmt.Sprintf("stopped-%02d", i),
			DirName: new(fmt.Sprintf("stopped-%02d", i)),
			Path:    projectPath,
			Status:  ProjectStatusStopped,
		}).Error)
	}

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Filters: map[string]string{
			"status": string(ProjectStatusStopped),
		},
		Limit: -1,
		Sort:  "name", Order: pagination.SortAsc,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 25, page.TotalItems)
	require.Len(t, items, 25)
	assert.Equal(t, "stopped-00", items[0].Name)
	assert.Equal(t, "stopped-24", items[len(items)-1].Name)
}

func TestProjectService_PrepareServiceBuildRequest_MapsComposeFields(t *testing.T) {
	svc := &ProjectService{}
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:     "web",
		Image:    "example/web:latest",
		Platform: "linux/amd64",
		Build: &composetypes.BuildConfig{
			Context:    ".",
			Dockerfile: "Dockerfile.custom",
			Target:     "prod",
			Args: composetypes.MappingWithEquals{
				"FOO": new("bar"),
			},
			Tags:      []string{"example/web:sha", "example/web:latest"},
			CacheFrom: []string{"example/cache:latest"},
			CacheTo:   []string{"type=local,dest=/tmp/cache"},
			NoCache:   true,
			Pull:      true,
			Network:   "host",
			Isolation: "default",
			ShmSize:   composetypes.UnitBytes(64 * 1024 * 1024),
			Ulimits: map[string]*composetypes.UlimitsConfig{
				"nofile": {Soft: 1024, Hard: 2048},
			},
			Entitlements: []string{"network.host"},
			Privileged:   true,
			ExtraHosts: composetypes.HostsList{
				"registry.local": {"10.0.0.5"},
			},
			Labels: composetypes.Labels{
				"com.example.team": "platform",
			},
		},
	}

	req, _, _, err := svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/project", req.ContextDir)
	assert.Equal(t, "Dockerfile.custom", req.Dockerfile)
	assert.Equal(t, "prod", req.Target)
	assert.Equal(t, map[string]string{"FOO": "bar"}, req.BuildArgs)
	assert.Equal(t, []string{"example/web:latest", "example/web:sha"}, req.Tags)
	assert.Equal(t, []string{"linux/amd64"}, req.Platforms)
	assert.Equal(t, []string{"example/cache:latest"}, req.CacheFrom)
	assert.Equal(t, []string{"type=local,dest=/tmp/cache"}, req.CacheTo)
	assert.True(t, req.NoCache)
	assert.True(t, req.Pull)
	assert.Equal(t, "host", req.Network)
	assert.Equal(t, "default", req.Isolation)
	assert.Equal(t, int64(64*1024*1024), req.ShmSize)
	assert.Equal(t, map[string]string{"nofile": "1024:2048"}, req.Ulimits)
	assert.Equal(t, []string{"network.host"}, req.Entitlements)
	assert.True(t, req.Privileged)
	assert.Equal(t, map[string]string{"com.example.team": "platform"}, req.Labels)
	require.Len(t, req.ExtraHosts, 1)
	assert.Contains(t, req.ExtraHosts[0], "registry.local")
	assert.Contains(t, req.ExtraHosts[0], "10.0.0.5")
}

// TestProjectService_PrepareServiceBuildRequest_KeepsContainerPaths is a
// regression test for #2314: Arcane's local build pipeline (the docker and
// buildkit providers both read the build context via the Arcane process's own
// filesystem) cannot use host paths, so prepareServiceBuildRequest must leave
// the build context and any absolute Dockerfile path as container paths even
// when the projects mount has a non-matching host prefix.
func TestProjectService_PrepareServiceBuildRequest_KeepsContainerPaths(t *testing.T) {
	svc := &ProjectService{}
	proj := &composetypes.Project{WorkingDir: "/app/data/projects/demo", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "web",
		Image: "example/web:latest",
		Build: &composetypes.BuildConfig{
			Context:    ".",
			Dockerfile: "/app/data/projects/demo/Dockerfile.custom",
		},
	}

	req, _, _, err := svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, "/app/data/projects/demo", req.ContextDir)
	assert.Equal(t, "/app/data/projects/demo/Dockerfile.custom", req.Dockerfile)
}

// TestProjectService_PrepareServiceBuildRequest_BuildDotKeepsContainerPath
// reproduces the exact configuration from #2314: a compose file with
// `build: .` next to its Dockerfile, on an installation where the projects
// directory is bind-mounted from a different host path than the container
// path. The resulting BuildRequest must point at the container path so the
// local builder can stat / tar the directory.
func TestProjectService_PrepareServiceBuildRequest_BuildDotKeepsContainerPath(t *testing.T) {
	svc := &ProjectService{}
	proj := &composetypes.Project{WorkingDir: "/app/data/projects/caddy", Name: "caddy"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "caddy",
		Image: "caddy",
		Build: &composetypes.BuildConfig{
			Context: ".",
		},
	}

	req, _, _, err := svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"caddy",
		serviceCfg,
		ProjectBuildOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, "/app/data/projects/caddy", req.ContextDir)
	assert.Equal(t, "Dockerfile", req.Dockerfile)
}

func TestProjectService_PrepareServiceBuildRequest_UsesInlineDockerfile(t *testing.T) {
	svc := &ProjectService{}
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name:  "web",
		Image: "example/web:latest",
		Build: &composetypes.BuildConfig{
			Context:          ".",
			DockerfileInline: "FROM alpine:3.20\nRUN echo inline\n",
		},
	}

	req, _, _, err := svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{},
	)
	require.NoError(t, err)

	assert.Equal(t, "/tmp/project", req.ContextDir)
	assert.Empty(t, req.Dockerfile)
	assert.Equal(t, "FROM alpine:3.20\nRUN echo inline\n", req.DockerfileInline)
}

func TestProjectService_PrepareServiceBuildRequest_GeneratedImageProviderGuardrails(t *testing.T) {
	svc := &ProjectService{}
	proj := &composetypes.Project{WorkingDir: "/tmp/project", Name: "demo"}

	serviceCfg := composetypes.ServiceConfig{
		Name: "web",
		Build: &composetypes.BuildConfig{
			Context: ".",
		},
	}

	_, _, _, err := svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{Provider: "depot"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define an image when using depot")

	_, _, _, err = svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{Provider: "local", Push: new(true)},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define an image when push is enabled")
}

func TestProjectService_DeployProject_StopsOnBuildPreparationError(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	composeContent := "services:\n" +
		"  web:\n" +
		"    pull_policy: build\n" +
		"    build:\n" +
		"      context: .\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot+":"+projectsRoot))

	proj := &Project{
		ID:     "p1",
		Name:   "demo",
		Path:   projectDir,
		Status: ProjectStatusStopped,
	}
	require.NoError(t, db.Create(proj).Error)

	buildSvc := testBuildBuilder{err: errors.New("boom build")}
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, nil, nil, config.Load())

	err = svc.DeployProject(ctx, "p1", common.User{ID: "u1", Username: "tester"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare project images for deploy")
	assert.Contains(t, err.Error(), "boom build")

	var updated Project
	require.NoError(t, db.First(&updated, "id = ?", "p1").Error)
	assert.Equal(t, ProjectStatusStopped, updated.Status)
}

func TestProjectService_DeployProject_BuildsGeneratedImageWithoutPull(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	composeContent := "services:\n" +
		"  caddy:\n" +
		"    build:\n" +
		"      dockerfile_inline: |\n" +
		"        FROM caddy:builder AS builder\n" +
		"        RUN xcaddy build --with github.com/caddyserver/replace-response\n" +
		"\n" +
		"        FROM caddy:latest\n" +
		"        COPY --from=builder /usr/bin/caddy /usr/bin/caddy\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot+":"+projectsRoot))

	proj := &Project{
		ID:     "p-generated",
		Name:   "build-test",
		Path:   projectDir,
		Status: ProjectStatusStopped,
	}
	require.NoError(t, db.Create(proj).Error)

	buildSvc := testBuildBuilder{err: errors.New("boom build")}
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, nil, nil, config.Load())

	err = svc.DeployProject(ctx, proj.ID, common.User{ID: "u1", Username: "tester"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare project images for deploy")
	assert.Contains(t, err.Error(), "boom build")
	assert.NotContains(t, err.Error(), "failed to pull image arcane.local/")
	assert.NotContains(t, err.Error(), "failed to resolve reference \"arcane.local/")
}

func TestProjectService_SyncProjectsFromFileSystem_IgnoresSymlinkedProjectDirsWhenDisabled(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "regular")
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	require.NoError(t, os.Symlink(linkTarget, filepath.Join(projectsRoot, "linked")))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "false"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "regular", items[0].Name)
	assert.Equal(t, filepath.Join(projectsRoot, "regular"), items[0].Path)
}

func TestProjectService_SyncProjectsFromFileSystem_DetectsSymlinkedProjectDirsWhenEnabled(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	linkPath := filepath.Join(projectsRoot, "linked")
	require.NoError(t, os.Symlink(linkTarget, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "linked", items[0].Name)
	assert.Equal(t, linkPath, items[0].Path)
}

func TestProjectService_CountProjectFolders_RespectsFollowProjectSymlinks(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "regular")
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	require.NoError(t, os.Symlink(linkTarget, filepath.Join(projectsRoot, "linked")))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "false"))
	count, err := svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))
	count, err = svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestProjectService_SyncProjectsFromFileSystem_DiscoversNestedProjectsAndRelativePaths(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	nestedPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project2")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Sort: "path", Order: pagination.SortAsc,
		Limit: -1,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.TotalItems)
	require.Len(t, items, 2)

	assert.Equal(t, "main-project/sub-project1", items[0].RelativePath)
	assert.Equal(t, nestedPath, items[0].Path)
	assert.Equal(t, "sub-project1", items[0].DirName)

	assert.Equal(t, "project2", items[1].RelativePath)
	assert.Equal(t, topLevelPath, items[1].Path)
	assert.Equal(t, "project2", items[1].DirName)
}

func TestProjectService_SyncProjectsFromFileSystem_RespectsConfiguredScanMaxDepth(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project1")
	createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "project1", items[0].Name)
	assert.Equal(t, topLevelPath, items[0].Path)
}

func TestProjectService_ListProjects_LoadsProjectIconFromGlobalEnvInIncludedMetadata(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(projectsRoot, projects.GlobalEnvFileName),
		[]byte("ICON_CDN_URL=https://cdn.jsdelivr.net/gh/selfhst/icons@main\n"),
		0o600,
	))

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(`include:
  - metadata.yaml
services:
  watchtower:
    image: nickfedor/watchtower:latest
`), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "metadata.yaml"), []byte(`x-watchtower-icon-light: &watchtower-icon "${ICON_CDN_URL:+${ICON_CDN_URL}/svg/watchtower.svg}"
x-arcane:
  icon-light: *watchtower-icon
  icon-dark: *watchtower-icon
services:
  watchtower:
    labels:
      com.getarcaneapp.arcane.icon-light: *watchtower-icon
      com.getarcaneapp.arcane.icon-dark: *watchtower-icon
`), 0o600))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Sort: "path", Order: pagination.SortAsc,
		Limit: -1,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", items[0].IconLightURL)
	assert.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", items[0].IconDarkURL)
}

func TestProjectService_CountProjectFolders_RecursivelyCountsNestedProjects(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))
	createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project2"))
	createComposeProjectDir(t, projectsRoot, "project2")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	count, err := svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestProjectService_CountProjectFolders_RespectsConfiguredScanMaxDepth(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "project1")
	createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	count, err := svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestProjectService_SyncProjectsFromFileSystem_RemovesDeletedNestedProject(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, os.Remove(filepath.Join(projectPath, "compose.yaml")))
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err = svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Empty(t, items)
}

// TestProjectService_SyncProjectsFromFileSystem_PrunesLeakedScratchRow verifies a
// phantom project row imported from a leaked gitops scratch dir is pruned and not
// re-imported, while a real project is kept. This closes the "deleted projects
// resurrected on restart" loop for gitops scratch leftovers.
func TestProjectService_SyncProjectsFromFileSystem_PrunesLeakedScratchRow(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	createComposeProjectDir(t, projectsRoot, "app")

	scratchName := ".gitops-backup-9"
	scratchPath := createComposeProjectDir(t, projectsRoot, scratchName)
	require.NoError(t, db.Create(&Project{
		ID:      "phantom-scratch",
		Name:    scratchName,
		DirName: &scratchName,
		Path:    scratchPath,
	}).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "app", items[0].Name)
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesProjectsWhenDirectoryEmptyOrUnmounted(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// An existing-but-empty projects directory simulates a mis-mapped or unmounted
	// projects volume: GetProjectsDirectory resolves (and MkdirAll's) it, discovery
	// finds nothing, and every stored project path is now missing on disk.
	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	// Seed a small deployment's worth of filesystem-managed projects whose
	// directories do not exist. Each would individually be pruned as "directory no
	// longer exists"; together they would wipe the table — exactly what the
	// mass-wipe guard must prevent, even for deployments with only a handful of
	// projects (the guard must not give small installs a free pass).
	const seeded = 3
	for i := range seeded {
		require.NoError(t, db.WithContext(ctx).Create(&Project{
			Name:   fmt.Sprintf("project-%d", i),
			Path:   filepath.Join(projectsRoot, fmt.Sprintf("project-%d", i)),
			Status: ProjectStatusStopped,
		}).Error)
	}

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, items, seeded, "an empty/mis-mapped projects directory must not wipe existing project records")
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesProjectWithAmbiguousCustomCompose(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, "ambiguous-project")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Replace the standard compose.yaml with two custom-named compose files. The
	// directory still holds compose content, but DetectComposeFile can't pick one
	// and returns common.ErrComposeFileNotFound. The reconcile must NOT delete the
	// record: the project's files are intact on disk and it may be deployable.
	require.NoError(t, os.Remove(filepath.Join(projectPath, "compose.yaml")))
	composeBody := []byte("services:\n  app:\n    image: nginx:alpine\n")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "app.yaml"), composeBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "extra.yaml"), composeBody, 0o644))

	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err = svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 1, "project with ambiguous compose files must not be deleted")
	assert.Equal(t, projectPath, items[0].Path)
}

func TestProjectService_SyncProjectsFromFileSystem_RemovesProjectsBeyondReducedScanMaxDepth(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project1")
	nestedPath := createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	// Initial sync at the default scan depth discovers both the top-level and
	// the nested project, persisting them to the database.
	defaultSvc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, defaultSvc.SyncProjectsFromFileSystem(ctx))

	items, err := defaultSvc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)

	// Lowering the scan depth must prune the nested project from the database on
	// the next sync, even though its compose file still exists on disk.
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")
	depthLimitedSvc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, depthLimitedSvc.SyncProjectsFromFileSystem(ctx))

	items, err = depthLimitedSvc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "project1", items[0].Name)
	assert.Equal(t, topLevelPath, items[0].Path)

	// The pruned project's files must remain untouched on disk so raising the
	// depth again re-discovers it.
	assert.FileExists(t, filepath.Join(nestedPath, "compose.yaml"))
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesDBRecordsWhenDirectoryUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denied behavior is not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root UID to trigger permission-denied on ReadDir")
	}

	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "project1")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	before, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, before, 1)

	unreadable := t.TempDir()
	require.NoError(t, os.Chmod(unreadable, 0))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", unreadable))

	syncErr := svc.SyncProjectsFromFileSystem(ctx)
	require.Error(t, syncErr, "sync should propagate the permission-denied error")

	after, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, after, 1, "DB records must not be wiped when discovery fails")
	assert.Equal(t, before[0].Path, after[0].Path)
}

// TestProjectService_SyncProjectsFromFileSystem_DiscoversReadableProjectsDespiteUnreadableNestedSibling
// covers the regression from GitHub issue #3080: a permission-denied error on a
// non-root nested subdirectory used to abort discovery of ALL projects. Unlike
// TestProjectService_SyncProjectsFromFileSystem_PreservesDBRecordsWhenDirectoryUnreadable
// above (which makes the projects ROOT itself unreadable and expects sync to still
// error), this test makes only a NESTED subdirectory unreadable and expects sync to
// succeed, discover the readable sibling project, and leave alone any existing DB
// record whose path happens to live under the now-skipped unreadable subtree.
func TestProjectService_SyncProjectsFromFileSystem_DiscoversReadableProjectsDespiteUnreadableNestedSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denied behavior is not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root UID to trigger permission-denied on ReadDir")
	}

	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	project1Path := createComposeProjectDir(t, projectsRoot, "project1")

	unreadableDir := filepath.Join(projectsRoot, "adguard-cheetah", "workingdir")
	require.NoError(t, os.MkdirAll(unreadableDir, 0o755))
	t.Cleanup(func() { _ = os.Chmod(unreadableDir, 0o700) })
	require.NoError(t, os.Chmod(unreadableDir, 0))

	// Seed a DB row (bypassing the filesystem) whose Path sits under the
	// now-unreadable subtree, simulating a project Arcane previously knew about.
	strandedDirName := "stranded-project"
	require.NoError(t, db.Create(&Project{
		ID:      "stranded-under-unreadable",
		Name:    "stranded-project",
		DirName: &strandedDirName,
		Path:    filepath.Join(unreadableDir, "stranded-project"),
		Status:  ProjectStatusStopped,
	}).Error)

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx), "sync must succeed despite the unreadable nested directory")

	after, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)

	var foundReadableSibling bool
	var foundStrandedRecord bool
	for _, p := range after {
		if p.Path == project1Path {
			foundReadableSibling = true
		}
		if p.ID == "stranded-under-unreadable" {
			foundStrandedRecord = true
		}
	}

	assert.True(t, foundReadableSibling, "readable sibling project must still be discovered")
	// evaluateProjectPathErrorInternal only prunes on os.IsNotExist; a permission-denied
	// stat falls into the "keep the record" branch, so the stranded record must survive.
	assert.True(t, foundStrandedRecord, "DB record under the unreadable subtree must not be pruned by a permission-denied stat")
}

func TestProjectService_SyncProjectsFromFileSystem_AllowsDuplicateLeafDirectoriesInDifferentParents(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	firstPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project1", "app"))
	secondPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project2", "app"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var items []Project
	require.NoError(t, db.WithContext(ctx).Order("path asc").Find(&items).Error)
	require.Len(t, items, 2)

	require.NotNil(t, items[0].DirName)
	require.NotNil(t, items[1].DirName)
	assert.Equal(t, "app", *items[0].DirName)
	assert.Equal(t, "app", *items[1].DirName)
	assert.Equal(t, firstPath, items[0].Path)
	assert.Equal(t, secondPath, items[1].Path)
}

func TestProjectService_SyncProjectsFromFileSystem_DetectsNestedSymlinkedProjectDirsWhenEnabled(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := createComposeProjectDir(t, targetRoot, filepath.Join("main-project", "sub-project1"))
	linkPath := filepath.Join(projectsRoot, "linked-root")
	require.NoError(t, os.Symlink(filepath.Join(targetRoot, "main-project"), linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Sort: "path", Order: pagination.SortAsc,
		Limit: -1,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, filepath.Join(linkPath, "sub-project1"), items[0].Path)
	assert.Equal(t, "linked-root/sub-project1", items[0].RelativePath)
	assert.Equal(t, targetPath, filepath.Join(targetRoot, "main-project", "sub-project1"))
}

func TestProjectService_SyncProjectsFromFileSystem_RemovesSymlinkedProjectsWhenDisabled(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	linkPath := filepath.Join(projectsRoot, "linked")
	require.NoError(t, os.Symlink(linkTarget, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "false"))
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err = svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Empty(t, items)

	_, statErr := os.Lstat(linkPath)
	require.NoError(t, statErr)
}

func TestProjectService_SyncProjectsFromFileSystem_RefreshesServiceCountOnComposeChange(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, "demo")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var project Project
	require.NoError(t, db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error)
	assert.Equal(t, 1, project.ServiceCount)

	updatedCompose := "services:\n  app:\n    image: nginx:alpine\n  worker:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(updatedCompose), 0o644))

	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))
	require.NoError(t, db.WithContext(ctx).Where("id = ?", project.ID).First(&project).Error)
	assert.Equal(t, 2, project.ServiceCount)
}

func TestProjectService_SyncProjectsFromFileSystem_AlignsNameToEffectiveComposeName(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "aitools")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(`name: ai_tools
services:
  app:
    image: nginx:alpine
`), 0o644))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var project Project
	require.NoError(t, db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error)
	assert.Equal(t, "ai_tools", project.Name)
	require.NotNil(t, project.DirName)
	assert.Equal(t, "aitools", *project.DirName)
	require.NotNil(t, project.ComposeProjectName)
	assert.Equal(t, "ai_tools", *project.ComposeProjectName)
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesValidCustomNameWithoutExplicitComposeName(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "folder-name")
	dirName := "folder-name"
	project := &Project{
		ID:      "proj-custom-name",
		Name:    "custom-name",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var fromDB Project
	require.NoError(t, db.WithContext(ctx).Where("id = ?", project.ID).First(&fromDB).Error)
	assert.Equal(t, "custom-name", fromDB.Name)
	require.NotNil(t, fromDB.DirName)
	assert.Equal(t, "folder-name", *fromDB.DirName)
	require.Nil(t, fromDB.ComposeProjectName)
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesGitOpsProjectWithCustomComposeFilename(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&GitOpsSync{}))

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	syncProjectID := "proj-custom-compose"
	syncID := "sync-custom-compose"
	sync := &GitOpsSync{
		ID:            syncID,
		Name:          "Radarr Sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &syncProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	project := &Project{
		ID:              syncProjectID,
		Name:            "Radarr",
		DirName:         new("Radarr-3"),
		Path:            projectDir,
		Status:          ProjectStatusStopped,
		GitOpsManagedBy: &syncID,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, syncProjectID, items[0].ID)
	assert.Equal(t, projectDir, items[0].Path)
	assert.Equal(t, syncID, *items[0].GitOpsManagedBy)
}

func TestProjectService_GetProjectDetails_UsesGitOpsCustomComposeFilename(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&GitOpsSync{}))

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "Radarr-3")
	composeContent := "services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "radarr.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte("TZ=UTC\n"), 0o644))

	syncProjectID := "proj-custom-compose-details"
	syncID := "sync-custom-compose-details"
	require.NoError(t, db.Create(&GitOpsSync{
		ID:            syncID,
		Name:          "Radarr Sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &syncProjectID,
		SyncDirectory: true,
	}).Error)

	require.NoError(t, db.Create(&Project{
		ID:              syncProjectID,
		Name:            "Radarr",
		DirName:         new("Radarr-3"),
		Path:            projectDir,
		Status:          ProjectStatusStopped,
		GitOpsManagedBy: &syncID,
	}).Error)

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())

	composeFromContent, envFromContent, _, err := svc.GetProjectContent(ctx, syncProjectID)
	require.NoError(t, err)
	assert.Equal(t, composeContent, composeFromContent)
	assert.Equal(t, "TZ=UTC\n", envFromContent)

	details, err := svc.GetProjectDetails(ctx, syncProjectID, projecttypes.AllDetails())
	require.NoError(t, err)
	assert.Equal(t, "radarr.yaml", details.ComposeFileName)
	assert.Equal(t, composeContent, details.ComposeContent)
	assert.Equal(t, "TZ=UTC\n", details.EnvContent)
	assert.Len(t, details.Services, 1)
}

func TestProjectService_UpdateProject_WritesThroughSymlinkedProjectPath(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := createComposeProjectDir(t, targetRoot, "demo-target")
	linkPath := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.Symlink(targetPath, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	project := &Project{
		ID:      "proj-symlink-update",
		Name:    "demo",
		DirName: new("demo"),
		Path:    linkPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updatedCompose := "services:\n  app:\n    image: nginx:1.27-alpine\n"
	updatedEnv := "FOO=updated\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(updatedCompose), new(updatedEnv), nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, linkPath, updated.Path)

	composeBytes, readErr := os.ReadFile(filepath.Join(targetPath, "compose.yaml"))
	require.NoError(t, readErr)
	assert.Equal(t, updatedCompose, string(composeBytes))

	envBytes, readErr := os.ReadFile(filepath.Join(targetPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, updatedEnv, string(envBytes))
}

func TestProjectService_UpdateProject_WritesThroughExternalEnvSymlink(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	projectPath := createComposeProjectDir(t, projectsRoot, "demo")
	targetPath := filepath.Join(t.TempDir(), "project.env")
	targetPerm := os.FileMode(0o640)
	require.NoError(t, os.WriteFile(targetPath, []byte("FOO=original\n"), targetPerm))
	require.NoError(t, os.Chmod(targetPath, targetPerm))

	envPath := filepath.Join(projectPath, projects.EffectiveEnvFileName)
	if err := os.Symlink(targetPath, envPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	originalLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	dirName := "demo"
	project := &Project{
		ID:      "proj-external-env-symlink-update",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updatedEnv := "FOO=updated\n"
	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, &updatedEnv, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	targetContent, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, updatedEnv, string(targetContent))
	currentLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)
	assert.Equal(t, originalLinkTarget, currentLinkTarget)
	linkInfo, err := os.Lstat(envPath)
	require.NoError(t, err)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
	if runtime.GOOS != "windows" {
		targetInfo, statErr := os.Stat(targetPath)
		require.NoError(t, statErr)
		assert.Equal(t, targetPerm, targetInfo.Mode().Perm())
	}
}

func TestProjectService_UpdateProject_RestoresExternalEnvSymlinkTargetWhenProjectSaveFails(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	projectPath := createComposeProjectDir(t, projectsRoot, "demo")
	targetPath := filepath.Join(t.TempDir(), "project.env")
	originalContent := "FOO=original\n"
	targetPerm := os.FileMode(0o640)
	require.NoError(t, os.WriteFile(targetPath, []byte(originalContent), targetPerm))
	require.NoError(t, os.Chmod(targetPath, targetPerm))

	envPath := filepath.Join(projectPath, projects.EffectiveEnvFileName)
	if err := os.Symlink(targetPath, envPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	originalLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())
	dirName := "demo"
	project := &Project{
		ID:      "proj-external-env-symlink-rollback",
		Name:    dirName,
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("arcane_test_external_env_project_save_failure", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Project" {
			_ = tx.AddError(errors.New("forced project save failure"))
		}
	}))

	updatedEnv := "FOO=updated\n"
	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, &updatedEnv, nil, common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forced project save failure")

	targetContent, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(targetContent))
	currentLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)
	assert.Equal(t, originalLinkTarget, currentLinkTarget)
	linkInfo, err := os.Lstat(envPath)
	require.NoError(t, err)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
	if runtime.GOOS != "windows" {
		targetInfo, statErr := os.Stat(targetPath)
		require.NoError(t, statErr)
		assert.Equal(t, targetPerm, targetInfo.Mode().Perm())
	}
}

func createComposeProjectDir(t *testing.T, root, name string) string {
	t.Helper()

	projectPath := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o644))

	return projectPath
}

func configureProjectRuntimeDockerInternal(t *testing.T, containers []container.Summary) {
	t.Helper()

	server := newProjectRuntimeDockerServerInternal(t, containers)
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))
}

func projectUpdatePreviewDirsInternal(t *testing.T, projectsDir string) []string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(projectsDir, ".project-update-preview-*"))
	require.NoError(t, err)
	return matches
}

func newProjectRuntimeDockerServerInternal(t *testing.T, containers []container.Summary) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = io.WriteString(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/version"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"ApiVersion":    "1.41",
				"MinAPIVersion": "1.24",
				"Version":       "24.0.0",
			})
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(containers)
		case strings.Contains(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
			containerID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/containers/"):], "/containers/"), "/json")
			for _, c := range containers {
				if c.ID != containerID {
					continue
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(container.InspectResponse{
					ID: c.ID,
					State: &container.State{
						Status:  c.State,
						Running: c.State == container.StateRunning,
					},
				})
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func dockerHostFromProjectRuntimeServerURLInternal(t *testing.T, serverURL string) string {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	require.NoError(t, err)
	return "tcp://" + parsed.Host
}

func TestResolveRemoveOrphans(t *testing.T) {
	tests := []struct {
		name          string
		gitOpsManaged bool
		options       *projecttypes.DeployOptions
		want          bool
	}{
		{"non-gitops, nil options", false, nil, false},
		{"non-gitops, flag false", false, &projecttypes.DeployOptions{RemoveOrphans: false}, false},
		{"non-gitops, flag true opts in", false, &projecttypes.DeployOptions{RemoveOrphans: true}, true},
		{"gitops, nil options stays true", true, nil, true},
		{"gitops, flag false stays true", true, &projecttypes.DeployOptions{RemoveOrphans: false}, true},
		{"gitops, flag true stays true", true, &projecttypes.DeployOptions{RemoveOrphans: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, resolveRemoveOrphansInternal(tt.gitOpsManaged, tt.options))
		})
	}
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackUncommittedDirectoryRename(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-recovery",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_StartedPhaseSkipsVolumeRollback(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-started-volume-recovery",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseStartedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_RelocatesTargetWhenBothPathsExist(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "compose.yaml"), []byte("services: {}\n"), 0o600))
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-both-paths",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseStartedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	conflictPaths, err := filepath.Glob(filepath.Join(projectsDir, ".web.rename-conflict-*"))
	require.NoError(t, err)
	require.Len(t, conflictPaths, 1)
	require.FileExists(t, filepath.Join(conflictPaths[0], "compose.yaml"))

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	newName := "web"
	require.NoError(t, svc.applyProjectRenameIfNeeded(t.Context(), &fromDB, &newName, projectsDir))
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
	require.DirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoryPathsMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &Project{
		ID:      "proj-rename-missing-paths",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseStartedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsPreservedTargetJournalWhenPathExists(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &Project{
		ID:      "proj-rename-preserved-target",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	var targetRemoved bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetRemoved, "preserved target volume should remain for manual inspection")
	require.DirExists(t, oldPath)
}

func TestProjectRenameJournalTargetsCopiedInternal(t *testing.T) {
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseStartedInternal))
	require.False(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateRolledBackInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseSourceCleanupPendingInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseTargetsCopiedInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseOldVolumesRemovedInternal))
	require.True(t, projectRenameJournalTargetsCopiedInternal(projectRenameJournalPhaseProjectStateCommittedInternal))
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournal(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-committed",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseOldVolumesRemovedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, newPath)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_ClearsJournalAfterSourceCleanup(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &Project{
		ID:      "proj-rename-old-volumes-removed",
		Name:    "web",
		DirName: &newDir,
		Path:    filepath.Join(t.TempDir(), newDir),
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := &projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    filepath.Join(t.TempDir(), oldDir),
		NewPath:    project.Path,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	require.NoError(t, svc.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseTargetsCopiedInternal))

	migration := &fakeProjectVolumeRenameMigrationInternal{}
	journalActive := true
	svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive)
	require.True(t, migration.commitCalled)
	require.False(t, journalActive)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_KeepsJournalWhenSourceCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &Project{
		ID:      "proj-rename-cleanup-failure",
		Name:    "web",
		DirName: &newDir,
		Path:    filepath.Join(t.TempDir(), newDir),
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := &projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    filepath.Join(t.TempDir(), oldDir),
		NewPath:    project.Path,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
	}
	require.NoError(t, svc.writeProjectRenameJournalInternal(ctx, journal, projectRenameJournalPhaseTargetsCopiedInternal))

	migration := &fakeProjectVolumeRenameMigrationInternal{
		commitErr: volumes.NewSourceCleanupError("nginx_data", errors.New("source cleanup failed")),
	}
	journalActive := true
	svc.finalizeProjectRenameAfterCommitInternal(ctx, project.ID, migration, journal, &journalActive)
	require.True(t, migration.commitCalled)
	require.True(t, journalActive)

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
}

func TestProjectService_RecoverProjectRenameJournals_KeepsJournalWhenDirectoryRollbackFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			if targetRemoved.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, "missing-parent", oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-directory-rollback-fails",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rollback project directory rename")

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, targetRemoved.Load(), "target volume rollback should still run after directory rollback fails")
	require.FileExists(t, filepath.Join(newPath, "compose.yaml"), "failed directory rollback leaves the target path for retry")
	require.NoDirExists(t, oldPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0o755))
	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err = kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_CompletesCommittedVolumeJournalWithoutHelperImage(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var imageInspectCalled atomic.Bool
	var oldVolumeRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			oldVolumeRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/images/"):
			imageInspectCalled.Store(true)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-committed-with-volumes",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseOldVolumesRemovedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, oldVolumeRemoved.Load(), "expected committed recovery to remove source volume")
	require.False(t, imageInspectCalled.Load(), "completion recovery should not inspect or pull the copy helper image")
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackCommittedJournalWhenTargetMissingAndSourceExists(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var oldVolumeRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			oldVolumeRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-missing-target-preserve-source",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseOldVolumesRemovedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)
	require.False(t, oldVolumeRemoved.Load(), "source volume is the only remaining copy and must not be deleted")

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalAfterDBRestoreWhenVolumeRollbackFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoveAttempts atomic.Int32
	var targetExists atomic.Bool
	var allowTargetRemove atomic.Bool
	targetExists.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			if !targetExists.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			targetRemoveAttempts.Add(1)
			if allowTargetRemove.Load() {
				targetExists.Store(false)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.Error(w, "volume busy", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-rollback-volume-fail",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseProjectStateCommittedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
			{
				Key:     "cache",
				OldName: "nginx_cache",
				NewName: "web_cache",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "remove rollback target volume web_cache")

	require.Positive(t, targetRemoveAttempts.Load())
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok, "project-state journal should clear after database rollback succeeds")
	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok, "target cleanup should keep retry state when removal fails")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	allowTargetRemove.Store(true)
	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetExists.Load())
}

func TestProjectService_RecoverProjectRenameJournals_KeepsRollbackCleanupWhenDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	oldPath := filepath.Join(projectsDir, oldDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &Project{
		ID:      "proj-rename-rollback-cleanup-docker-unavailable",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	cleanup := projectRenameRollbackCleanupInternal{
		ProjectID: project.ID,
		OldName:   "nginx",
		OldPath:   oldPath,
		NewName:   "web",
		NewPath:   filepath.Join(projectsDir, "web"),
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(cleanup)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameRollbackCleanupKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "docker service unavailable")

	_, ok, err := kvService.Get(ctx, projectRenameRollbackCleanupKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournalWhenSourceAndTargetMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-missing-both-volumes",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseProjectStateCommittedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournalAndCleansRemainingSourcesWhenSomeVolumesExternallyRemoved(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var cacheSourceRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			cacheSourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-mixed-missing-volumes",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseProjectStateCommittedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
			{
				Key:     "cache",
				OldName: "nginx_cache",
				NewName: "web_cache",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, cacheSourceRemoved.Load(), "source volume should still be cleaned up when the target exists")
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_MarksSourceCleanupPendingWhenCommittedCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var sourceRemoveAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			sourceRemoveAttempts.Add(1)
			http.Error(w, "volume busy", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-source-cleanup-fail",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseProjectStateCommittedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	var cleanupErr *volumes.SourceCleanupError
	require.ErrorAs(t, err, &cleanupErr)
	require.Equal(t, "nginx_data", cleanupErr.SourceVolume)
	require.Positive(t, sourceRemoveAttempts.Load())

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsSourceCleanupPendingJournalAfterCleanup(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var sourceRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			sourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-source-cleanup-pending-clear",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseSourceCleanupPendingInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, sourceRemoved.Load())
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackSourceCleanupPendingWhenTargetMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var dataSourceRemoved atomic.Bool
	var dataTargetRemoved atomic.Bool
	var cacheTargetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			dataSourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			dataTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			cacheTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-source-cleanup-target-missing",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseSourceCleanupPendingInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
			{
				Key:     "cache",
				OldName: "nginx_cache",
				NewName: "web_cache",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, dataSourceRemoved.Load(), "source volume is the remaining data copy and must not be removed")
	require.False(t, dataTargetRemoved.Load(), "missing target should not be removed")
	require.True(t, cacheTargetRemoved.Load(), "safe target volume should still be cleaned during rollback")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_KeepsSourceCleanupPendingJournalWhenCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var sourceRemoveAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			sourceRemoveAttempts.Add(1)
			http.Error(w, "volume busy", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &Project{
		ID:      "proj-rename-source-cleanup-pending-fail",
		Name:    "web",
		DirName: &newDir,
		Path:    newPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseSourceCleanupPendingInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	var cleanupErr *volumes.SourceCleanupError
	require.ErrorAs(t, err, &cleanupErr)
	require.Equal(t, "nginx_data", cleanupErr.SourceVolume)
	require.Positive(t, sourceRemoveAttempts.Load())

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoriesAreMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &Project{
		ID:      "proj-rename-missing-directories",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseStartedInternal,
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsMissingPathJournalWhenTargetPreserved(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &Project{
		ID:      "proj-rename-missing-path-preserved-target",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenRollbackSourceInspectFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.Error(w, "temporary docker error", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &Project{
		ID:      "proj-rename-source-inspect-preserve",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok, "inspect uncertainty should not permanently block future renames")
	require.False(t, targetRemoved.Load(), "target volume must not be deleted when source inspection is uncertain")
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenRollbackTargetInspectFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.Error(w, "temporary docker error", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &Project{
		ID:      "proj-rename-target-inspect-preserve",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok, "inspect uncertainty should not permanently block future renames")
	require.False(t, targetRemoved.Load(), "target volume must not be deleted when target inspection is uncertain")
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenTargetPreservedAndDirectoryRolledBack(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &Project{
		ID:      "proj-rename-preserved-target-retry",
		Name:    "nginx",
		DirName: &oldDir,
		Path:    oldPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := kv.NewKVService(db)
	dockerService := &docker.DockerClientService{Client: newTestDockerClientInternal(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, nil, config.Load()).WithKVService(kvService)
	journal := projectRenameJournalInternal{
		ProjectID:  project.ID,
		OldName:    "nginx",
		NewName:    "web",
		OldPath:    oldPath,
		NewPath:    newPath,
		OldDirName: &oldDir,
		NewDirName: newDir,
		Phase:      projectRenameJournalPhaseTargetsCopiedInternal,
		Volumes: []volumes.JournalVolume{
			{
				Key:     "data",
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}
	payload, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyPrefixInternal+project.ID, string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyPrefixInternal+project.ID)
	require.NoError(t, err)
	require.False(t, ok, "preserved target data should not leave the project permanently blocked")
	require.False(t, targetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
}

func TestProjectService_MapProjectToDto_SeedsHasBuildDirectiveFromPersistedRefs(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, nil, config.Load())
	metaEnv := &projectMetadataEnvInternal{projectsDirectory: t.TempDir()}

	now := time.Now()
	tests := []struct {
		name               string
		buildImageRefsJSON *string
		want               bool
	}{
		{name: "build refs persisted", buildImageRefsJSON: new(`["demo-worker"]`), want: true},
		{name: "no build services", buildImageRefsJSON: new(`[]`), want: false},
		{name: "refs never resolved", buildImageRefsJSON: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Project{ID: tt.name, Name: tt.name, Path: t.TempDir(), UpdatedAt: &now, BuildImageRefsJSON: tt.buildImageRefsJSON}
			assert.Equal(t, tt.want, svc.mapProjectToDto(ctx, metaEnv.projectsDirectory, p, nil, "", nil, metaEnv).HasBuildDirective)
		})
	}
}
