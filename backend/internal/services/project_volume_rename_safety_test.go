package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestProjectVolumeCopyCommandInternal_ChecksCapacityBeforeCopy(t *testing.T) {
	command := projectVolumeCopyCommandInternal()

	require.Contains(t, command, "du -sk /from")
	require.Contains(t, command, "df -Pk /to")
	require.Contains(t, command, "required_kb")
	require.Contains(t, command, "exit 99")
	require.NotContains(t, command, "awk")
	require.Less(t, strings.Index(command, "df -Pk /to"), strings.Index(command, "tar -cf - ."))
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

func TestRunProjectVolumeHelperContainerInternal_RemovesHelperWhenContextIsCanceled(t *testing.T) {
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
	err := runProjectVolumeHelperContainerInternal(ctx, dockerClient, &container.Config{Image: "busybox"}, &container.HostConfig{})

	require.ErrorIs(t, err, context.Canceled)
	select {
	case <-deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected helper container to be removed after context cancellation")
	}
}
