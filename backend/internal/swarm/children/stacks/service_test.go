package stacks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
)

func setupSwarmServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &environment.Environment{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForSwarmTest(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

type stackSourceDeployRecorder struct {
	calls   int
	lastReq swarm.StackDeployRequest
}

func stubStackSourceUpdateDeploy(t *testing.T) *stackSourceDeployRecorder {
	t.Helper()
	original := deployStackAfterSourceUpdate
	t.Cleanup(func() { deployStackAfterSourceUpdate = original })
	rec := &stackSourceDeployRecorder{}
	deployStackAfterSourceUpdate = func(_ *Service, _ context.Context, _ string, req swarm.StackDeployRequest) (*swarm.StackDeployResponse, error) {
		rec.calls++
		rec.lastReq = req
		return &swarm.StackDeployResponse{Name: req.Name}, nil
	}
	return rec
}

func TestService_UpdateAndGetStackSource_UsesStoredFilesWithoutSwarmManager(t *testing.T) {
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)

	svc := NewService(nil, nil, nil, nil, nil, settingsSvc)
	deployRec := stubStackSourceUpdateDeploy(t)

	updated, err := svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)
	require.Equal(t, "demo-stack", updated.Name)
	// Saving stack source must trigger an actual stack deploy (#3463).
	require.Equal(t, 1, deployRec.calls)
	// The edit-path redeploy must carry registry auth, the same as Git Sync (#3778):
	// a service added in the edit has no previous spec to fall back on.
	require.True(t, deployRec.lastReq.WithRegistryAuth)

	composePath := filepath.Join(rootDir, "0", "demo-stack", "compose.yaml")
	envPath := filepath.Join(rootDir, "0", "demo-stack", ".env")
	require.FileExists(t, composePath)
	require.FileExists(t, envPath)

	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, updated.ComposeContent, source.ComposeContent)
	require.Equal(t, updated.EnvContent, source.EnvContent)

	// Test with additional files
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		Files: []swarm.SyncFile{
			{RelativePath: "config/nginx.conf", Content: []byte("worker_processes 1;")},
			{RelativePath: "scripts/setup.sh", Content: []byte("#!/bin/sh")},
		},
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "config", "nginx.conf"))
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "scripts", "setup.sh"))

	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Len(t, source.Files, 2)

	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		Files: []swarm.SyncFile{
			{RelativePath: "config/nginx.conf", Content: []byte("worker_processes auto;")},
		},
	})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(rootDir, "0", "demo-stack", "scripts", "setup.sh"))
	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Len(t, source.Files, 1)
	require.Equal(t, []byte("worker_processes auto;"), source.Files[0].Content)
}

func TestService_UpdateAndGetStackSource_RoundTripsOverride(t *testing.T) {
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)

	svc := NewService(nil, nil, nil, nil, nil, settingsSvc)
	stubStackSourceUpdateDeploy(t)

	overridePath := filepath.Join(rootDir, "0", "demo-stack", "compose.override.yaml")

	updated, err := svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent:  "services:\n  web:\n    image: nginx:alpine\n",
		OverrideContent: "services:\n  web:\n    image: busybox:latest\n",
	})
	require.NoError(t, err)
	require.Equal(t, "services:\n  web:\n    image: busybox:latest\n", updated.OverrideContent)
	require.FileExists(t, overridePath)

	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, updated.OverrideContent, source.OverrideContent)
	// The override is a first-class field, never surfaced as an extra file.
	require.Empty(t, source.Files)

	// Clearing the override removes the file so a UI redeploy stops merging it.
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)
	require.NoFileExists(t, overridePath)

	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Empty(t, source.OverrideContent)
}

func TestService_UpdateStackSource_PrunesAndRestoresOnDeployFailure(t *testing.T) {
	ctx := t.Context()
	db := setupSwarmServiceTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTest(t, ctx, db)
	require.NoError(t, err)

	svc := NewService(nil, nil, nil, nil, nil, settingsSvc)

	original := deployStackAfterSourceUpdate
	t.Cleanup(func() { deployStackAfterSourceUpdate = original })
	var lastReq swarm.StackDeployRequest
	var deployErr error
	deployStackAfterSourceUpdate = func(_ *Service, _ context.Context, _ string, req swarm.StackDeployRequest) (*swarm.StackDeployResponse, error) {
		lastReq = req
		if deployErr != nil {
			return nil, deployErr
		}
		return &swarm.StackDeployResponse{Name: req.Name}, nil
	}

	firstCompose := "services:\n  web:\n    image: nginx:alpine\n"
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: firstCompose,
	})
	require.NoError(t, err)
	// The saved source is the full stack spec: services removed from it must
	// be pruned from the swarm on redeploy.
	require.True(t, lastReq.Prune)

	deployErr = errors.New("deploy failed")
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarm.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:broken\n",
	})
	require.ErrorContains(t, err, "deploy failed")

	// A failed deploy must not commit the edited source.
	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, firstCompose, source.ComposeContent)
}
