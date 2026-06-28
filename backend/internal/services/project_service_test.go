package services

import (
	"bytes"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/volume"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	buildtypes "go.getarcane.app/builds/types"
	libupdater "go.getarcane.app/updater/pkg/labels"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
)

func TestWriteProjectProgressInternal_SuppressedContextSkipsProgress(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	ctx = context.WithValue(ctx, projects.ProgressWriterKey{}, &buf)
	ctx = withProjectProgressSuppressedInternal(ctx)

	writeProjectProgressInternal(ctx, "Project stopped", 100, "complete")

	require.Empty(t, buf.String())
}

type testBuildBuilder struct {
	err error
}

func (b testBuildBuilder) BuildImage(_ context.Context, _ buildtypes.BuildRequest, _ io.Writer, _ string) (*buildtypes.BuildResult, error) {
	if b.err != nil {
		return nil, b.err
	}
	return &buildtypes.BuildResult{Provider: "local"}, nil
}

var _ buildtypes.Builder = testBuildBuilder{}

func setupProjectTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Project{}, &models.SettingVariable{}, &models.ImageUpdateRecord{}, &models.Event{}))
	return &database.DB{DB: db}
}

func setupProjectDestroyTestServiceInternal(t *testing.T) (*ProjectService, *database.DB, string) {
	t.Helper()

	ctx := context.Background()
	db := setupProjectTestDB(t)
	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsDir := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, config.Load(), nil)
	return NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load()), db, projectsDir
}

func newProjectImagePullServer(t *testing.T, inspectByRef map[string]dockertypesimage.InspectResponse) *httptest.Server {
	return newProjectImagePullServerWithObserverInternal(t, inspectByRef, nil)
}

func TestProjectService_DestroyProject_RemovesFilesWhenRequested(t *testing.T) {
	ctx := context.Background()
	svc, db, projectsDir := setupProjectDestroyTestServiceInternal(t)

	projectPath := filepath.Join(projectsDir, "demo-remove")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project-data.txt"), []byte("keep until destroy\n"), 0o644))

	dirName := "demo-remove"
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "project-destroy-remove-files"},
		Name:      "demo-remove",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, svc.DestroyProject(ctx, project.ID, true, false, models.User{}))

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
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "project-destroy-preserve-files"},
		Name:      "demo-preserve",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, svc.DestroyProject(ctx, project.ID, false, false, models.User{}))

	assert.DirExists(t, projectPath)
	assert.FileExists(t, projectDataPath)
}

func newProjectImagePullServerWithObserverInternal(t *testing.T, inspectByRef map[string]dockertypesimage.InspectResponse, onPull func(fullRef string, authHeader string)) *httptest.Server {
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
			_, _ = io.WriteString(w, fmt.Sprintf(`{"status":"Pulled","id":%q}`+"\n", fullRef))
			return
		case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			path := r.URL.Path
			imagePathIndex := strings.Index(path, "/images/")
			require.NotEqual(t, -1, imagePathIndex)
			encodedRef := strings.TrimSuffix(path[imagePathIndex+len("/images/"):], "/json")
			imageRef, err := url.PathUnescape(encodedRef)
			require.NoError(t, err)

			inspect, ok := inspectByRef[imageRef]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(inspect))
			return
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(server.Close)

	return server
}

func TestProjectService_GetProjectFromDatabaseByID(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	// Setup dependencies
	settingsService, _ := NewSettingsService(ctx, db)
	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	// Create test project
	proj := &models.Project{
		BaseModel: models.BaseModel{
			ID: "p1",
		},
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
	svc := &ProjectService{}

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
			total, running := svc.getServiceCounts(tt.services)
			assert.Equal(t, tt.wantTotal, total)
			assert.Equal(t, tt.wantRunning, running)
		})
	}
}

func TestProjectService_CalculateProjectStatus(t *testing.T) {
	svc := &ProjectService{}

	tests := []struct {
		name     string
		services []ProjectServiceInfo
		want     models.ProjectStatus
	}{
		{
			name:     "empty",
			services: []ProjectServiceInfo{},
			want:     models.ProjectStatusUnknown,
		},
		{
			name: "all running",
			services: []ProjectServiceInfo{
				{Status: "running"},
				{Status: "up"},
			},
			want: models.ProjectStatusRunning,
		},
		{
			name: "all stopped",
			services: []ProjectServiceInfo{
				{Status: "exited"},
				{Status: "stopped"},
			},
			want: models.ProjectStatusStopped,
		},
		{
			name: "partial",
			services: []ProjectServiceInfo{
				{Status: "running"},
				{Status: "exited"},
			},
			want: models.ProjectStatusPartiallyRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.calculateProjectStatus(tt.services)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProjectService_UpdateProjectStatusInternal(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

	proj := &models.Project{
		BaseModel: models.BaseModel{
			ID: "p1",
		},
		Status: models.ProjectStatusUnknown,
	}
	require.NoError(t, db.Create(proj).Error)

	err := svc.updateProjectStatusInternal(ctx, "p1", models.ProjectStatusRunning)
	require.NoError(t, err)

	var updated models.Project
	require.NoError(t, db.First(&updated, "id = ?", "p1").Error)
	assert.Equal(t, models.ProjectStatusRunning, updated.Status)
	if updated.UpdatedAt != nil {
		assert.WithinDuration(t, time.Now(), *updated.UpdatedAt, time.Second)
	} else {
		t.Error("UpdatedAt should not be nil")
	}
}

func TestProjectService_IncrementStatusCounts(t *testing.T) {
	svc := &ProjectService{}
	running := 0
	stopped := 0

	svc.incrementStatusCounts(models.ProjectStatusRunning, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 0, stopped)

	svc.incrementStatusCounts(models.ProjectStatusStopped, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 1, stopped)

	svc.incrementStatusCounts(models.ProjectStatusUnknown, &running, &stopped)
	assert.Equal(t, 1, running)
	assert.Equal(t, 1, stopped)
}

func TestProjectService_FormatDockerPorts(t *testing.T) {
	tests := []struct {
		name     string
		input    []container.PortSummary
		expected []string
	}{
		{
			name: "public port",
			input: []container.PortSummary{
				{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
			},
			expected: []string{"8080:80/tcp"},
		},
		{
			name: "private only",
			input: []container.PortSummary{
				{PrivatePort: 80, Type: "tcp"},
			},
			expected: []string{"80/tcp"},
		},
		{
			name:     "empty",
			input:    []container.PortSummary{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDockerPorts(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestProjectService_NormalizeComposeProjectName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "with special chars",
			input:    "My Project!",
			expected: "myproject",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeComposeProjectName(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestProjectService_GetProjectByComposeName(t *testing.T) {
	ctx := context.Background()

	t.Run("exact match", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

		proj := &models.Project{
			BaseModel: models.BaseModel{ID: "p1"},
			Name:      "myproject",
			Path:      "/tmp/myproject",
		}
		require.NoError(t, db.Create(proj).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, proj.ID, found.ID)
	})

	t.Run("normalized fallback", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

		proj := &models.Project{
			BaseModel: models.BaseModel{ID: "p1"},
			Name:      "myproject",
			Path:      "/tmp/myproject",
		}
		require.NoError(t, db.Create(proj).Error)

		found, err := svc.GetProjectByComposeName(ctx, "My Project!")
		require.NoError(t, err)
		assert.Equal(t, proj.ID, found.ID)
	})

	t.Run("display name in db, normalized compose label input", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

		display := &models.Project{
			BaseModel: models.BaseModel{ID: "p2"},
			Name:      "My Project!",
			Path:      "/tmp/my-project",
		}
		require.NoError(t, db.Create(display).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, display.ID, found.ID)
	})

	t.Run("invalidates stale normalized cache entries after deletion", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

		original := &models.Project{
			BaseModel: models.BaseModel{ID: "p3"},
			Name:      "My Project!",
			Path:      "/tmp/my-project",
		}
		require.NoError(t, db.Create(original).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, original.ID, found.ID)

		cachedProjectID, cached := svc.getCachedComposeProjectIDInternal("myproject")
		require.True(t, cached)
		assert.Equal(t, original.ID, cachedProjectID)

		require.NoError(t, db.Delete(&models.Project{}, "id = ?", original.ID).Error)

		replacement := &models.Project{
			BaseModel: models.BaseModel{ID: "p4"},
			Name:      "My Project!",
			Path:      "/tmp/my-project-recreated",
		}
		require.NoError(t, db.Create(replacement).Error)

		found, err = svc.GetProjectByComposeName(ctx, "myproject")
		require.NoError(t, err)
		assert.Equal(t, replacement.ID, found.ID)

		cachedProjectID, cached = svc.getCachedComposeProjectIDInternal("myproject")
		require.True(t, cached)
		assert.Equal(t, replacement.ID, cachedProjectID)
	})

	t.Run("invalidates stale normalized cache entries after rename", func(t *testing.T) {
		db := setupProjectTestDB(t)
		svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load())

		original := &models.Project{
			BaseModel: models.BaseModel{ID: "p5"},
			Name:      "My App!",
			Path:      "/tmp/my-app",
		}
		require.NoError(t, db.Create(original).Error)

		found, err := svc.GetProjectByComposeName(ctx, "myapp")
		require.NoError(t, err)
		assert.Equal(t, original.ID, found.ID)

		cachedProjectID, cached := svc.getCachedComposeProjectIDInternal("myapp")
		require.True(t, cached)
		assert.Equal(t, original.ID, cachedProjectID)

		require.NoError(t, db.Model(&models.Project{}).Where("id = ?", original.ID).Updates(map[string]any{
			"name": "New Service",
			"path": "/tmp/new-service",
		}).Error)

		_, err = svc.GetProjectByComposeName(ctx, "myapp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "project not found")

		_, cached = svc.getCachedComposeProjectIDInternal("myapp")
		assert.False(t, cached)
	})
}

func TestResolveServiceImagePullMode(t *testing.T) {
	tests := []struct {
		name     string
		service  composetypes.ServiceConfig
		expected imagePullMode
	}{
		{
			name:     "default policy is missing",
			service:  composetypes.ServiceConfig{},
			expected: imagePullModeIfMissing,
		},
		{
			name:     "always policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyAlways},
			expected: imagePullModeAlways,
		},
		{
			name:     "refresh policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyRefresh},
			expected: imagePullModeAlways,
		},
		{
			name:     "missing policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyMissing},
			expected: imagePullModeIfMissing,
		},
		{
			name:     "if not present policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyIfNotPresent},
			expected: imagePullModeIfMissing,
		},
		{
			name:     "never policy",
			service:  composetypes.ServiceConfig{PullPolicy: composetypes.PullPolicyNever},
			expected: imagePullModeNever,
		},
		{
			name:     "invalid policy defaults to missing behavior",
			service:  composetypes.ServiceConfig{PullPolicy: "definitely_invalid"},
			expected: imagePullModeIfMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, resolveServiceImagePullMode(tt.service))
		})
	}
}

func TestBuildProjectImagePullPlan(t *testing.T) {
	services := composetypes.Services{
		"web": {
			Name:       "web",
			Image:      "redis:latest",
			PullPolicy: composetypes.PullPolicyIfNotPresent,
		},
		"worker": {
			Name:       "worker",
			Image:      "redis:latest",
			PullPolicy: composetypes.PullPolicyAlways,
		},
		"api": {
			Name:       "api",
			Image:      "nginx:latest",
			PullPolicy: composetypes.PullPolicyNever,
		},
		"empty-image": {
			Name:       "empty-image",
			Image:      "",
			PullPolicy: composetypes.PullPolicyAlways,
		},
	}

	plan := buildProjectImagePullPlan(services)

	assert.Len(t, plan, 2)
	assert.Equal(t, imagePullModeAlways, plan["redis:latest"])
	assert.Equal(t, imagePullModeNever, plan["nginx:latest"])
}

func TestProjectService_PullProjectImages_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageRef := "registry.example.com/team/app:1.2.3"
	repository := "registry.example.com/team/app"
	imageID := "sha256:team-app"
	imageDigest := digest.FromString("team-app-digest").String()
	buildRepository := "arcane.local/demo-builder/service"

	server := newProjectImagePullServer(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	})

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	eventService := NewEventService(db, nil, nil)
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, "compose-pull")
	composeContent := fmt.Sprintf("services:\n  app:\n    image: %s\n  builder:\n    build: .\n", imageRef)
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(composeContent), 0o644))

	dirName := "compose-pull"
	projectRecord := &models.Project{
		BaseModel: models.BaseModel{ID: "project-pull"},
		Name:      "compose-pull",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	now := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:old-full",
		Repository:     repository,
		Tag:            "1.2.3",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "1.2.3",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:old-short",
		Repository:     "team/app",
		Tag:            "1.2.3",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "1.2.3",
		CheckTime:      now.Add(time.Minute),
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:build-only",
		Repository:     buildRepository,
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      now.Add(2 * time.Minute),
	}).Error)

	require.NoError(t, svc.PullProjectImages(ctx, projectRecord.ID, io.Discard, systemUser, nil))

	// sha256:old-* records represent update records for OTHER containers still running
	// the old image. Pulling the image for one container must not mark them as up-to-date
	// (#2453: updating one container was incorrectly removing others from the update list).
	var fullRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-full").First(&fullRecord).Error)
	assert.True(t, fullRecord.HasUpdate)

	var shortRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-short").First(&shortRecord).Error)
	assert.True(t, shortRecord.HasUpdate)

	var buildRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:build-only").First(&buildRecord).Error)
	assert.True(t, buildRecord.HasUpdate)

	// The newly pulled image itself is correctly marked as up-to-date.
	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, repository, currentRecord.Repository)
	assert.Equal(t, "1.2.3", currentRecord.Tag)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.CurrentDigest))
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_EnsureImagesPresent_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/api:2.0.0"
	repository := "registry.example.com/team/api"
	imageID := "sha256:team-api"
	imageDigest := digest.FromString("team-api-digest").String()

	server := newProjectImagePullServer(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	})

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	eventService := NewEventService(db, nil, nil)
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:old-api",
		Repository:     repository,
		Tag:            "2.0.0",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "2.0.0",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	require.NoError(t, svc.ensureImagesPresent(ctx, map[string]imagePullMode{
		imageRef: imagePullModeAlways,
	}, io.Discard, nil, systemUser))

	// sha256:old-api may still be in use by another container — pulling for one container
	// must not clear it (fixes #2453).
	var oldRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-api").First(&oldRecord).Error)
	assert.True(t, oldRecord.HasUpdate)

	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_PullImageForService_UpdatesCurrentImageRecordAfterPull(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/worker:3.1.4"
	repository := "registry.example.com/team/worker"
	imageID := "sha256:team-worker"
	imageDigest := digest.FromString("team-worker-digest").String()

	server := newProjectImagePullServer(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	})

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	eventService := NewEventService(db, nil, nil)
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:old-worker",
		Repository:     repository,
		Tag:            "3.1.4",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "3.1.4",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	require.NoError(t, svc.pullAndReconcileImageInternal(ctx, imageRef, io.Discard, systemUser, nil))

	// sha256:old-worker may still be in use by another container — must not be cleared (fixes #2453).
	var oldRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-worker").First(&oldRecord).Error)
	assert.True(t, oldRecord.HasUpdate)

	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_ComposePullSelectedServicesInternal_ReconcilesOnlyOnSuccess(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := NewSettingsService(ctx, db)
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
	server := newProjectImagePullServerWithObserverInternal(t, map[string]dockertypesimage.InspectResponse{
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

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	eventService := NewEventService(db, nil, nil)
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())

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
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     privateRepository,
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:sidecar-old",
		Repository:     publicRepository,
		Tag:            "1.27",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "1.27",
		CheckTime:      now,
	}).Error)

	credentials := []containerregistry.Credential{{
		URL:      "https://registry.example.com",
		Username: "arcane-user",
		Token:    "arcane-token",
		Enabled:  true,
	}}

	require.NoError(t, svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app", "app-copy", "sidecar", "builder"}, systemUser, credentials))
	assert.Equal(t, 1, pullsByRef[privateImageRef], "duplicate service refs should only be pulled once")
	assert.Equal(t, 1, pullsByRef[publicImageRef], "selected public image should still be pulled")
	assert.Len(t, pullsByRef, 2, "build-backed services should not trigger image pulls")

	privateAuth := decodeRegistryAuth(t, authHeadersByRef[privateImageRef])
	assert.Equal(t, "arcane-user", privateAuth.Username)
	assert.Equal(t, "arcane-token", privateAuth.Password)
	assert.Equal(t, "registry.example.com", privateAuth.ServerAddress)
	assert.Empty(t, authHeadersByRef[publicImageRef], "public image pull should not receive unrelated registry auth")

	// sha256:selected-old may still be used by another container — must not be cleared (fixes #2453).
	var selectedRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.True(t, selectedRecord.HasUpdate)

	var sidecarRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:sidecar-old").First(&sidecarRecord).Error)
	assert.True(t, sidecarRecord.HasUpdate)

	var privateCurrentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", privateImageID).First(&privateCurrentRecord).Error)
	assert.False(t, privateCurrentRecord.HasUpdate)
	assert.Equal(t, privateImageDigest, stringPtrToString(privateCurrentRecord.LatestDigest))

	var publicCurrentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", publicImageID).First(&publicCurrentRecord).Error)
	assert.False(t, publicCurrentRecord.HasUpdate)
	assert.Equal(t, publicImageDigest, stringPtrToString(publicCurrentRecord.LatestDigest))
}

func TestProjectService_ComposePullSelectedServicesInternal_LeavesRecordsWhenPullFails(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := NewSettingsService(ctx, db)
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

	dockerService := &DockerClientService{client: newTestDockerClient(t, failingServer)}
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, NewEventService(db, nil, nil))
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())

	projectDef := &composetypes.Project{
		Name: "compose-selected",
		Services: composetypes.Services{
			"app": {
				Name:  "app",
				Image: imageRef,
			},
		},
	}

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     repository,
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	err = svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app"}, systemUser, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to pull image")

	var selectedRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.True(t, selectedRecord.HasUpdate)

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.ImageUpdateRecord{}).Where("id = ?", "sha256:team-app-compose").Count(&count).Error)
	assert.Zero(t, count)
}

func TestProjectService_UpdateProjectServicesHardFailsWhenPullFailsInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
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

	dockerService := &DockerClientService{client: newTestDockerClient(t, failingServer)}
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, NewEventService(db, nil, nil))

	projectPath := createComposeProjectDir(t, projectsDir, "compose-update-pull-fail")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: "+imageRef+"\n"), 0o644))

	projectRecord := &models.Project{
		BaseModel: models.BaseModel{ID: "project-update-pull-fail"},
		Name:      "compose-update-pull-fail",
		DirName:   ptr("compose-update-pull-fail"),
		Path:      projectPath,
		Status:    models.ProjectStatusRunning,
	}
	require.NoError(t, db.Create(projectRecord).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:selected-old",
		Repository:     "registry.example.com/team/app",
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	originalComposeUp := composeUpProjectServicesInternal
	t.Cleanup(func() {
		composeUpProjectServicesInternal = originalComposeUp
	})
	upCalled := false
	composeUpProjectServicesInternal = func(context.Context, *composetypes.Project, []string, bool, bool) error {
		upCalled = true
		return errors.New("compose up should not run")
	}

	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())
	err = svc.UpdateProjectServices(ctx, projectRecord.ID, []string{"app"}, systemUser)
	require.Error(t, err)
	assert.ErrorContains(t, err, "pull updated service images")
	assert.False(t, upCalled, "compose up must not run after a pull failure")

	var persistedProject models.Project
	require.NoError(t, db.WithContext(ctx).Where("id = ?", projectRecord.ID).First(&persistedProject).Error)
	assert.Equal(t, models.ProjectStatusRunning, persistedProject.Status)

	var persistedRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&persistedRecord).Error)
	assert.True(t, persistedRecord.HasUpdate)
}

func TestProjectService_UpdateProjectServicesForcesRecreateInternal(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageRef := "registry.example.com/team/app:9.9.9"
	repository := "registry.example.com/team/app"
	imageID := "sha256:project-update-force"
	imageDigest := digest.FromString("project-update-force-digest").String()

	server := newProjectImagePullServer(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	})

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, NewEventService(db, nil, nil))

	projectPath := createComposeProjectDir(t, projectsDir, "compose-update-force")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: "+imageRef+"\n"), 0o644))

	projectRecord := &models.Project{
		BaseModel: models.BaseModel{ID: "project-update-force"},
		Name:      "compose-update-force",
		DirName:   ptr("compose-update-force"),
		Path:      projectPath,
		Status:    models.ProjectStatusRunning,
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
	composeUpProjectServicesInternal = func(_ context.Context, _ *composetypes.Project, services []string, removeOrphans bool, force bool) error {
		upCalled = true
		forceRecreate = force
		assert.Equal(t, []string{"app"}, services)
		assert.False(t, removeOrphans)
		return errors.New("compose up failed after assertion")
	}

	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, nil, config.Load())
	err = svc.UpdateProjectServices(ctx, projectRecord.ID, []string{"app"}, systemUser)
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	dockerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			_, _ = io.WriteString(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/version"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"ApiVersion":    "1.41",
				"MinAPIVersion": "1.24",
				"Version":       "24.0.0",
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/bar_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name": "bar_data",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(dockerServer.Close)
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, dockerServer.URL))

	dockerService := &DockerClientService{client: newTestDockerClient(t, dockerServer)}
	svc := NewProjectService(db, settingsService, eventService, nil, dockerService, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	require.NoError(t, os.WriteFile(filepath.Join(originalPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"), 0o644))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-volume-conflict"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target volume already exists")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
}

func TestProjectService_ApplyProjectUpdateWithRenameJournal_AppliesVolumeMigrationWhenNameChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{}

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-volume-success"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
	require.NoError(t, err)

	assert.True(t, migration.applyCalled)
	assert.True(t, migration.commitCalled)
	assert.False(t, migration.rollbackCalled)
	assert.True(t, projectStateCommitted)
	assert.Equal(t, "bar", projectForUpdate.Name)
	assert.DirExists(t, filepath.Join(projectsDir, "bar"))
	assert.NoDirExists(t, originalPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "bar", fromDB.Name)
	assert.Equal(t, filepath.Join(projectsDir, "bar"), fromDB.Path)
}

func TestProjectService_PrepareProjectRenameVolumeMigrationForUpdate_UsesComposePreview(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	dockerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name":   "nginx_data",
				"Driver": "local",
				"Labels": map[string]string{
					composeapi.ProjectLabel: "nginx",
					composeapi.VolumeLabel:  "data",
				},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(dockerServer.Close)

	dockerService := &DockerClientService{client: newTestDockerClient(t, dockerServer)}
	svc := NewProjectService(db, settingsService, nil, nil, dockerService, nil, nil, config.Load())

	projectPath := filepath.Join(projectsDir, "nginx")
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	oldCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(oldCompose), 0o644))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-preview-volume-rename"},
		Name:      "nginx",
		DirName:   ptr("nginx"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}

	t.Run("skips volume made explicit in pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: fixed-data\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil, nil)

		require.NoError(t, err)
		require.Nil(t, migration)
		bytes, readErr := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
		require.NoError(t, readErr)
		require.Equal(t, oldCompose, string(bytes))
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans unchanged auto-managed volume from pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		volumes := journalSource.JournalVolumes()
		require.Len(t, volumes, 1)
		require.Equal(t, "data", volumes[0].Key)
		require.Equal(t, "nginx_data", volumes[0].OldName)
		require.Equal(t, "web_data", volumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans auto-managed volume when pending compose name renames project", func(t *testing.T) {
		newCompose := "name: web\nservices:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    driver: local\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		volumes := journalSource.JournalVolumes()
		require.Len(t, volumes, 1)
		require.Equal(t, "data", volumes[0].Key)
		require.Equal(t, "nginx_data", volumes[0].OldName)
		require.Equal(t, "web_data", volumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})

	t.Run("plans interpolated explicit name from pending compose", func(t *testing.T) {
		newCompose := "services:\n  app:\n    image: nginx:alpine\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: ${DATA_VOLUME:-nginx_data}\n"

		migration, err := svc.prepareProjectRenameVolumeMigrationForUpdateInternal(ctx, project, new("web"), projectsDir, &newCompose, nil, nil, nil)

		require.NoError(t, err)
		require.NotNil(t, migration)
		journalSource, ok := migration.(volumes.JournalSource)
		require.True(t, ok)
		volumes := journalSource.JournalVolumes()
		require.Len(t, volumes, 1)
		require.Equal(t, "data", volumes[0].Key)
		require.Equal(t, "nginx_data", volumes[0].OldName)
		require.Equal(t, "web_data", volumes[0].NewName)
		require.Empty(t, projectUpdatePreviewDirsInternal(t, projectsDir))
	})
}

func TestProjectService_ApplyProjectUpdateWithRenameJournal_RollsBackVolumeMigrationWhenProjectSaveFails(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{}

	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("arcane_test_project_save_failure", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Project" {
			_ = tx.AddError(errors.New("forced project save failure"))
		}
	}))

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-volume-save-fail"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = svc.withProjectRenameRollback(ctx, &projectForUpdate, &projectStateCommitted, func() error {
		return svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	migration := &fakeProjectVolumeRenameMigrationInternal{
		commitErr: errors.New("source cleanup failed"),
	}

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-volume-cleanup-fail"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	projectForUpdate := *project
	journalActive := false
	projectStateCommitted := false
	err = svc.withProjectRenameRollback(ctx, &projectForUpdate, &projectStateCommitted, func() error {
		return svc.applyProjectUpdateWithRenameJournalInternal(ctx, &projectForUpdate, new("bar"), projectsDir, nil, nil, nil, nil, migration, nil, &journalActive, &projectStateCommitted)
	})
	require.NoError(t, err)
	require.True(t, migration.applyCalled)
	require.True(t, migration.commitCalled)
	require.False(t, migration.rollbackCalled)
	require.True(t, projectStateCommitted)
	require.NoDirExists(t, originalPath)
	require.DirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "bar", fromDB.Name)
	require.Equal(t, filepath.Join(projectsDir, "bar"), fromDB.Path)
}

func TestProjectService_UpdateProject_ClearsJournalForNonRenameWhenRecoveryDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	kvService := NewKVService(db)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load()).WithKVService(kvService)

	oldDir := "nginx"
	projectPath := createComposeProjectDir(t, projectsDir, oldDir)
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-non-rename-recovery-docker-unavailable"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	envContent := "FOO=bar\n"
	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, &envContent, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.Equal(t, "nginx", updated.Name)

	envBytes, err := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, err)
	require.Equal(t, envContent, string(envBytes))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_UpdateProject_AllowsRenameAfterJournalRecoveryDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	kvService := NewKVService(db)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load()).WithKVService(kvService)

	oldDir := "nginx"
	projectPath := createComposeProjectDir(t, projectsDir, oldDir)
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-recovery-docker-unavailable"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	updated, err := svc.UpdateProject(ctx, project.ID, ptr("web"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "web", updated.Name)
	require.NoDirExists(t, projectPath)
	require.DirExists(t, filepath.Join(projectsDir, "web"))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_UpdateProject_RenamesDirectoryWhenNameChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-1"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	expectedPath := filepath.Join(projectsDir, "bar")
	assert.Equal(t, "bar", updated.Name)
	assert.Equal(t, expectedPath, updated.Path)
	require.NotNil(t, updated.DirName)
	assert.Equal(t, "bar", *updated.DirName)
	assert.NoDirExists(t, originalPath)
	assert.DirExists(t, expectedPath)

	var fromDB models.Project
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)

	targetPath := filepath.Join(projectsDir, "bar")
	require.NoError(t, os.MkdirAll(targetPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-2"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project directory already exists")
	assert.DirExists(t, originalPath)
	assert.DirExists(t, targetPath)

	var fromDB models.Project
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := filepath.Join(projectsDir, originalDirName)
	require.NoError(t, os.MkdirAll(originalPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-3"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusRunning,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB models.Project
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-stale-stopped-running-rename"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	assert.Equal(t, models.ProjectStatusStopped, fromDB.Status)
}

func TestProjectService_UpdateProject_RenameResolvesUnknownStoppedStatusBeforeVolumeMigration(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	server := newProjectRuntimeDockerServerInternal(t, nil)
	t.Setenv("DOCKER_HOST", dockerHostFromProjectRuntimeServerURLInternal(t, server.URL))

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	statusReason := "stale runtime status"

	project := &models.Project{
		BaseModel:    models.BaseModel{ID: "proj-unknown-stopped-rename"},
		Name:         "Foo",
		DirName:      &originalDirName,
		Path:         originalPath,
		Status:       models.ProjectStatusUnknown,
		StatusReason: &statusReason,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	expectedPath := filepath.Join(projectsDir, "bar")
	assert.Equal(t, "bar", updated.Name)
	assert.Equal(t, expectedPath, updated.Path)
	assert.Equal(t, models.ProjectStatusStopped, updated.Status)
	assert.Nil(t, updated.StatusReason)
	assert.Equal(t, 1, updated.ServiceCount)
	assert.Equal(t, 0, updated.RunningCount)
	assert.NoDirExists(t, originalPath)
	assert.DirExists(t, expectedPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "bar", fromDB.Name)
	assert.Equal(t, expectedPath, fromDB.Path)
	assert.Equal(t, models.ProjectStatusStopped, fromDB.Status)
	assert.Nil(t, fromDB.StatusReason)
	assert.Equal(t, 1, fromDB.ServiceCount)
	assert.Equal(t, 0, fromDB.RunningCount)
}

func TestProjectService_UpdateProject_RenameRejectsUnknownWhenRuntimeIsRunning(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

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

	project := &models.Project{
		BaseModel:    models.BaseModel{ID: "proj-unknown-running-rename"},
		Name:         "Foo",
		DirName:      &originalDirName,
		Path:         originalPath,
		Status:       models.ProjectStatusUnknown,
		StatusReason: &statusReason,
	}
	require.NoError(t, db.Create(project).Error)

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project must be stopped before renaming (current status: running)")
	assert.DirExists(t, originalPath)
	assert.NoDirExists(t, filepath.Join(projectsDir, "bar"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	assert.Equal(t, "Foo", fromDB.Name)
	assert.Equal(t, originalPath, fromDB.Path)
	assert.Equal(t, models.ProjectStatusUnknown, fromDB.Status)
	require.NotNil(t, fromDB.StatusReason)
	assert.Equal(t, statusReason, *fromDB.StatusReason)
}

func TestProjectService_UpdateProject_ValidatesComposeUsingExistingProjectName(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "demo"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-compose-name"},
		Name:      "demo",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `name: ${COMPOSE_PROJECT_NAME}
services:
  app:
    image: nginx:alpine
`
	env := "COMPOSE_PROJECT_NAME=\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "env-required"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-env-file"},
		Name:      "env-required",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	_, statErr := os.Stat(filepath.Join(projectPath, ".env"))
	require.NoError(t, statErr)
}

func TestProjectService_UpdateProject_AllowsMissingLocalIncludeDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "include-new"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-missing-include"},
		Name:      "include-new",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - metadata.yaml
services:
  app:
    image: nginx:alpine
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, ptr(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	includePath := filepath.Join(projectPath, "metadata.yaml")
	assert.NoFileExists(t, includePath)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
	require.NoError(t, err)
	require.Len(t, details.IncludeFiles, 1)
	assert.Equal(t, "metadata.yaml", details.IncludeFiles[0].RelativePath)

	includeFile, err := svc.GetProjectFileContent(ctx, project.ID, "metadata.yaml")
	require.NoError(t, err)
	assert.Equal(t, "metadata.yaml", includeFile.RelativePath)
	assert.Contains(t, includeFile.Content, "This file will be created when you save changes")

	includeContent := "services: {}\n"
	require.NoError(t, svc.UpdateProjectIncludeFile(ctx, project.ID, "metadata.yaml", includeContent, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	}))

	writtenContent, err := os.ReadFile(includePath)
	require.NoError(t, err)
	assert.Equal(t, includeContent, string(writtenContent))
}

func TestProjectService_UpdateProject_RejectsMissingExternalIncludeDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "include-external"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-external-include"},
		Name:      "include-external",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - ../metadata.yaml
services:
  app:
    image: nginx:alpine
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, ptr(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "invalid compose file")
	assert.NoFileExists(t, filepath.Join(projectsDir, "metadata.yaml"))
	assert.NoFileExists(t, filepath.Join(projectPath, "compose.yaml"))
}

func TestProjectService_CreateProject_RejectsExternalInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "metadata.yaml"), []byte("services: {}\n"), 0o644))

	compose := `include:
  - ../metadata.yaml
services:
  app:
    image: nginx:alpine
`

	project, err := svc.CreateProject(ctx, "evil", compose, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Nil(t, project)
	assert.Contains(t, err.Error(), "invalid compose file")
	assert.NoDirExists(t, filepath.Join(projectsDir, "evil"))
	assert.FileExists(t, filepath.Join(projectsDir, "metadata.yaml"))

	var count int64
	require.NoError(t, db.Model(&models.Project{}).Where("name = ?", "evil").Count(&count).Error)
	assert.Zero(t, count)
}

func TestProjectService_CreateProject_RejectsArrayPathInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "metadata.yaml"), []byte("services: {}\n"), 0o644))

	compose := `include:
  - path:
      - ./local.yaml
      - ../metadata.yaml
services:
  app:
    image: nginx:alpine
`

	project, err := svc.CreateProject(ctx, "evil-array", compose, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Nil(t, project)
	assert.Contains(t, err.Error(), "invalid compose file")
	assert.NoDirExists(t, filepath.Join(projectsDir, "evil-array"))
	assert.FileExists(t, filepath.Join(projectsDir, "metadata.yaml"))

	var count int64
	require.NoError(t, db.Model(&models.Project{}).Where("name = ?", "evil-array").Count(&count).Error)
	assert.Zero(t, count)
}

func TestProjectService_CreateProject_WritesStagedProjectFiles(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	includeContent := "services: {}\n"
	compose := `include:
  - config/app.yaml
services:
  app:
    image: nginx:alpine
`

	project, err := svc.CreateProject(ctx, "with-files", compose, nil, []projecttypes.ProjectFileDraft{
		{RelativePath: "config", IsDirectory: true},
		{RelativePath: "config/app.yaml", Content: includeContent},
		{RelativePath: "README.md", Content: "hello\n"},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, project)

	assert.FileExists(t, filepath.Join(project.Path, "compose.yaml"))
	assert.FileExists(t, filepath.Join(project.Path, ".env"))
	assert.FileExists(t, filepath.Join(project.Path, "config", "app.yaml"))
	assert.FileExists(t, filepath.Join(project.Path, "README.md"))

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{IncludeProjectFiles: true})
	require.NoError(t, err)
	assert.NotEmpty(t, details.FileTreeRevision)

	relativePaths := make([]string, 0, len(details.ProjectFiles))
	for _, file := range details.ProjectFiles {
		relativePaths = append(relativePaths, file.RelativePath)
	}
	assert.Contains(t, relativePaths, "config")
	assert.Contains(t, relativePaths, filepath.ToSlash(filepath.Join("config", "app.yaml")))
	assert.Contains(t, relativePaths, "README.md")
	assert.NotContains(t, relativePaths, "compose.yaml")
	assert.NotContains(t, relativePaths, ".env")
}

func TestProjectService_GetProjectDetails_UsesFileTreeMaxDepthForProjectFiles(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "3")
	t.Setenv("PROJECT_FILE_TREE_MAX_DEPTH", "8")

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	cfg := config.Load()
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, cfg)

	deepFolder := filepath.ToSlash(filepath.Join("level1", "level2", "level3", "level4", "level5"))
	project, err := svc.CreateProject(ctx, "deep-files", "services:\n  app:\n    image: nginx:alpine\n", nil, []projecttypes.ProjectFileDraft{
		{RelativePath: deepFolder, IsDirectory: true},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	assert.DirExists(t, filepath.Join(project.Path, filepath.FromSlash(deepFolder)))

	filesAtScanDepth, _, err := projects.ReadProjectFileTree(project.Path, cfg.ProjectScanMaxDepth, cfg.ProjectScanSkipDirs, "compose.yaml")
	require.NoError(t, err)
	scanDepthRelativePaths := make([]string, 0, len(filesAtScanDepth))
	for _, file := range filesAtScanDepth {
		scanDepthRelativePaths = append(scanDepthRelativePaths, file.RelativePath)
	}
	assert.NotContains(t, scanDepthRelativePaths, deepFolder)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{IncludeProjectFiles: true})
	require.NoError(t, err)

	fileTreeRelativePaths := make([]string, 0, len(details.ProjectFiles))
	for _, file := range details.ProjectFiles {
		fileTreeRelativePaths = append(fileTreeRelativePaths, file.RelativePath)
	}
	assert.Contains(t, fileTreeRelativePaths, deepFolder)
}

func TestProjectService_UpdateProject_AppliesStagedProjectFileChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	project, err := svc.CreateProject(ctx, "editable-files", "services:\n  app:\n    image: nginx:alpine\n", nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	_, revision, err := projects.ReadProjectFileTree(project.Path, config.Load().ProjectScanMaxDepth, config.Load().ProjectScanSkipDirs, "compose.yaml")
	require.NoError(t, err)

	updated := "updated\n"
	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, nil, &revision, []projecttypes.ProjectFileChange{
		{Operation: "create_folder", RelativePath: "config"},
		{Operation: "create_folder", RelativePath: "archive"},
		{Operation: "create_file", RelativePath: "config/app.yaml", Content: new("hello\n")},
		{Operation: "update_file", RelativePath: "config/app.yaml", Content: &updated},
		{Operation: "rename", RelativePath: "config/app.yaml", NewName: "renamed.yaml"},
		{Operation: "move", RelativePath: "config/renamed.yaml", NewParentPath: "archive"},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	bytes, err := os.ReadFile(filepath.Join(project.Path, "archive", "renamed.yaml"))
	require.NoError(t, err)
	assert.Equal(t, updated, string(bytes))
	assert.NoFileExists(t, filepath.Join(project.Path, "config", "renamed.yaml"))
}

func TestProjectService_UpdateProject_RejectsStaleProjectFileRevision(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	project, err := svc.CreateProject(ctx, "stale-files", "services:\n  app:\n    image: nginx:alpine\n", nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	_, revision, err := projects.ReadProjectFileTree(project.Path, config.Load().ProjectScanMaxDepth, config.Load().ProjectScanSkipDirs, "compose.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(project.Path, "external.txt"), []byte("external\n"), 0o644))

	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, nil, &revision, []projecttypes.ProjectFileChange{
		{Operation: "create_file", RelativePath: "notes.txt", Content: new("new\n")},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)

	var conflictErr *common.ProjectFileConflictError
	assert.ErrorAs(t, err, &conflictErr)
	assert.NoFileExists(t, filepath.Join(project.Path, "notes.txt"))
}

func TestProjectService_UpdateProject_RejectsStaleDeepProjectFileRevision(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "3")
	t.Setenv("PROJECT_FILE_TREE_MAX_DEPTH", "8")

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	project, err := svc.CreateProject(ctx, "stale-deep-files", "services:\n  app:\n    image: nginx:alpine\n", nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{IncludeProjectFiles: true})
	require.NoError(t, err)
	require.NotEmpty(t, details.FileTreeRevision)

	deepParent := filepath.Join(project.Path, "level1", "level2", "level3", "level4")
	require.NoError(t, os.MkdirAll(deepParent, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deepParent, "external.txt"), []byte("external\n"), 0o644))

	_, err = svc.UpdateProject(ctx, project.ID, nil, nil, nil, &details.FileTreeRevision, []projecttypes.ProjectFileChange{
		{Operation: "create_file", RelativePath: "notes.txt", Content: new("new\n")},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)

	var conflictErr *common.ProjectFileConflictError
	assert.ErrorAs(t, err, &conflictErr)
	assert.NoFileExists(t, filepath.Join(project.Path, "notes.txt"))
}

func TestProjectService_GetProjectFileContent_RejectsExternalInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "include-read"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, "metadata.yaml"), []byte("services: {}\n"), 0o644))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-external-include-read"},
		Name:      "include-read",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - ../metadata.yaml
services:
  app:
    image: nginx:alpine
`
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(compose), 0o644))

	includeFile, err := svc.GetProjectFileContent(ctx, project.ID, "../metadata.yaml")
	require.Error(t, err)
	assert.Empty(t, includeFile)

	var forbiddenErr *common.ProjectFileForbiddenError
	assert.ErrorAs(t, err, &forbiddenErr)
}

func TestProjectService_GetProjectFileContent_RejectsSymlinkInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "include-symlink"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	outsidePath := filepath.Join(t.TempDir(), "outside.yaml")
	require.NoError(t, os.WriteFile(outsidePath, []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Symlink(outsidePath, filepath.Join(projectPath, "evil-link")))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-symlink-include-read"},
		Name:      "include-symlink",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - ./evil-link
services:
  app:
    image: nginx:alpine
`
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(compose), 0o644))

	includeFile, err := svc.GetProjectFileContent(ctx, project.ID, "evil-link")
	require.Error(t, err)
	assert.Empty(t, includeFile)

	var forbiddenErr *common.ProjectFileForbiddenError
	assert.ErrorAs(t, err, &forbiddenErr)
}

func TestProjectService_GetProjectFileContent_RejectsIntermediateSymlinkInclude(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "include-intermediate-symlink"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "secret.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(projectPath, "subdir")))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-intermediate-symlink-include-read"},
		Name:      "include-intermediate-symlink",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `include:
  - ./subdir/secret.yaml
services:
  app:
    image: nginx:alpine
`
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(compose), 0o644))

	includeFile, err := svc.GetProjectFileContent(ctx, project.ID, "subdir/secret.yaml")
	require.Error(t, err)
	assert.Empty(t, includeFile)

	var forbiddenErr *common.ProjectFileForbiddenError
	assert.ErrorAs(t, err, &forbiddenErr)
}

func TestProjectService_GetProjectFileContent_RejectsIntermediateSymlinkProjectFile(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "project-file-intermediate-symlink"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services: {}\n"), 0o644))

	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "secret.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(projectPath, "subdir")))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-intermediate-symlink-project-file-read"},
		Name:      "project-file-intermediate-symlink",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	includeFile, err := svc.GetProjectFileContent(ctx, project.ID, "subdir/secret.yaml")
	require.Error(t, err)
	assert.Empty(t, includeFile)

	var forbiddenErr *common.ProjectFileForbiddenError
	assert.ErrorAs(t, err, &forbiddenErr)
}

func TestProjectService_UpdateProject_UsesExistingEnvFileDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "env-existing"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("FOO=bar\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-existing-env-file"},
		Name:      "env-existing",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	envBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "FOO=bar\n", string(envBytes))
}

func TestProjectService_UpdateProject_UsesProvidedEnvContentDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "env-updated"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-new-env-file"},
		Name:      "env-updated",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    env_file:
      - .env
`
	env := "FOO=updated\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "env-invalid"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-invalid-env-file"},
		Name:      "env-invalid",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    environment:
      - REQUIRED=${REQUIRED}
`
	env := "BROKEN=${UNTERMINATED\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "global-env-update"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=/srv/media\nMYPATH=/containers/\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-global-env-update"},
		Name:      dirName,
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  cats:
    image: mikesir87/cats:1.0
    volumes:
      - ${DATA_NAS_FOLDER}:/data
      - ${MYPATH}cats/templates:/app/templates
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "host-env-guard"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=${HOST_ONLY_PATH}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-host-env-guard"},
		Name:      dirName,
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  app:
    image: nginx:alpine
    volumes:
      - ${DATA_NAS_FOLDER}:/data
`

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "override-edit"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-override-edit"},
		Name:      "override-edit",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nLOCAL_ONLY=example\n"), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

func TestProjectService_UpdateProject_DeletingGitBackedKeyFallsBackToGit(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "override-delete"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=local\nLOCAL_ONLY=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nTOKEN=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=local\nLOCAL_ONLY=1\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-override-delete"},
		Name:      "override-delete",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nLOCAL_ONLY=1\n"), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-migrate"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("TOKEN=stale-local\nLOCAL_ONLY=1\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-git-sync-migrate"},
		Name:      "git-sync-migrate",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	gitEnv := "TOKEN=git\nREMOTE_ONLY=1\n"
	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", &gitEnv, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

func TestProjectService_ApplyGitSyncProjectFiles_NormalizesStaleCopiedGitOverrides(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-normalize"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\nSHARED=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("BASE=git\nSHARED=1\nTOKEN=local\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nSHARED=1\nTOKEN=local\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-git-sync-normalize"},
		Name:      "git-sync-normalize",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", new("BASE=git-updated\nSHARED=1\nREMOTE_ONLY=1\n"), models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-delete-mask"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("TOKEN=git\nSHARED=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=\nLOCAL_ONLY=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("LOCAL_ONLY=1\nSHARED=1\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-git-sync-delete-mask"},
		Name:      "git-sync-delete-mask",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", new("TOKEN=git-updated\nSHARED=1\nREMOTE_ONLY=1\n"), models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-remove"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-git-sync-remove"},
		Name:      "git-sync-remove",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:alpine\n", nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	_, statErr := os.Stat(filepath.Join(projectPath, ".env.git"))
	assert.True(t, os.IsNotExist(statErr))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "BASE=git\n", string(effectiveBytes))
}

func TestProjectService_ApplyGitSyncProjectFiles_UsesGlobalEnvDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-global-env"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectsDir, projects.GlobalEnvFileName), []byte("DATA_NAS_FOLDER=/srv/media\nMYPATH=/containers/\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-git-sync-global-env"},
		Name:      dirName,
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	compose := `services:
  cats:
    image: mikesir87/cats:1.0
    volumes:
      - ${DATA_NAS_FOLDER}:/data
      - ${MYPATH}cats/templates:/app/templates
`

	updated, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, compose, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

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

	require.NoError(t, svc.persistGitSyncEnvFilesInternal(projectPath, projectsDir, update))

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "TOKEN=local\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Equal(t, "BASE=git-updated\nREMOTE=1\nTOKEN=local\n", string(effectiveBytes))
}

func TestProjectService_GetProjectDetails_ReturnsEffectiveEnvContent(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	dirName := "details-override"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env"), []byte("BASE=git\nTOKEN=secret\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, ".env.git"), []byte("BASE=git\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "project.env"), []byte("TOKEN=secret\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-details-override"},
		Name:      "details-override",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageService := &ImageService{db: db}
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, "updates-demo")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:latest\n"), 0o644))

	projectRecord := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-update-info"},
		Name:      "updates-demo",
		DirName:   ptr("updates-demo"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
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

	settingsService, err := NewSettingsService(ctx, db)
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

	projectRecord := &models.Project{
		BaseModel:    models.BaseModel{ID: "proj-runtime-refresh"},
		Name:         "projectA",
		DirName:      ptr("projectA"),
		Path:         projectPath,
		Status:       models.ProjectStatusStopped,
		ServiceCount: 2,
		RunningCount: 0,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID, projecttypes.DetailsOptions{})
	require.NoError(t, err)
	assert.Equal(t, string(models.ProjectStatusRunning), details.Status)
	assert.Equal(t, 2, details.ServiceCount)
	assert.Equal(t, 2, details.RunningCount)
	assert.Empty(t, details.RuntimeServices)
}

func TestProjectService_GetProjectDetails_PopulatesRuntimeServicesFromComposePs(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
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

	projectRecord := &models.Project{
		BaseModel:    models.BaseModel{ID: "proj-runtime-services"},
		Name:         "projectA",
		DirName:      ptr("projectA"),
		Path:         projectPath,
		Status:       models.ProjectStatusStopped,
		ServiceCount: 1,
		RunningCount: 0,
	}
	require.NoError(t, db.Create(projectRecord).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID, projecttypes.DetailsOptions{IncludeRuntimeServices: true})
	require.NoError(t, err)
	require.Len(t, details.RuntimeServices, 1)
	assert.Equal(t, string(models.ProjectStatusRunning), details.Status)
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageService := &ImageService{db: db}
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, nil, config.Load())

	updatedPath := createComposeProjectDir(t, projectsDir, "updated-demo")
	require.NoError(t, os.WriteFile(filepath.Join(updatedPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:latest\n"), 0o644))
	upToDatePath := createComposeProjectDir(t, projectsDir, "current-demo")
	require.NoError(t, os.WriteFile(filepath.Join(upToDatePath, "compose.yaml"), []byte("services:\n  app:\n    image: redis:7\n"), 0o644))
	errorPath := createComposeProjectDir(t, projectsDir, "error-demo")
	require.NoError(t, os.WriteFile(filepath.Join(errorPath, "compose.yaml"), []byte("services:\n  app:\n    image: busybox:latest\n"), 0o644))
	unknownPath := createComposeProjectDir(t, projectsDir, "unknown-demo")
	require.NoError(t, os.WriteFile(filepath.Join(unknownPath, "compose.yaml"), []byte("services:\n  app:\n    image: alpine:latest\n"), 0o644))

	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-updated"},
		Name:      "updated-demo",
		DirName:   ptr("updated-demo"),
		Path:      updatedPath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-current"},
		Name:      "current-demo",
		DirName:   ptr("current-demo"),
		Path:      upToDatePath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-error"},
		Name:      "error-demo",
		DirName:   ptr("error-demo"),
		Path:      errorPath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-unknown"},
		Name:      "unknown-demo",
		DirName:   ptr("unknown-demo"),
		Path:      unknownPath,
		Status:    models.ProjectStatusStopped,
	}).Error)

	now := time.Now().UTC()
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:updated-image",
		Repository:     "docker.io/library/nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     "digest",
		CurrentVersion: "latest",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:current-image",
		Repository:     "docker.io/library/redis",
		Tag:            "7",
		HasUpdate:      false,
		UpdateType:     "tag",
		CurrentVersion: "7",
		CheckTime:      now.Add(-time.Minute),
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:error-image",
		Repository:     "docker.io/library/busybox",
		Tag:            "latest",
		HasUpdate:      false,
		UpdateType:     "error",
		CurrentVersion: "latest",
		CheckTime:      now.Add(-2 * time.Minute),
		LastError:      ptr("registry timeout"),
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
				Params:     pagination.Params{Limit: -1},
				SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
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
	imageService := &ImageService{db: db}
	now := time.Now().UTC()

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:media-image",
		Repository:     "docker.io/library/nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:known-image",
		Repository:     "docker.io/library/redis",
		Tag:            "7",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
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
	assert.Equal(t, string(models.ProjectStatusRunning), row.Status)
	require.NotNil(t, row.UpdateInfo)
	assert.True(t, row.UpdateInfo.HasUpdate)
	assert.Equal(t, []string{"nginx:latest"}, row.UpdateInfo.UpdatedImageRefs)
	require.Len(t, row.RuntimeServices, 1)
	assert.Equal(t, "web", row.RuntimeServices[0].Name)
}

func TestBuildDiscoveredComposeProjectUpdateRowsInternal_FallsBackToImageID(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	imageService := &ImageService{db: db}

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:media-image",
		Repository:     "registry.example.test/custom/media",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	activePath := createComposeProjectDir(t, projectsRoot, "active-demo")
	archivedPath := createComposeProjectDir(t, projectsRoot, "archived-demo")
	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-active"},
		Name:      "active-demo",
		DirName:   ptr("active-demo"),
		Path:      activePath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	require.NoError(t, db.Create(&models.Project{
		BaseModel:  models.BaseModel{ID: "project-archived"},
		Name:       "archived-demo",
		DirName:    ptr("archived-demo"),
		Path:       archivedPath,
		Status:     models.ProjectStatusStopped,
		IsArchived: true,
		ArchivedAt: new(time.Now().UTC()),
	}).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Params:     pagination.Params{Limit: -1},
		SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "active-demo", items[0].Name)

	items, page, err = svc.ListProjects(ctx, pagination.QueryParams{
		Filters:    map[string]string{"archived": "true"},
		Params:     pagination.Params{Limit: -1},
		SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "archived-demo", items[0].Name)
	assert.True(t, items[0].IsArchived)

	items, page, err = svc.ListProjects(ctx, pagination.QueryParams{
		Filters:    map[string]string{"archived": "all"},
		Params:     pagination.Params{Limit: -1},
		SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
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
	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "running-demo")
	require.NoError(t, db.Create(&models.Project{
		BaseModel:    models.BaseModel{ID: "project-running"},
		Name:         "running-demo",
		DirName:      ptr("running-demo"),
		Path:         projectPath,
		Status:       models.ProjectStatusRunning,
		RunningCount: 1,
	}).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	err = svc.ArchiveProject(ctx, "project-running", models.User{BaseModel: models.BaseModel{ID: "user-1"}, Username: "tester"})
	require.Error(t, err)
	var stoppedErr *common.ProjectMustBeStoppedError
	assert.ErrorAs(t, err, &stoppedErr)

	var stored models.Project
	require.NoError(t, db.First(&stored, "id = ?", "project-running").Error)
	assert.False(t, stored.IsArchived)
	assert.Nil(t, stored.ArchivedAt)
}

func TestProjectService_ArchiveProject_TogglesArchiveFlag(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsRoot := t.TempDir()
	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	projectPath := createComposeProjectDir(t, projectsRoot, "stopped-demo")
	require.NoError(t, db.Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-stopped"},
		Name:      "stopped-demo",
		DirName:   ptr("stopped-demo"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}).Error)

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	user := models.User{BaseModel: models.BaseModel{ID: "user-1"}, Username: "tester"}

	require.NoError(t, svc.ArchiveProject(ctx, "project-stopped", user))
	var stored models.Project
	require.NoError(t, db.First(&stored, "id = ?", "project-stopped").Error)
	assert.True(t, stored.IsArchived)
	assert.NotNil(t, stored.ArchivedAt)

	require.NoError(t, svc.UnarchiveProject(ctx, "project-stopped", user))
	var unarchived models.Project
	require.NoError(t, db.First(&unarchived, "id = ?", "project-stopped").Error)
	assert.False(t, unarchived.IsArchived)
	assert.Nil(t, unarchived.ArchivedAt)
}

func TestProjectService_MapProjectToDto_SetsRedeployDisabledFromRuntimeServices(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "arcane")
	now := time.Now()
	proj := models.Project{
		Name:         "arcane",
		Path:         projectPath,
		ServiceCount: 1,
		BaseModel: models.BaseModel{
			ID:        "project-arcane",
			CreatedAt: now,
			UpdatedAt: &now,
		},
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
				libupdater.LabelArcane:       "true",
			},
			wantProject: true,
			wantService: true,
		},
		{
			name:               "current legacy Arcane server container disables project redeploy",
			containerID:        "arcane1234567890",
			currentContainerID: "arcane1234567890",
			labels: map[string]string{
				"com.docker.compose.project":       "arcane",
				"com.docker.compose.service":       "server",
				libupdater.LabelArcaneLegacyServer: "true",
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
				libupdater.LabelArcane:       "true",
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
				libupdater.LabelArcane:       "true",
				libupdater.LabelArcaneAgent:  "true",
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
			}, tt.currentContainerID, tt.currentErr)

			require.Equal(t, tt.wantProject, details.RedeployDisabled)
			require.Len(t, details.RuntimeServices, 1)
			require.Equal(t, tt.wantService, details.RuntimeServices[0].RedeployDisabled)
		})
	}
}

func TestProjectService_ListProjects_WithDerivedStatusFilter_AllowsAllPageSizeSentinel(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	for i := range 25 {
		projectPath := createComposeProjectDir(t, projectsRoot, fmt.Sprintf("stopped-%02d", i))
		require.NoError(t, db.Create(&models.Project{
			BaseModel: models.BaseModel{ID: fmt.Sprintf("project-%02d", i)},
			Name:      fmt.Sprintf("stopped-%02d", i),
			DirName:   ptr(fmt.Sprintf("stopped-%02d", i)),
			Path:      projectPath,
			Status:    models.ProjectStatusStopped,
		}).Error)
	}

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Filters: map[string]string{
			"status": string(models.ProjectStatusStopped),
		},
		Params:     pagination.Params{Limit: -1},
		SortParams: pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
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

func TestNormalizePullPolicy(t *testing.T) {
	assert.Equal(t, "missing", normalizePullPolicy("if_not_present"))
	assert.Equal(t, "build", normalizePullPolicy(" BUILD "))
	assert.Equal(t, "", normalizePullPolicy(""))
}

func TestDecideDeployImageAction(t *testing.T) {
	t.Run("build service with explicit build policy", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			PullPolicy: "build",
			Build:      &composetypes.BuildConfig{Context: "."},
		}

		decision := decideDeployImageAction(svc, "")
		assert.True(t, decision.Build)
		assert.False(t, decision.PullAlways)
	})

	t.Run("build service default policy uses pull then fallback build", func(t *testing.T) {
		svc := composetypes.ServiceConfig{Build: &composetypes.BuildConfig{Context: "."}}
		decision := decideDeployImageAction(svc, "")
		assert.True(t, decision.PullIfMissing)
		assert.True(t, decision.FallbackBuildOnPullFail)
		assert.False(t, decision.Build)
	})

	t.Run("non-build service never policy requires local only", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "never"}
		decision := decideDeployImageAction(svc, "")
		assert.True(t, decision.RequireLocalOnly)
		assert.False(t, decision.PullIfMissing)
	})

	t.Run("compose pull policy wins over deploy override", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "never"}
		decision := decideDeployImageAction(svc, "always")
		assert.True(t, decision.RequireLocalOnly)
		assert.False(t, decision.PullAlways)
	})
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
	settingsService, err := NewSettingsService(ctx, db)
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

	proj := &models.Project{
		BaseModel: models.BaseModel{ID: "p1"},
		Name:      "demo",
		Path:      projectDir,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(proj).Error)

	buildSvc := &BuildService{builder: testBuildBuilder{err: errors.New("boom build")}}
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, nil, config.Load())

	err = svc.DeployProject(ctx, "p1", models.User{BaseModel: models.BaseModel{ID: "u1"}, Username: "tester"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare project images for deploy")
	assert.Contains(t, err.Error(), "boom build")

	var updated models.Project
	require.NoError(t, db.First(&updated, "id = ?", "p1").Error)
	assert.Equal(t, models.ProjectStatusStopped, updated.Status)
}

func TestProjectService_DeployProject_BuildsGeneratedImageWithoutPull(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	settingsService, err := NewSettingsService(ctx, db)
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

	proj := &models.Project{
		BaseModel: models.BaseModel{ID: "p-generated"},
		Name:      "build-test",
		Path:      projectDir,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(proj).Error)

	buildSvc := &BuildService{builder: testBuildBuilder{err: errors.New("boom build")}}
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, nil, config.Load())

	err = svc.DeployProject(ctx, proj.ID, models.User{BaseModel: models.BaseModel{ID: "u1"}, Username: "tester"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare project images for deploy")
	assert.Contains(t, err.Error(), "boom build")
	assert.NotContains(t, err.Error(), "failed to pull image arcane.local/")
	assert.NotContains(t, err.Error(), "failed to resolve reference \"arcane.local/")
}

func TestResolveBuildContextInternal_AllowsRemoteGitContext(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://github.com/getarcaneapp/arcane.git#main:docker/app",
		},
	}

	contextDir, err := resolveBuildContextInternal("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/getarcaneapp/arcane.git#main:docker/app", contextDir)
}

func TestResolveBuildContextInternal_AllowsRemoteGitContextWithoutGitSuffix(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Build: &composetypes.BuildConfig{
			Context: "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app",
		},
	}

	contextDir, err := resolveBuildContextInternal("/projects/demo", svc, "web")
	require.NoError(t, err)
	assert.Equal(t, "https://git.sr.ht/~jordanreger/nws-alerts#main:docker/app", contextDir)
}

func TestProjectService_SyncProjectsFromFileSystem_IgnoresSymlinkedProjectDirsWhenDisabled(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "regular")
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	require.NoError(t, os.Symlink(linkTarget, filepath.Join(projectsRoot, "linked")))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "false"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	linkPath := filepath.Join(projectsRoot, "linked")
	require.NoError(t, os.Symlink(linkTarget, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "regular")
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	require.NoError(t, os.Symlink(linkTarget, filepath.Join(projectsRoot, "linked")))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	nestedPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project2")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams: pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		Params:     pagination.Params{Limit: -1},
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project1")
	createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

	settingsService, err := NewSettingsService(ctx, db)
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams: pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		Params:     pagination.Params{Limit: -1},
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))
	createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project2"))
	createComposeProjectDir(t, projectsRoot, "project2")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	count, err := svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestProjectService_CountProjectFolders_RespectsConfiguredScanMaxDepth(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "project1")
	createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	count, err := svc.countProjectFolders(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestProjectService_SyncProjectsFromFileSystem_RemovesDeletedNestedProject(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project", "sub-project1"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

func TestProjectService_SyncProjectsFromFileSystem_PreservesProjectsWhenDirectoryEmptyOrUnmounted(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
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
		require.NoError(t, db.WithContext(ctx).Create(&models.Project{
			Name:   fmt.Sprintf("project-%d", i),
			Path:   filepath.Join(projectsRoot, fmt.Sprintf("project-%d", i)),
			Status: models.ProjectStatusStopped,
		}).Error)
	}

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	assert.Len(t, items, seeded, "an empty/mis-mapped projects directory must not wipe existing project records")
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesProjectWithAmbiguousCustomCompose(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, "ambiguous-project")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Replace the standard compose.yaml with two custom-named compose files. The
	// directory still holds compose content, but DetectComposeFile can't pick one
	// and returns common.AmbiguousComposeFileError. The reconcile must NOT delete the
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	topLevelPath := createComposeProjectDir(t, projectsRoot, "project1")
	nestedPath := createComposeProjectDir(t, projectsRoot, filepath.Join("group", "project2"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	// Initial sync at the default scan depth discovers both the top-level and
	// the nested project, persisting them to the database.
	defaultSvc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, defaultSvc.SyncProjectsFromFileSystem(ctx))

	items, err := defaultSvc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)

	// Lowering the scan depth must prune the nested project from the database on
	// the next sync, even though its compose file still exists on disk.
	t.Setenv("PROJECT_SCAN_MAX_DEPTH", "1")
	depthLimitedSvc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	createComposeProjectDir(t, projectsRoot, "project1")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

func TestProjectService_SyncProjectsFromFileSystem_AllowsDuplicateLeafDirectoriesInDifferentParents(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	firstPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project1", "app"))
	secondPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project2", "app"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var items []models.Project
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := createComposeProjectDir(t, targetRoot, filepath.Join("main-project", "sub-project1"))
	linkPath := filepath.Join(projectsRoot, "linked-root")
	require.NoError(t, os.Symlink(filepath.Join(targetRoot, "main-project"), linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams: pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		Params:     pagination.Params{Limit: -1},
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	linkTarget := createComposeProjectDir(t, targetRoot, "linked-target")
	linkPath := filepath.Join(projectsRoot, "linked")
	require.NoError(t, os.Symlink(linkTarget, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectPath := createComposeProjectDir(t, projectsRoot, "demo")

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	var project models.Project
	require.NoError(t, db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error)
	assert.Equal(t, 1, project.ServiceCount)

	updatedCompose := "services:\n  app:\n    image: nginx:alpine\n  worker:\n    image: busybox:latest\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(updatedCompose), 0o644))

	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))
	require.NoError(t, db.WithContext(ctx).Where("id = ?", project.ID).First(&project).Error)
	assert.Equal(t, 2, project.ServiceCount)
}

func TestProjectService_SyncProjectsFromFileSystem_PreservesGitOpsProjectWithCustomComposeFilename(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&models.GitOpsSync{}))

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "radarr.yaml"), []byte("services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"), 0o644))

	syncProjectID := "proj-custom-compose"
	syncID := "sync-custom-compose"
	sync := &models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: syncID},
		Name:          "Radarr Sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &syncProjectID,
		SyncDirectory: true,
	}
	require.NoError(t, db.Create(sync).Error)

	project := &models.Project{
		BaseModel:       models.BaseModel{ID: syncProjectID},
		Name:            "Radarr",
		DirName:         ptr("Radarr-3"),
		Path:            projectDir,
		Status:          models.ProjectStatusStopped,
		GitOpsManagedBy: &syncID,
	}
	require.NoError(t, db.Create(project).Error)

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())
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
	require.NoError(t, db.AutoMigrate(&models.GitOpsSync{}))

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	projectDir := filepath.Join(projectsRoot, "Radarr-3")
	composeContent := "services:\n  app:\n    image: lscr.io/linuxserver/radarr:latest\n"
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "radarr.yaml"), []byte(composeContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte("TZ=UTC\n"), 0o644))

	syncProjectID := "proj-custom-compose-details"
	syncID := "sync-custom-compose-details"
	require.NoError(t, db.Create(&models.GitOpsSync{
		BaseModel:     models.BaseModel{ID: syncID},
		Name:          "Radarr Sync",
		EnvironmentID: "0",
		RepositoryID:  "repo-1",
		ComposePath:   "apps/media/radarr.yaml",
		ProjectName:   "Radarr",
		ProjectID:     &syncProjectID,
		SyncDirectory: true,
	}).Error)

	require.NoError(t, db.Create(&models.Project{
		BaseModel:       models.BaseModel{ID: syncProjectID},
		Name:            "Radarr",
		DirName:         ptr("Radarr-3"),
		Path:            projectDir,
		Status:          models.ProjectStatusStopped,
		GitOpsManagedBy: &syncID,
	}).Error)

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, nil, config.Load())

	composeFromContent, envFromContent, err := svc.GetProjectContent(ctx, syncProjectID)
	require.NoError(t, err)
	assert.Equal(t, composeContent, composeFromContent)
	assert.Equal(t, "TZ=UTC\n", envFromContent)

	details, err := svc.GetProjectDetails(ctx, syncProjectID, projecttypes.AllDetails())
	require.NoError(t, err)
	assert.Equal(t, "radarr.yaml", details.ComposeFileName)
	assert.Equal(t, composeContent, details.ComposeContent)
	assert.Equal(t, "TZ=UTC\n", details.EnvContent)
	assert.Equal(t, 1, len(details.Services))
}

func TestProjectService_UpdateProject_WritesThroughSymlinkedProjectPath(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := createComposeProjectDir(t, targetRoot, "demo-target")
	linkPath := filepath.Join(projectsRoot, "demo")
	require.NoError(t, os.Symlink(targetPath, linkPath))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))
	require.NoError(t, settingsService.SetStringSetting(ctx, "followProjectSymlinks", "true"))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-symlink-update"},
		Name:      "demo",
		DirName:   new("demo"),
		Path:      linkPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updatedCompose := "services:\n  app:\n    image: nginx:1.27-alpine\n"
	updatedEnv := "FOO=updated\n"

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(updatedCompose), new(updatedEnv), nil, nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
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

func ptr(v string) *string {
	return new(v)
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

func TestProjectService_UpdateProject_RenameRollsBackWhenFileRevisionIsStale(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, config.Load())
	configureProjectRuntimeDockerInternal(t, nil)

	originalDirName := "Foo"
	originalPath := createComposeProjectDir(t, projectsDir, originalDirName)
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-stale-file-revision"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	_, revision, err := projects.ReadProjectFileTree(project.Path, config.Load().ProjectFileTreeMaxDepth, config.Load().ProjectScanSkipDirs, "compose.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(project.Path, "external.txt"), []byte("external\n"), 0o644))

	content := "new\n"
	_, err = svc.UpdateProject(ctx, project.ID, ptr("bar"), nil, nil, &revision, []projecttypes.ProjectFileChange{
		{Operation: projecttypes.FileOpCreateFile, RelativePath: "notes.txt", Content: &content},
	}, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})

	require.Error(t, err)
	var conflictErr *common.ProjectFileConflictError
	require.ErrorAs(t, err, &conflictErr)
	require.DirExists(t, originalPath)
	require.NoDirExists(t, filepath.Join(projectsDir, "bar"))
	require.FileExists(t, filepath.Join(originalPath, "external.txt"))
	require.NoFileExists(t, filepath.Join(originalPath, "notes.txt"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "Foo", fromDB.Name)
	require.Equal(t, originalPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackUncommittedDirectoryRename(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-recovery"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_StartedPhaseSkipsVolumeRollback(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "compose.yaml"), []byte("services: {}\n"), 0o600))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-started-volume-recovery"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_RelocatesTargetWhenBothPathsExist(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-both-paths"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	conflictPaths, err := filepath.Glob(filepath.Join(projectsDir, ".web.rename-conflict-*"))
	require.NoError(t, err)
	require.Len(t, conflictPaths, 1)
	require.FileExists(t, filepath.Join(conflictPaths[0], "compose.yaml"))

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	newName := "web"
	require.NoError(t, svc.applyProjectRenameIfNeeded(&fromDB, &newName, projectsDir))
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
	require.DirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoryPathsMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-paths"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsPreservedTargetJournalWhenPathExists(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-preserved-target"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	var targetRemoved bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
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
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)
	require.NoError(t, os.MkdirAll(newPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-committed"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, newPath)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_ClearsJournalAfterSourceCleanup(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-old-volumes-removed"},
		Name:      "web",
		DirName:   &newDir,
		Path:      filepath.Join(t.TempDir(), newDir),
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_FinalizeProjectRenameAfterCommit_KeepsJournalWhenSourceCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	oldDir := "nginx"
	newDir := "web"
	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-cleanup-failure"},
		Name:      "web",
		DirName:   &newDir,
		Path:      filepath.Join(t.TempDir(), newDir),
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
}

func TestProjectService_RecoverProjectRenameJournals_KeepsJournalWhenDirectoryRollbackFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			if targetRemoved.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-directory-rollback-fails"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rollback project directory rename")

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, targetRemoved.Load(), "target volume rollback should still run after directory rollback fails")
	require.FileExists(t, filepath.Join(newPath, "compose.yaml"), "failed directory rollback leaves the target path for retry")
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	require.NoError(t, os.MkdirAll(filepath.Dir(oldPath), 0o755))
	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err = kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
}

func TestProjectService_RecoverProjectRenameJournals_CompletesCommittedVolumeJournalWithoutHelperImage(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var imageInspectCalled atomic.Bool
	var oldVolumeRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-committed-with-volumes"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, oldVolumeRemoved.Load(), "expected committed recovery to remove source volume")
	require.False(t, imageInspectCalled.Load(), "completion recovery should not inspect or pull the copy helper image")
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackCommittedJournalWhenTargetMissingAndSourceExists(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var oldVolumeRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-target-preserve-source"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)
	require.False(t, oldVolumeRemoved.Load(), "source volume is the only remaining copy and must not be deleted")

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalAfterDBRestoreWhenVolumeRollbackFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			if !targetExists.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-rollback-volume-fail"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "remove rollback target volume web_cache")

	require.Positive(t, targetRemoveAttempts.Load())
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok, "project-state journal should clear after database rollback succeeds")
	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok, "target cleanup should keep retry state when removal fails")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)

	allowTargetRemove.Store(true)
	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetExists.Load())
}

func TestProjectService_RecoverProjectRenameJournals_KeepsRollbackCleanupWhenDockerUnavailable(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	oldPath := filepath.Join(projectsDir, oldDir)
	require.NoError(t, os.MkdirAll(oldPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-rollback-cleanup-docker-unavailable"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameRollbackCleanupKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "docker service unavailable")

	_, ok, err := kvService.Get(ctx, projectRenameRollbackCleanupKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournalWhenSourceAndTargetMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-both-volumes"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsCommittedJournalAndCleansRemainingSourcesWhenSomeVolumesExternallyRemoved(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-mixed-missing-volumes"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, cacheSourceRemoved.Load(), "source volume should still be cleaned up when the target exists")
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_MarksSourceCleanupPendingWhenCommittedCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var sourceRemoveAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-source-cleanup-fail"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	var cleanupErr *volumes.SourceCleanupError
	require.ErrorAs(t, err, &cleanupErr)
	require.Equal(t, "nginx_data", cleanupErr.SourceVolume)
	require.Positive(t, sourceRemoveAttempts.Load())

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsSourceCleanupPendingJournalAfterCleanup(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var sourceRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-source-cleanup-pending-clear"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, sourceRemoved.Load())
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)
}

func TestProjectService_RecoverProjectRenameJournals_RollsBackSourceCleanupPendingWhenTargetMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-source-cleanup-target-missing"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	_, ok, err = kvService.Get(ctx, projectRenameRollbackCleanupKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, dataSourceRemoved.Load(), "source volume is the remaining data copy and must not be removed")
	require.False(t, dataTargetRemoved.Load(), "missing target should not be removed")
	require.True(t, cacheTargetRemoved.Load(), "safe target volume should still be cleaned during rollback")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
	require.NotNil(t, fromDB.DirName)
	require.Equal(t, oldDir, *fromDB.DirName)
}

func TestProjectService_RecoverProjectRenameJournals_KeepsSourceCleanupPendingJournalWhenCleanupFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var sourceRemoveAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-source-cleanup-pending-fail"},
		Name:      "web",
		DirName:   &newDir,
		Path:      newPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.Error(t, err)
	var cleanupErr *volumes.SourceCleanupError
	require.ErrorAs(t, err, &cleanupErr)
	require.Equal(t, "nginx_data", cleanupErr.SourceVolume)
	require.Positive(t, sourceRemoveAttempts.Load())

	raw, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.True(t, ok)

	var updatedJournal projectRenameJournalInternal
	require.NoError(t, json.Unmarshal([]byte(raw), &updatedJournal))
	require.Equal(t, projectRenameJournalPhaseSourceCleanupPendingInternal, updatedJournal.Phase)
	require.DirExists(t, newPath)
	require.NoDirExists(t, oldPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "web", fromDB.Name)
	require.Equal(t, newPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsStartedJournalWhenDirectoriesAreMissing(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	projectsDir := t.TempDir()
	oldDir := "nginx"
	newDir := "web"
	oldPath := filepath.Join(projectsDir, oldDir)
	newPath := filepath.Join(projectsDir, newDir)

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-directories"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	svc := NewProjectService(db, nil, nil, nil, nil, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)
	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsMissingPathJournalWhenTargetPreserved(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-missing-path-preserved-target"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok)
	require.False(t, targetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.NoDirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenRollbackSourceInspectFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-source-inspect-preserve"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok, "inspect uncertainty should not permanently block future renames")
	require.False(t, targetRemoved.Load(), "target volume must not be deleted when source inspection is uncertain")
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenRollbackTargetInspectFails(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "nginx_data"}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-target-inspect-preserve"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	require.NoError(t, svc.RecoverProjectRenameJournals(ctx))

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok, "inspect uncertainty should not permanently block future renames")
	require.False(t, targetRemoved.Load(), "target volume must not be deleted when target inspection is uncertain")
	require.DirExists(t, oldPath)
	require.NoDirExists(t, newPath)

	var fromDB models.Project
	require.NoError(t, db.First(&fromDB, "id = ?", project.ID).Error)
	require.Equal(t, "nginx", fromDB.Name)
	require.Equal(t, oldPath, fromDB.Path)
}

func TestProjectService_RecoverProjectRenameJournals_ClearsJournalWhenTargetPreservedAndDirectoryRolledBack(t *testing.T) {
	db := setupProjectTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))
	ctx := context.Background()

	var targetRemoved atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(volume.Volume{Name: "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-rename-preserved-target-retry"},
		Name:      "nginx",
		DirName:   &oldDir,
		Path:      oldPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	kvService := NewKVService(db)
	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	svc := NewProjectService(db, nil, nil, nil, dockerService, nil, nil, config.Load()).WithKVService(kvService)
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
	require.NoError(t, kvService.Set(ctx, projectRenameJournalKeyInternal(project.ID), string(payload)))

	err = svc.RecoverProjectRenameJournals(ctx)
	require.NoError(t, err)

	_, ok, err := kvService.Get(ctx, projectRenameJournalKeyInternal(project.ID))
	require.NoError(t, err)
	require.False(t, ok, "preserved target data should not leave the project permanently blocked")
	require.False(t, targetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.FileExists(t, filepath.Join(oldPath, "compose.yaml"))
	require.NoDirExists(t, newPath)
}
