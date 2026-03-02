package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
)

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
	svc := NewProjectService(db, settingsService, nil, nil, nil, nil)

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
	svc := NewProjectService(db, nil, nil, nil, nil, nil)

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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil)

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

	updatedName := "bar"
	updated, err := svc.UpdateProject(ctx, project.ID, &updatedName, nil, nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil)

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

	updatedName := "bar"
	_, err = svc.UpdateProject(ctx, project.ID, &updatedName, nil, nil, models.User{
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
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil)

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

	updatedName := "bar"
	_, err = svc.UpdateProject(ctx, project.ID, &updatedName, nil, nil, models.User{
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

		decision := decideDeployImageAction(svc)
		assert.True(t, decision.Build)
		assert.False(t, decision.PullAlways)
	})

	t.Run("build service default policy uses pull then fallback build", func(t *testing.T) {
		svc := composetypes.ServiceConfig{Build: &composetypes.BuildConfig{Context: "."}}
		decision := decideDeployImageAction(svc)
		assert.True(t, decision.PullIfMissing)
		assert.True(t, decision.FallbackBuildOnPullFail)
		assert.False(t, decision.Build)
	})

	t.Run("non-build service never policy requires local only", func(t *testing.T) {
		svc := composetypes.ServiceConfig{PullPolicy: "never"}
		decision := decideDeployImageAction(svc)
		assert.True(t, decision.RequireLocalOnly)
		assert.False(t, decision.PullIfMissing)
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

	push := true
	_, _, _, err = svc.prepareServiceBuildRequest(
		context.Background(),
		"project-id",
		proj,
		"web",
		serviceCfg,
		ProjectBuildOptions{Provider: "local", Push: &push},
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define an image when push is enabled")
}

//go:fix inline
func ptr(v string) *string {
	return new(v)
}
