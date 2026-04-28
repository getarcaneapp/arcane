package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	libupdater "github.com/getarcaneapp/arcane/backend/pkg/libarcane/updater"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	buildtypes "github.com/getarcaneapp/arcane/types/builds"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
	dockertypesimage "github.com/moby/moby/api/types/image"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

type testBuildBuilder struct {
	err error
}

func (b testBuildBuilder) BuildImage(_ context.Context, _ imagetypes.BuildRequest, _ io.Writer, _ string) (*imagetypes.BuildResult, error) {
	if b.err != nil {
		return nil, b.err
	}
	return &imagetypes.BuildResult{Provider: "local"}, nil
}

var _ buildtypes.Builder = testBuildBuilder{}

func setupProjectTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Project{}, &models.SettingVariable{}, &models.ImageUpdateRecord{}, &models.Event{}))
	return &database.DB{DB: db}
}

func newProjectImagePullServer(t *testing.T, inspectByRef map[string]dockertypesimage.InspectResponse) *httptest.Server {
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

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"Pulled"}`+"\n")
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
	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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
		svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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
		svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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
		svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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
		svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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
		svc := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())

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

func TestProjectService_PullProjectImages_ClearsUpdateRecordsForPulledNonBuildRefs(t *testing.T) {
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
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, "compose-pull")
	composeContent := fmt.Sprintf("services:\n  app:\n    image: %s\n  builder:\n    build: .\n", imageRef)
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte(composeContent), 0o644))

	projectRecord := &models.Project{
		BaseModel: models.BaseModel{ID: "project-pull"},
		Name:      "compose-pull",
		DirName:   ptr("compose-pull"),
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

	var fullRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-full").First(&fullRecord).Error)
	assert.False(t, fullRecord.HasUpdate)

	var shortRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-short").First(&shortRecord).Error)
	assert.False(t, shortRecord.HasUpdate)

	var buildRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:build-only").First(&buildRecord).Error)
	assert.True(t, buildRecord.HasUpdate)

	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, repository, currentRecord.Repository)
	assert.Equal(t, "1.2.3", currentRecord.Tag)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.CurrentDigest))
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_EnsureImagesPresent_ClearsUpdateRecordsAfterPull(t *testing.T) {
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
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, config.Load())

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

	var oldRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-api").First(&oldRecord).Error)
	assert.False(t, oldRecord.HasUpdate)

	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_PullImageForService_ClearsUpdateRecordsAfterPull(t *testing.T) {
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
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, config.Load())

	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:old-worker",
		Repository:     repository,
		Tag:            "3.1.4",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "3.1.4",
		CheckTime:      time.Now().UTC().Add(-time.Hour),
	}).Error)

	require.NoError(t, svc.pullImageForService(ctx, imageRef, io.Discard, nil))

	var oldRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:old-worker").First(&oldRecord).Error)
	assert.False(t, oldRecord.HasUpdate)

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

	imageRef := "registry.example.com/team/app:9.9.9"
	repository := "registry.example.com/team/app"
	imageID := "sha256:team-app-compose"
	imageDigest := digest.FromString("team-app-compose-digest").String()

	server := newProjectImagePullServer(t, map[string]dockertypesimage.InspectResponse{
		imageRef: {
			ID:          imageID,
			RepoTags:    []string{imageRef},
			RepoDigests: []string{repository + "@" + imageDigest},
		},
	})

	dockerService := &DockerClientService{client: newTestDockerClient(t, server)}
	eventService := NewEventService(db, nil, nil)
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, eventService)
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, config.Load())

	projectDef := &composetypes.Project{
		Name: "compose-selected",
		Services: composetypes.Services{
			"app": {
				Name:  "app",
				Image: imageRef,
			},
			"sidecar": {
				Name:  "sidecar",
				Image: "registry.example.com/team/sidecar:1.0.0",
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
		Repository:     repository,
		Tag:            "9.9.9",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "9.9.9",
		CheckTime:      now,
	}).Error)
	require.NoError(t, db.Create(&models.ImageUpdateRecord{
		ID:             "sha256:sidecar-old",
		Repository:     "registry.example.com/team/sidecar",
		Tag:            "1.0.0",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "1.0.0",
		CheckTime:      now,
	}).Error)

	originalComposePull := composePullProjectServicesInternal
	t.Cleanup(func() { composePullProjectServicesInternal = originalComposePull })

	composePullProjectServicesInternal = func(_ context.Context, _ *composetypes.Project, services []string) error {
		assert.Equal(t, []string{"app"}, services)
		return nil
	}

	require.NoError(t, svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app"}))

	var selectedRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.False(t, selectedRecord.HasUpdate)

	var sidecarRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:sidecar-old").First(&sidecarRecord).Error)
	assert.True(t, sidecarRecord.HasUpdate)

	var currentRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", imageID).First(&currentRecord).Error)
	assert.False(t, currentRecord.HasUpdate)
	assert.Equal(t, imageDigest, stringPtrToString(currentRecord.LatestDigest))
}

func TestProjectService_ComposePullSelectedServicesInternal_LeavesRecordsWhenPullFails(t *testing.T) {
	ctx := context.Background()
	db := setupProjectTestDB(t)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	imageRef := "registry.example.com/team/app:9.9.9"
	repository := "registry.example.com/team/app"

	dockerService := &DockerClientService{}
	imageUpdateService := NewImageUpdateService(db, nil, nil, dockerService, nil, nil)
	imageService := NewImageService(db, dockerService, nil, imageUpdateService, nil, NewEventService(db, nil, nil))
	svc := NewProjectService(db, settingsService, nil, imageService, dockerService, nil, config.Load())

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

	originalComposePull := composePullProjectServicesInternal
	t.Cleanup(func() { composePullProjectServicesInternal = originalComposePull })

	composePullProjectServicesInternal = func(context.Context, *composetypes.Project, []string) error {
		return errors.New("compose pull failed")
	}

	err = svc.composePullSelectedServicesInternal(ctx, projectDef, []string{"app"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "compose pull failed")

	var selectedRecord models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", "sha256:selected-old").First(&selectedRecord).Error)
	assert.True(t, selectedRecord.HasUpdate)

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.ImageUpdateRecord{}).Where("id = ?", "sha256:team-app-compose").Count(&count).Error)
	assert.Zero(t, count)
}

func TestProjectService_UpdateProject_RenamesDirectoryWhenNameChanges(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := filepath.Join(projectsDir, originalDirName)
	require.NoError(t, os.MkdirAll(originalPath, 0o755))

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "proj-1"},
		Name:      "Foo",
		DirName:   &originalDirName,
		Path:      originalPath,
		Status:    models.ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, err := svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

	originalDirName := "Foo"
	originalPath := filepath.Join(projectsDir, originalDirName)
	require.NoError(t, os.MkdirAll(originalPath, 0o755))

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

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	_, err = svc.UpdateProject(ctx, project.ID, new("bar"), nil, nil, models.User{
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

func TestProjectService_UpdateProject_ValidatesComposeUsingExistingProjectName(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, ptr(compose), nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	includePath := filepath.Join(projectPath, "metadata.yaml")
	assert.NoFileExists(t, includePath)

	details, err := svc.GetProjectDetails(ctx, project.ID)
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, ptr(compose), nil, models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "invalid compose file")
	assert.NoFileExists(t, filepath.Join(projectsDir, "metadata.yaml"))
	assert.NoFileExists(t, filepath.Join(projectPath, "compose.yaml"))
}

func TestProjectService_UpdateProject_UsesExistingEnvFileDuringComposeValidation(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), new(env), models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(compose), nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nTOKEN=secret\n"), models.User{
		BaseModel: models.BaseModel{ID: "u1"},
		Username:  "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	overrideBytes, readErr := os.ReadFile(filepath.Join(projectPath, "project.env"))
	require.NoError(t, readErr)
	assert.Equal(t, "TOKEN=secret\n", string(overrideBytes))

	effectiveBytes, readErr := os.ReadFile(filepath.Join(projectPath, ".env"))
	require.NoError(t, readErr)
	assert.Contains(t, string(effectiveBytes), "BASE=git\n")
	assert.Contains(t, string(effectiveBytes), "TOKEN=secret\n")
}

func TestProjectService_UpdateProject_DeletingGitBackedKeyFallsBackToGit(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	eventService := NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("BASE=git\nLOCAL_ONLY=1\n"), models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	details, err := svc.GetProjectDetails(ctx, project.ID)
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
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, config.Load())

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

	details, err := svc.GetProjectDetails(ctx, projectRecord.ID)
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

func TestProjectService_ListProjects_FiltersByUpdateStatus(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	imageService := &ImageService{db: db}
	svc := NewProjectService(db, settingsService, nil, imageService, nil, nil, config.Load())

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
				PaginationParams: pagination.PaginationParams{Limit: -1},
				SortParams:       pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
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

func TestProjectService_MergeBuildTags(t *testing.T) {
	tags := mergeBuildTags("example/app:latest", []string{"example/app:sha", "example/app:latest", " "})
	assert.Equal(t, []string{"example/app:latest", "example/app:sha"}, tags)
}

func TestProjectService_ListProjects_WithDerivedStatusFilter_AllowsAllPageSizeSentinel(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	for i := 0; i < 25; i++ {
		projectPath := createComposeProjectDir(t, projectsRoot, fmt.Sprintf("stopped-%02d", i))
		require.NoError(t, db.Create(&models.Project{
			BaseModel: models.BaseModel{ID: fmt.Sprintf("project-%02d", i)},
			Name:      fmt.Sprintf("stopped-%02d", i),
			DirName:   ptr(fmt.Sprintf("stopped-%02d", i)),
			Path:      projectPath,
			Status:    models.ProjectStatusStopped,
		}).Error)
	}

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		Filters: map[string]string{
			"status": string(models.ProjectStatusStopped),
		},
		PaginationParams: pagination.PaginationParams{Limit: -1},
		SortParams:       pagination.SortParams{Sort: "name", Order: pagination.SortAsc},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 25, page.TotalItems)
	require.Len(t, items, 25)
	assert.Equal(t, "stopped-00", items[0].Name)
	assert.Equal(t, "stopped-24", items[len(items)-1].Name)
}

func TestProjectService_BuildPlatformsFromCompose(t *testing.T) {
	t.Run("uses service platform when build platforms missing", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context: ".",
			},
		}

		platforms := buildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/amd64"}, platforms)
	})

	t.Run("keeps explicit build platforms", func(t *testing.T) {
		svc := composetypes.ServiceConfig{
			Platform: "linux/amd64",
			Build: &composetypes.BuildConfig{
				Context:   ".",
				Platforms: []string{"linux/arm64"},
			},
		}

		platforms := buildPlatformsFromCompose(svc)
		assert.Equal(t, []string{"linux/arm64"}, platforms)
	})
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
		nil,
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
	pm := projects.NewPathMapper("/app/data/projects", "/docker-data/arcane/projects")

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
		pm,
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
	pm := projects.NewPathMapper("/app/data/projects", "/storage/volumes/arcane/projects")

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
		pm,
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
		nil,
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
		nil,
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
		nil,
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
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, config.Load())

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
	svc := NewProjectService(db, settingsService, nil, nil, nil, buildSvc, config.Load())

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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams:       pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		PaginationParams: pagination.PaginationParams{Limit: -1},
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "metadata.yaml"), []byte(`x-watchtower-icon: &watchtower-icon "${ICON_CDN_URL:+${ICON_CDN_URL}/svg/watchtower.svg}"
x-arcane:
  icon: *watchtower-icon
services:
  watchtower:
    labels:
      com.getarcaneapp.arcane.icon: *watchtower-icon
`), 0o600))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams:       pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		PaginationParams: pagination.PaginationParams{Limit: -1},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.TotalItems)
	require.Len(t, items, 1)
	assert.Equal(t, "https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/watchtower.svg", items[0].IconURL)
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

func TestProjectService_SyncProjectsFromFileSystem_AllowsDuplicateLeafDirectoriesInDifferentParents(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	settingsService, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	projectsRoot := t.TempDir()
	firstPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project1", "app"))
	secondPath := createComposeProjectDir(t, projectsRoot, filepath.Join("main-project2", "app"))

	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsRoot))

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))

	items, page, err := svc.ListProjects(ctx, pagination.QueryParams{
		SortParams:       pagination.SortParams{Sort: "path", Order: pagination.SortAsc},
		PaginationParams: pagination.PaginationParams{Limit: -1},
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())
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

	svc := NewProjectService(db, settingsService, nil, nil, nil, nil, config.Load())

	composeFromContent, envFromContent, err := svc.GetProjectContent(ctx, syncProjectID)
	require.NoError(t, err)
	assert.Equal(t, composeContent, composeFromContent)
	assert.Equal(t, "TZ=UTC\n", envFromContent)

	details, err := svc.GetProjectDetails(ctx, syncProjectID)
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, config.Load())

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

	updated, err := svc.UpdateProject(ctx, project.ID, nil, new(updatedCompose), new(updatedEnv), models.User{
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

//go:fix inline
func ptr(v string) *string {
	return new(v)
}
