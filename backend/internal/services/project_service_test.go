package services

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	buildtypes "github.com/getarcaneapp/arcane/types/builds"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
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
	require.NoError(t, db.AutoMigrate(&models.Project{}, &models.SettingVariable{}))
	return &database.DB{DB: db}
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

func TestProjectService_MergeBuildTags(t *testing.T) {
	tags := mergeBuildTags("example/app:latest", []string{"example/app:sha", "example/app:latest", " "})
	assert.Equal(t, []string{"example/app:latest", "example/app:sha"}, tags)
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

func TestProjectService_PrepareServiceBuildRequest_UsesExecutorVisiblePaths(t *testing.T) {
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

	assert.Equal(t, "/docker-data/arcane/projects/demo", req.ContextDir)
	assert.Equal(t, "/docker-data/arcane/projects/demo/Dockerfile.custom", req.Dockerfile)
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
