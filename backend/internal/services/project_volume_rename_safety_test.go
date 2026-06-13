package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestProjectVolumeCopyRequiredBytesInternal_AddsMargin(t *testing.T) {
	require.EqualValues(t, projectVolumeCopyMinMarginBytesInternal, projectVolumeCopyRequiredBytesInternal(0))
	require.EqualValues(t, 1024+projectVolumeCopyMinMarginBytesInternal, projectVolumeCopyRequiredBytesInternal(1024))

	largeSource := uint64(10 * projectVolumeCopyMinMarginBytesInternal)
	require.Equal(t, largeSource+(largeSource/10), projectVolumeCopyRequiredBytesInternal(largeSource))
	require.Equal(t, ^uint64(0), projectVolumeCopyRequiredBytesInternal(^uint64(0)))
}

func TestEnsureProjectVolumeCopyCapacityInternal_ReturnsInsufficientSpace(t *testing.T) {
	err := ensureProjectVolumeCopyCapacityInternal(
		projectVolumeCopyProbeInternal{AllocatedBytes: 1024},
		projectVolumeCopyProbeInternal{AvailableBytes: 1024},
		"nginx_data",
		"web_data",
	)

	var spaceErr *ProjectVolumeRenameInsufficientSpaceError
	require.ErrorAs(t, err, &spaceErr)
	require.Equal(t, "nginx_data", spaceErr.SourceVolume)
	require.Equal(t, "web_data", spaceErr.TargetVolume)
	require.Contains(t, spaceErr.Detail, "source=1024B")
	require.Contains(t, spaceErr.Detail, "available=1024B")
}

func TestIsProjectVolumeCopyNoSpaceErrorInternal(t *testing.T) {
	require.True(t, isProjectVolumeCopyNoSpaceErrorInternal(syscall.ENOSPC))
	require.True(t, isProjectVolumeCopyNoSpaceErrorInternal(errors.New("write target volume archive: no space left on device")))
	require.False(t, isProjectVolumeCopyNoSpaceErrorInternal(errors.New("permission denied")))
}

func TestEnsureProjectRenameTargetVolumeAbsentInternal_ReturnsConflictWhenTargetExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/volumes/web_data") {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Name": "web_data",
			}))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	dockerClient := newTestDockerClient(t, server)

	err := ensureProjectRenameTargetVolumeAbsentInternal(context.Background(), dockerClient, "web_data")

	var conflictErr *ProjectVolumeRenameConflictError
	require.ErrorAs(t, err, &conflictErr)
	require.Equal(t, "web_data", conflictErr.VolumeName)
}

func TestEnsureProjectRenameSourceVolumeDetachedInternal_ReturnsConflictWhenContainerUsesVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID: "stopped-container",
					Mounts: []container.MountPoint{
						{Type: mount.TypeVolume, Name: "nginx_data"},
					},
				},
			}))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	dockerClient := newTestDockerClient(t, server)

	err := ensureProjectRenameSourceVolumeDetachedInternal(context.Background(), dockerClient, "nginx_data")

	var inUseErr *ProjectVolumeRenameInUseError
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
				require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:    "agent-container",
					Image: "arcane-agent:local",
					State: container.StateRunning,
				},
			}))
		case strings.Contains(r.URL.Path, "/containers/agent-container/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(container.InspectResponse{
				ID: "agent-container",
				Config: &container.Config{
					Image: "arcane-agent:local",
					Cmd:   []string{"./arcane-agent"},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	copyRuntime, err := getProjectVolumeCopyRuntimeInternal(context.Background(), newTestDockerClient(t, server))

	require.NoError(t, err)
	require.Equal(t, "arcane-agent:local", copyRuntime.Image)
	require.Equal(t, []string{"./arcane-agent"}, copyRuntime.Command)
	require.Equal(t, "arcane-agent-label", copyRuntime.Source)
}

func TestProjectVolumeCopyHolderContainerInternal_RemovesHelperWhenContextIsCanceled(t *testing.T) {
	started := make(chan struct{})
	deleted := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"Id":       "helper-container",
				"Warnings": []string{},
			}))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/start"):
			w.WriteHeader(http.StatusNoContent)
			close(started)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/wait"):
			<-r.Context().Done()
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/helper-container"):
			w.WriteHeader(http.StatusNoContent)
			close(deleted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	go func() {
		<-started
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	dockerClient := newTestDockerClient(t, server)
	func() {
		containerID, cleanup, err := createProjectVolumeCopyHolderContainerInternal(
			ctx,
			dockerClient,
			projectVolumeCopyRuntimeInternal{Image: "arcane:local", Command: []string{"./arcane"}},
			"nginx_data",
			true,
		)
		require.NoError(t, err)
		defer cleanup()

		_, err = startProjectVolumeHelperContainerInternal(ctx, dockerClient, containerID)
		require.ErrorIs(t, err, context.Canceled)
	}()

	select {
	case <-deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected helper container to be removed after context cancellation")
	}
}

func TestStartProjectVolumeHelperContainerInternal_ReturnsWaitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/wait"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("wait interrupted"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	_, err := startProjectVolumeHelperContainerInternal(context.Background(), newTestDockerClient(t, server), "helper-container")

	require.Error(t, err)
	require.ErrorContains(t, err, "wait interrupted")
}

func TestProbeProjectVolumeCopyContainerInternal_IgnoresStderrOnSuccess(t *testing.T) {
	expected := projectVolumeCopyProbeInternal{
		Path:           projectVolumeCopyMountPathInternal,
		AllocatedBytes: 1024,
		AvailableBytes: 1024 * 1024,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/wait"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"StatusCode": 0}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/helper-container/logs"):
			writeProjectVolumeCopyProbeLogInternal(t, w, expected, "startup log")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	got, err := probeProjectVolumeCopyContainerInternal(context.Background(), newTestDockerClient(t, server), "helper-container")

	require.NoError(t, err)
	require.Equal(t, expected, got)
}

func TestStartProjectVolumeHelperContainerInternal_ReturnsCombinedOutputOnFailure(t *testing.T) {
	probe := projectVolumeCopyProbeInternal{
		Path:           projectVolumeCopyMountPathInternal,
		AllocatedBytes: 1024,
		AvailableBytes: 1024 * 1024,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-container/wait"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"StatusCode": 1}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/helper-container/logs"):
			writeProjectVolumeCopyProbeLogInternal(t, w, probe, "startup failure")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	output, err := startProjectVolumeHelperContainerInternal(context.Background(), newTestDockerClient(t, server), "helper-container")

	require.Error(t, err)
	require.Contains(t, output, "startup failure")
	require.Contains(t, output, `"path":"/volume"`)
	require.ErrorContains(t, err, "startup failure")
	require.ErrorContains(t, err, `"path":"/volume"`)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackPreservesTargetWhenSourceMissing(t *testing.T) {
	var targetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			targetRemoved.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	migration := &dockerProjectVolumeRenameMigrationInternal{
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		createdNew: []projectVolumeRenameEntryInternal{
			{OldName: "nginx_data", NewName: "web_data"},
		},
		removedOld: []projectVolumeRenameEntryInternal{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(context.Background())

	require.Error(t, err)
	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
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
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		createdNew: []projectVolumeRenameEntryInternal{
			{OldName: "nginx_data", NewName: "web_data"},
			{OldName: "nginx_cache", NewName: "web_cache"},
		},
		removedOld: []projectVolumeRenameEntryInternal{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(context.Background())

	require.Error(t, err)
	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
	require.ErrorAs(t, err, &preserved)
	require.False(t, preservedTargetRemoved.Load(), "target volume may be the only complete copy and must stay when source restore fails")
	require.True(t, safeTargetRemoved.Load(), "targets without removed sources should still be cleaned up")
	require.Equal(t, []projectVolumeRenameEntryInternal{{OldName: "nginx_data", NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_RollbackRemovesTargetsWhenSourcesRemain(t *testing.T) {
	var targetRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
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
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		createdNew: []projectVolumeRenameEntryInternal{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(context.Background())

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
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		createdNew: []projectVolumeRenameEntryInternal{
			{NewName: "web_data"},
		},
		removedOld: []projectVolumeRenameEntryInternal{
			{
				OldName: "nginx_data",
				NewName: "web_data",
			},
		},
	}

	err := migration.Rollback(context.Background())

	require.Error(t, err)
	var preserved *projectRenameTargetPreservedDuringRollbackInternalError
	require.ErrorAs(t, err, &preserved)
	require.False(t, targetRemoved.Load(), "rollback must not delete target volumes after source cleanup has started")
	require.Equal(t, []projectVolumeRenameEntryInternal{{OldName: "nginx_data", NewName: "web_data"}}, migration.removedOld)
	require.Equal(t, []projectVolumeRenameEntryInternal{{NewName: "web_data"}}, migration.createdNew)
}

func TestDockerProjectVolumeRenameMigrationInternal_CommitPreflightsAllTargetsBeforeRemovingSources(t *testing.T) {
	var firstSourceRemoved atomic.Bool
	var secondSourceRemoved atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_cache"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_cache"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "nginx_cache"}))
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
		service: &ProjectService{
			dockerService: &DockerClientService{client: newTestDockerClient(t, server)},
		},
		entries: []projectVolumeRenameEntryInternal{
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

	err := migration.Commit(context.Background())

	var missingTarget *projectRenameTargetMissingWithSourceInternalError
	require.ErrorAs(t, err, &missingTarget)
	require.Equal(t, "nginx_cache", missingTarget.SourceVolume)
	require.Equal(t, "web_cache", missingTarget.TargetVolume)
	require.False(t, firstSourceRemoved.Load(), "no source volume should be removed until every target is verified")
	require.False(t, secondSourceRemoved.Load())
}

func writeProjectVolumeCopyProbeLogInternal(t *testing.T, w http.ResponseWriter, probe projectVolumeCopyProbeInternal, stderr ...string) {
	t.Helper()

	payload, err := json.Marshal(probe)
	require.NoError(t, err)

	var stream bytes.Buffer
	for _, message := range stderr {
		writeProjectVolumeCopyLogFrameInternal(t, &stream, 2, []byte(message+"\n"))
	}
	writeProjectVolumeCopyLogFrameInternal(t, &stream, 1, append(payload, '\n'))

	_, err = w.Write(stream.Bytes())
	require.NoError(t, err)
}

func writeProjectVolumeCopyLogFrameInternal(t *testing.T, stream *bytes.Buffer, streamID byte, payload []byte) {
	t.Helper()

	header := make([]byte, 8)
	header[0] = streamID
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	_, err := stream.Write(header)
	require.NoError(t, err)
	_, err = stream.Write(payload)
	require.NoError(t, err)
}

func setProjectVolumeCopyArchiveStatHeaderInternal(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	payload, err := json.Marshal(container.PathStat{Name: "."})
	require.NoError(t, err)
	w.Header().Set("X-Docker-Container-Path-Stat", base64.StdEncoding.EncodeToString(payload))
}
