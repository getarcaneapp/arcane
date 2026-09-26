//go:build unix

package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectService_ApplyGitSyncProjectFiles_TolerantOfPermissionLockedEnv verifies
// a permission-locked (e.g. chmod 000, foreign-owned) .env does not brick a git sync:
// the compose file still updates, the sync succeeds, and the locked file is left
// untouched instead of aborting the whole update.
func TestProjectService_ApplyGitSyncProjectFiles_TolerantOfPermissionLockedEnv(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	eventService := event.NewEventService(db, nil, nil)
	svc := NewProjectService(db, settingsService, eventService, nil, nil, nil, nil, nil, config.Load())

	dirName := "git-sync-locked-env"
	projectPath := filepath.Join(projectsDir, dirName)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	envPath := filepath.Join(projectPath, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("FOO=locked\n"), 0o600))
	require.NoError(t, os.Chmod(envPath, 0o000))
	t.Cleanup(func() { _ = os.Chmod(envPath, 0o644) })

	project := &Project{
		ID:      "proj-git-sync-locked-env",
		Name:    "git-sync-locked-env",
		DirName: &dirName,
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	updated, _, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:1.27-alpine\n", new("FOO=fromgit\n"), nil, "", common.User{
		ID:       "u1",
		Username: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	composeBytes, err := os.ReadFile(filepath.Join(projectPath, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")

	// The locked .env survived untouched — sync did not attempt to overwrite it.
	require.NoError(t, os.Chmod(envPath, 0o644))
	envBytes, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, "FOO=locked\n", string(envBytes))
}

func TestPrepareProjectBindDirectoriesInternal_PermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	projectPath := t.TempDir()
	locked := filepath.Join(projectPath, "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { assert.NoError(t, os.Chmod(locked, 0o755)) })

	project := &composetypes.Project{Services: composetypes.Services{
		"app": {Name: "app", Volumes: []composetypes.ServiceVolumeConfig{{
			Type: composetypes.VolumeTypeBind, Source: filepath.Join(locked, "conf"), Target: "/etc/caddy",
		}}},
	}}

	err := prepareProjectBindDirectoriesInternal(projectPath)(context.Background(), project)
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(locked, "conf"))
	assert.Contains(t, err.Error(), "service app")
	assert.NoDirExists(t, filepath.Join(locked, "conf"))
}

func newEnvDirectoryProjectInternal(t *testing.T, id string) (*ProjectService, *Project, string, context.Context) {
	t.Helper()

	db := setupProjectTestDB(t)
	ctx := context.Background()

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)

	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsService.SetStringSetting(ctx, "projectsDirectory", projectsDir))

	svc := NewProjectService(db, settingsService, event.NewEventService(db, nil, nil), nil, nil, nil, nil, nil, config.Load())

	projectPath := createComposeProjectDir(t, projectsDir, id)
	envDir := filepath.Join(projectPath, ".env")
	require.NoError(t, os.Mkdir(envDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(envDir, "keep"), []byte("x"), 0o644))

	project := &Project{
		ID:      "proj-" + id,
		Name:    id,
		DirName: new(id),
		Path:    projectPath,
		Status:  ProjectStatusStopped,
	}
	require.NoError(t, db.Create(project).Error)

	return svc, project, envDir, ctx
}

func assertEnvDirectoryIntactInternal(t *testing.T, envDir string) {
	t.Helper()
	info, err := os.Stat(envDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.FileExists(t, filepath.Join(envDir, "keep"))
}

func TestProjectService_EnvDirectory_SyncAndDetails(t *testing.T) {
	svc, project, envDir, ctx := newEnvDirectoryProjectInternal(t, "env-dir-scan")

	require.NoError(t, svc.SyncProjectsFromFileSystem(ctx))
	all, err := svc.ListAllProjects(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, project.Path, all[0].Path)

	details, err := svc.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
	require.NoError(t, err)
	assert.Empty(t, details.EnvContent)
	assert.Contains(t, details.ComposeContent, "nginx:alpine")

	assertEnvDirectoryIntactInternal(t, envDir)
}

func TestProjectService_EnvDirectory_UpdateCompose(t *testing.T) {
	svc, project, envDir, ctx := newEnvDirectoryProjectInternal(t, "env-dir-update")

	_, err := svc.UpdateProject(ctx, project.ID, nil, new("services:\n  app:\n    image: nginx:1.27-alpine\n"), nil, nil, common.User{ID: "u1", Username: "tester"})
	require.NoError(t, err)

	composeBytes, err := os.ReadFile(filepath.Join(project.Path, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
	assertEnvDirectoryIntactInternal(t, envDir)
}

func TestProjectService_EnvDirectory_GitSync(t *testing.T) {
	svc, project, envDir, ctx := newEnvDirectoryProjectInternal(t, "env-dir-git")

	_, _, err := svc.ApplyGitSyncProjectFiles(ctx, project.ID, "services:\n  app:\n    image: nginx:1.27-alpine\n", new("FOO=fromgit\n"), nil, "", common.User{ID: "u1", Username: "tester"})
	require.NoError(t, err)

	composeBytes, err := os.ReadFile(filepath.Join(project.Path, "compose.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(composeBytes), "nginx:1.27-alpine")
	assertEnvDirectoryIntactInternal(t, envDir)
}

func TestProjectService_EnvDirectory_EnvSaveFails(t *testing.T) {
	svc, project, envDir, ctx := newEnvDirectoryProjectInternal(t, "env-dir-save")

	_, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("FOO=bar\n"), nil, common.User{ID: "u1", Username: "tester"})
	require.ErrorContains(t, err, ".env is a directory")
	assertEnvDirectoryIntactInternal(t, envDir)
}

func TestProjectService_OverrideEnvDirectory_EnvSaveFails(t *testing.T) {
	svc, project, envDir, ctx := newEnvDirectoryProjectInternal(t, "override-dir-save")
	require.NoError(t, os.RemoveAll(envDir))
	require.NoError(t, os.WriteFile(filepath.Join(project.Path, ".env.git"), []byte("FOO=git\n"), 0o644))
	overrideDir := filepath.Join(project.Path, "project.env")
	require.NoError(t, os.Mkdir(overrideDir, 0o755))

	_, err := svc.UpdateProject(ctx, project.ID, nil, nil, new("FOO=local\n"), nil, common.User{ID: "u1", Username: "tester"})
	require.ErrorContains(t, err, "project.env is a directory")
	info, err := os.Stat(overrideDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
