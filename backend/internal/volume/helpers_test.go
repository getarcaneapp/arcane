package volume

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/volumehelper"
)

func newVolumeServiceTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })
	return dockerClient
}

func TestCreateTempContainerReusesWritableHelper(t *testing.T) {
	var createCalls, startCalls, removeCalls int
	var createRequest struct {
		NetworkDisabled bool                  `json:"NetworkDisabled"`
		HostConfig      *container.HostConfig `json:"HostConfig"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{"Id": "tools-image"}); err != nil {
				t.Errorf("encode image inspect response: %v", err)
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Errorf("decode container create request: %v", err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"Id": "helper-1", "Warnings": []string{}}); err != nil {
				t.Errorf("encode container create response: %v", err)
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/helper-1/start"):
			startCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/helper-1/json"):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(container.InspectResponse{
				ID:    "helper-1",
				State: &container.State{Running: true},
			}); err != nil {
				t.Errorf("encode container inspect response: %v", err)
			}
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/containers/helper-1"):
			removeCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected Docker request: "+r.Method+" "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	service := &VolumeService{
		dockerService:  (&docker.DockerClientService{}).WithClient(newVolumeServiceTestDockerClient(t, server)),
		helperByVolume: make(map[string]*volumeHelper),
	}

	firstID, releaseFirst, err := service.acquireVolumeHelper(t.Context(), "workspace-volume")
	require.NoError(t, err)
	secondID, releaseSecond, err := service.acquireVolumeHelper(t.Context(), "workspace-volume")
	require.NoError(t, err)

	require.Equal(t, "helper-1", firstID)
	require.Equal(t, firstID, secondID)
	require.Equal(t, 1, createCalls)
	require.Equal(t, 1, startCalls)
	require.Zero(t, removeCalls)
	require.True(t, createRequest.NetworkDisabled)
	require.NotNil(t, createRequest.HostConfig)
	require.Equal(t, []string{"workspace-volume:/volume"}, createRequest.HostConfig.Binds)
	require.Equal(t, 2, service.helperByVolume["workspace-volume"].inUse)

	service.helperByVolume["workspace-volume"].lastUsedAt = time.Now().Add(-time.Hour)
	require.Empty(t, service.collectStaleHelperIDs(time.Now(), time.Minute))
	require.Contains(t, service.helperByVolume, "workspace-volume")

	releaseFirst()
	releaseSecond()
	require.Zero(t, service.helperByVolume["workspace-volume"].inUse)
}

func TestIsUnlabeledVolumeHelperContainer(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "unlabeled helper signature matches",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Command: "sleep infinity",
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: true,
		},
		{
			name: "internal trivy-like helper is not treated as an unlabeled volume helper",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Command: "trivy image --quiet alpine:latest",
				Mounts: []container.MountPoint{
					{Destination: "/var/run/docker.sock"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isUnlabeledVolumeHelperContainer(tt.summary))
		})
	}
}

func TestIsVolumeHelperContainer_UsesExplicitHelperLabel(t *testing.T) {
	tests := []struct {
		name    string
		summary container.Summary
		want    bool
	}{
		{
			name: "new helper label matches",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
					volumehelper.ContainerLabel:     "true",
				},
			},
			want: true,
		},
		{
			name: "generic internal volume mount does not match",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: false,
		},
		{
			name: "unlabeled helper still matches",
			summary: container.Summary{
				Labels: map[string]string{
					libarcane.InternalResourceLabel: "true",
				},
				Command: "sleep infinity",
				Mounts: []container.MountPoint{
					{Destination: "/volume"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isVolumeHelperContainer(tt.summary))
		})
	}
}

func TestCollectStaleHelperIDs(t *testing.T) {
	now := time.Now()
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"fresh":  {id: "c-fresh", lastUsedAt: now.Add(-1 * time.Minute)},
			"stale":  {id: "c-stale", lastUsedAt: now.Add(-11 * time.Minute)},
			"atedge": {id: "c-atedge", lastUsedAt: now.Add(-10 * time.Minute)},
			"nilent": nil,
		},
	}

	stale := s.collectStaleHelperIDs(now, 10*time.Minute)

	require.ElementsMatch(t, []string{"c-stale", "c-atedge"}, stale,
		"helpers idle >= timeout (and exactly at the edge) should be collected")

	// Only the fresh entry survives; stale, at-edge, and nil entries are dropped.
	require.Len(t, s.helperByVolume, 1)
	require.Contains(t, s.helperByVolume, "fresh")
}

// A helper serving a request longer than the idle timeout (e.g. a slow download)
// must not be reaped mid-stream; it becomes collectible once released, with the
// idle clock measured from the release.
func TestCollectStaleHelperIDsSkipsInUseHelpers(t *testing.T) {
	now := time.Now()
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: now.Add(-30 * time.Minute)},
		},
	}

	release, ok := s.acquireHelper("vol-a", "c-a")
	require.True(t, ok)

	require.Empty(t, s.collectStaleHelperIDs(now, 10*time.Minute),
		"an in-use helper must survive the reaper regardless of lastUsedAt")
	require.Contains(t, s.helperByVolume, "vol-a")

	release()
	require.Empty(t, s.collectStaleHelperIDs(now, 10*time.Minute),
		"release refreshes the idle clock, so the helper is fresh again")
	stale := s.collectStaleHelperIDs(now.Add(11*time.Minute), 10*time.Minute)
	require.Equal(t, []string{"c-a"}, stale)

	// Acquiring a helper that was reaped (or replaced) since resolve must fail
	// so the caller re-resolves instead of using a dead container.
	_, ok = s.acquireHelper("vol-a", "c-a")
	require.False(t, ok)
}

func TestTakeHelperID(t *testing.T) {
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: time.Now()},
		},
	}

	// Present: returns id and removes the entry.
	require.Equal(t, "c-a", s.takeHelperID("vol-a"))
	require.NotContains(t, s.helperByVolume, "vol-a")

	// Absent (idempotent): returns "" without panicking.
	require.Empty(t, s.takeHelperID("vol-a"))
	require.Empty(t, s.takeHelperID("never-existed"))
}

func TestTouchHelper(t *testing.T) {
	old := time.Now().Add(-30 * time.Minute)
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: old},
		},
	}

	s.touchHelper("vol-a")
	require.True(t, s.helperByVolume["vol-a"].lastUsedAt.After(old),
		"touch should reset the idle clock forward")

	// Missing volume is a no-op (must not panic or create an entry).
	s.touchHelper("missing")
	require.NotContains(t, s.helperByVolume, "missing")
}
