package volumes

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
)

func TestIsProjectVolumeCopyNoSpaceError(t *testing.T) {
	require.True(t, isProjectVolumeCopyNoSpaceError(syscall.ENOSPC))
	require.True(t, isProjectVolumeCopyNoSpaceError(errors.New("write target volume archive: no space left on device")))
	require.False(t, isProjectVolumeCopyNoSpaceError(errors.New("permission denied")))
}

func TestEnsureProjectRenameTargetVolumeAbsentInternal_ReturnsConflictWhenTargetExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/volumes/web_data") {
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name": "web_data",
			})) {
				return
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	dockerClient := newTestDockerClient(t, server)

	err := EnsureRenameTargetAbsent(t.Context(), dockerClient, "web_data")

	var conflictErr *types.ProjectVolumeRenameConflictError
	require.ErrorAs(t, err, &conflictErr)
	require.Equal(t, "web_data", conflictErr.VolumeName)
}

func TestEnsureProjectRenameSourceVolumeDetachedInternal_ReturnsConflictWhenContainerUsesVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID: "stopped-container",
					Mounts: []container.MountPoint{
						{Type: mount.TypeVolume, Name: "nginx_data"},
					},
				},
			})) {
				return
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	dockerClient := newTestDockerClient(t, server)

	err := EnsureRenameSourceDetached(t.Context(), dockerClient, "nginx_data")

	var inUseErr *types.ProjectVolumeRenameInUseError
	require.ErrorAs(t, err, &inUseErr)
	require.Equal(t, "nginx_data", inUseErr.VolumeName)
	require.Equal(t, []string{"stopped-container"}, inUseErr.ContainerIDs)
}

func TestGetProjectVolumeCopyRuntimeInternal_UsesArcaneAgentLabel(t *testing.T) {
	var listCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if listCalls.Add(1) == 1 {
				if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
					return
				}
				return
			}
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:    "agent-container",
					Image: "arcane-agent:local",
					State: container.StateRunning,
				},
			})) {
				return
			}
		case strings.Contains(r.URL.Path, "/containers/agent-container/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID: "agent-container",
				Config: &container.Config{
					Image: "arcane-agent:local",
					Cmd:   []string{"./arcane-agent"},
				},
			})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	copyRuntime, err := getProjectVolumeCopyRuntime(t.Context(), newTestDockerClient(t, server), volumehelper.ToolsImage(""))

	require.NoError(t, err)
	require.Equal(t, "arcane-agent:local", copyRuntime.Image)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackPreservesTargetWhenSourceMissing(t *testing.T) {
	var targetRemoved atomic.Bool

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
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{OldName: "nginx_data", NewName: "web_data"},
		},
		removedOld: []types.VolumeRenameEntry{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, targetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.Len(t, migration.createdNew, 1)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackCleansSafeTargetsWhenSourceMissing(t *testing.T) {
	var preservedTargetRemoved atomic.Bool
	var safeTargetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			preservedTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			safeTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{OldName: "nginx_data", NewName: "web_data"},
			{OldName: "nginx_cache", NewName: "web_cache"},
		},
		removedOld: []types.VolumeRenameEntry{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, preservedTargetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.True(t, safeTargetRemoved.Load(), "targets without removed sources should still be cleaned up")
	require.Equal(t, []types.VolumeRenameEntry{{OldName: "nginx_data", NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackPreservesTargetWhenSourceInspectFails(t *testing.T) {
	var preservedTargetRemoved atomic.Bool
	var safeTargetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.Error(w, "temporary docker error", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			preservedTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			safeTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{OldName: "nginx_data", NewName: "web_data"},
			{OldName: "nginx_cache", NewName: "web_cache"},
		},
	}

	err := migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, preservedTargetRemoved.Load(), "target volume must not be deleted when source inspection is uncertain")
	require.True(t, safeTargetRemoved.Load(), "targets without rollback uncertainty should still be cleaned up")
	require.Equal(t, []types.VolumeRenameEntry{{OldName: "nginx_data", NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackPreservesTargetWhenTargetInspectFails(t *testing.T) {
	var preservedTargetRemoved atomic.Bool
	var safeTargetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			http.Error(w, "temporary docker error", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			preservedTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			safeTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{OldName: "nginx_data", NewName: "web_data"},
			{OldName: "nginx_cache", NewName: "web_cache"},
		},
	}

	err := migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, preservedTargetRemoved.Load(), "target volume must not be deleted when target inspection is uncertain")
	require.True(t, safeTargetRemoved.Load(), "targets without rollback uncertainty should still be cleaned up")
	require.Equal(t, []types.VolumeRenameEntry{{OldName: "nginx_data", NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackRemovesTargetsWhenSourcesRemain(t *testing.T) {
	var targetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/copy-holder-"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(t.Context())

	require.NoError(t, err)
	require.True(t, targetRemoved.Load(), "rollback should remove copied targets while sources still exist")
	require.Empty(t, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackPreservesRemovedOldTargetsWithoutRestoreCopy(t *testing.T) {
	var targetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		createdNew: []types.VolumeRenameEntry{
			{NewName: "web_data"},
		},
		removedOld: []types.VolumeRenameEntry{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, targetRemoved.Load(), "rollback must not delete target volumes after source cleanup has started")
	require.Equal(t, []types.VolumeRenameEntry{{OldName: "nginx_data", NewName: "web_data"}}, migration.removedOld)
	require.Equal(t, []types.VolumeRenameEntry{{NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackAfterPartialCommitCleansSafeTargets(t *testing.T) {
	var firstSourceRemoved atomic.Bool
	var secondSourceRemoveAttempts atomic.Int32
	var preservedTargetRemoved atomic.Bool
	var safeTargetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			firstSourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			secondSourceRemoveAttempts.Add(1)
			http.Error(w, "volume busy", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			preservedTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			safeTargetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	entries := []types.VolumeRenameEntry{
		{OldName: "nginx_data", NewName: "web_data"},
		{OldName: "nginx_cache", NewName: "web_cache"},
	}
	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		entries:      entries,
		createdNew:   append([]types.VolumeRenameEntry(nil), entries...),
	}

	err := migration.Commit(t.Context())

	var cleanupErr *types.SourceCleanupError
	require.ErrorAs(t, err, &cleanupErr)
	require.Equal(t, "nginx_cache", cleanupErr.SourceVolume)
	require.True(t, firstSourceRemoved.Load())
	require.Positive(t, secondSourceRemoveAttempts.Load())
	require.Equal(t, []types.VolumeRenameEntry{entries[0]}, migration.removedOld)

	err = migration.Rollback(t.Context())

	require.Error(t, err)
	var preserved *types.TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.False(t, preservedTargetRemoved.Load(), "target volume may be the only complete copy and must stay when source cleanup has started")
	require.True(t, safeTargetRemoved.Load(), "targets whose source volumes remain should still be cleaned up")
	require.Equal(t, []types.VolumeRenameEntry{entries[0]}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_CommitPreflightsAllTargetsBeforeRemovingSources(t *testing.T) {
	var firstSourceRemoved atomic.Bool
	var secondSourceRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"})) {
				return
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"})) {
				return
			}
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			firstSourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			secondSourceRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		dockerClient: newTestDockerClient(t, server),
		entries: []types.VolumeRenameEntry{
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

	err := migration.Commit(t.Context())

	var missingTarget *types.TargetMissingWithSourceError
	require.ErrorAs(t, err, &missingTarget)
	require.Equal(t, "nginx_cache", missingTarget.SourceVolume)
	require.Equal(t, "web_cache", missingTarget.TargetVolume)
	require.False(t, firstSourceRemoved.Load(), "no source volume should be removed until every target is verified")
	require.False(t, secondSourceRemoved.Load())
}
