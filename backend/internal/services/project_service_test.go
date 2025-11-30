package services

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	glsqlite "github.com/glebarez/sqlite"
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
	svc := NewProjectService(db, settingsService, nil, nil)

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
	svc := NewProjectService(db, nil, nil, nil)

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
		input    []container.Port
		expected []string
	}{
		{
			name: "public port",
			input: []container.Port{
				{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
			},
			expected: []string{"8080:80/tcp"},
		},
		{
			name: "private only",
			input: []container.Port{
				{PrivatePort: 80, Type: "tcp"},
			},
			expected: []string{"80/tcp"},
		},
		{
			name:     "empty",
			input:    []container.Port{},
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
