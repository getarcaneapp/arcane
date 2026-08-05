package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestValidateVolumeHelperSupportInternalOnlyInspectsVolume(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/volumes/workspace-volume") {
			http.Error(w, "unexpected Docker request", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(volume.Volume{Name: "workspace-volume", Driver: "local"}); err != nil {
			t.Errorf("encode volume inspect response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	service := &VolumeService{dockerService: &DockerClientService{client: newTestDockerClient(t, server)}}

	require.NoError(t, service.validateVolumeHelperSupportInternal(context.Background(), "workspace-volume"))
	require.Equal(t, []string{"GET /v1.41/volumes/workspace-volume"}, requests)
}

func TestCreateTempContainerInternalReusesWritableHelper(t *testing.T) {
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
		dockerService:  &DockerClientService{client: newTestDockerClient(t, server)},
		helperByVolume: make(map[string]*volumeHelper),
	}

	firstID, releaseFirst, err := service.acquireVolumeHelperInternal(context.Background(), "workspace-volume")
	require.NoError(t, err)
	secondID, releaseSecond, err := service.acquireVolumeHelperInternal(context.Background(), "workspace-volume")
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
	require.Empty(t, service.collectStaleHelperIDsInternal(time.Now(), time.Minute))
	require.Contains(t, service.helperByVolume, "workspace-volume")

	releaseFirst()
	releaseSecond()
	require.Zero(t, service.helperByVolume["workspace-volume"].inUse)
}

func TestIsUnlabeledVolumeHelperContainerInternal(t *testing.T) {
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
			require.Equal(t, tt.want, isUnlabeledVolumeHelperContainerInternal(tt.summary))
		})
	}
}

func TestIsVolumeHelperContainerInternal_UsesExplicitHelperLabel(t *testing.T) {
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
			require.Equal(t, tt.want, isVolumeHelperContainerInternal(tt.summary))
		})
	}
}

func TestEnrichVolumesWithUsageDataInternal(t *testing.T) {
	svc := &VolumeService{}

	tests := []struct {
		name         string
		volumes      []volume.Volume
		usageVolumes []volume.Volume
		wantLen      int
		assertions   func(t *testing.T, got []volume.Volume)
	}{
		{
			name: "attaches usage by name",
			volumes: []volume.Volume{
				{Name: "vol-a"},
				{Name: "vol-b"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-a", UsageData: &volume.UsageData{Size: 100, RefCount: 2}},
				{Name: "vol-c", UsageData: &volume.UsageData{Size: 50, RefCount: 1}},
			},
			wantLen: 2,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 100, got[0].UsageData.Size)
				require.EqualValues(t, 2, got[0].UsageData.RefCount)
				require.Nil(t, got[1].UsageData)
			},
		},
		{
			name: "keeps first usage entry when duplicate usage names exist",
			volumes: []volume.Volume{
				{Name: "vol-dup"},
			},
			usageVolumes: []volume.Volume{
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 10, RefCount: 1}},
				{Name: "vol-dup", UsageData: &volume.UsageData{Size: 20, RefCount: 3}},
			},
			wantLen: 1,
			assertions: func(t *testing.T, got []volume.Volume) {
				require.NotNil(t, got[0].UsageData)
				require.EqualValues(t, 10, got[0].UsageData.Size)
				require.EqualValues(t, 1, got[0].UsageData.RefCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.enrichVolumesWithUsageDataInternal(tt.volumes, tt.usageVolumes)
			require.Len(t, got, tt.wantLen)
			tt.assertions(t, got)
		})
	}
}

func TestIsInternalVolumeInternal(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	require.True(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "arcane-backups"}))
	require.False(t, svc.isInternalVolumeInternal(volumetypes.Volume{Name: "user-volume"}))
}

func TestBuildVolumePruneOptionsInternal_PreservesTrivyCache(t *testing.T) {
	options := buildVolumePruneOptionsInternal(true, true)

	require.True(t, options.All)
	require.NotNil(t, options.Filters)
	require.True(t, options.Filters["label!"][trivyCacheVolumePruneFilterValue])
}

func TestBuildVolumePruneOptionsInternal_PreservesTrivyCacheForAnonymousVolumes(t *testing.T) {
	options := buildVolumePruneOptionsInternal(false, true)

	require.False(t, options.All)
	require.NotNil(t, options.Filters)
	require.True(t, options.Filters["label!"][trivyCacheVolumePruneFilterValue])
}

func TestBuildVolumePruneOptionsInternal_DisabledPreservationOmitsFilter(t *testing.T) {
	options := buildVolumePruneOptionsInternal(true, false)

	require.True(t, options.All)
	require.Nil(t, options.Filters)
}

func TestBuildVolumePruneMetadataInternal(t *testing.T) {
	metadata := buildVolumePruneMetadataInternal(true, 2, 4096, true)

	require.Equal(t, "prune", metadata["action"])
	require.Equal(t, true, metadata["all"])
	require.Equal(t, 2, metadata["volumesDeleted"])
	require.EqualValues(t, 4096, metadata["spaceReclaimed"])
	require.Equal(t, true, metadata["preserveTrivyCache"])
	require.Equal(t, trivyCacheVolumePruneFilterValue, metadata["trivyCacheFilterLabel"])
}

func TestResolveBackupStorageMountFromMountsInternal(t *testing.T) {
	tests := []struct {
		name         string
		mounts       []container.MountPoint
		target       string
		readOnly     bool
		wantResolved bool
		wantMode     backupStorageMode
		wantType     mount.Type
		wantSource   string
		wantTarget   string
		wantReadOnly bool
		wantEnsure   bool
	}{
		{
			name: "mirrors bind mount",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			target:       "/volume",
			readOnly:     true,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: true,
		},
		{
			name: "writable request against read-only bind mount still resolves",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups", RW: false},
			},
			target:       "/volume",
			readOnly:     false,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: false,
		},
		{
			name: "mirrors named volume",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     false,
			wantResolved: true,
			wantMode:     backupStorageModeArcaneMount,
			wantType:     mount.TypeVolume,
			wantSource:   "arcane-backups",
			wantTarget:   "/backups",
			wantReadOnly: false,
		},
		{
			name: "ignores unsupported mount types",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
		{
			name:         "returns unresolved when mount is absent",
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := resolveBackupStorageMountFromMountsInternal(tt.mounts, tt.target, tt.readOnly).Get()
			require.Equal(t, tt.wantResolved, ok)
			if !tt.wantResolved {
				return
			}

			require.Equal(t, tt.wantMode, got.mode)
			require.Equal(t, tt.wantType, got.mount.Type)
			require.Equal(t, tt.wantSource, got.mount.Source)
			require.Equal(t, tt.wantTarget, got.mount.Target)
			require.Equal(t, tt.wantReadOnly, got.mount.ReadOnly)
			require.Equal(t, tt.wantEnsure, got.requiresEnsure)
		})
	}
}

func TestResolveBackupStorageMountInternalFallsBackToNamedVolume(t *testing.T) {
	svc := &VolumeService{backupVolumeName: "arcane-backups"}

	got := svc.resolveBackupStorageMountInternal(context.Background(), nil, "/backups", true)
	require.Equal(t, backupStorageModeNamedVolumeFallback, got.mode)
	require.Equal(t, mount.TypeVolume, got.mount.Type)
	require.Equal(t, "arcane-backups", got.mount.Source)
	require.Equal(t, "/backups", got.mount.Target)
	require.True(t, got.mount.ReadOnly)
	require.True(t, got.requiresEnsure)
}

func TestBackupMountWarningForStorageInternal(t *testing.T) {
	require.Empty(t, backupMountWarningForStorageInternal(backupStorageMountInternal{mode: backupStorageModeArcaneMount}))
	require.Equal(t, backupMountMissingWarning, backupMountWarningForStorageInternal(backupStorageMountInternal{mode: backupStorageModeNamedVolumeFallback}))
}

func TestBackupMountWarningFromArcaneMountsInternal(t *testing.T) {
	tests := []struct {
		name   string
		mounts []container.MountPoint
		want   string
	}{
		{
			name: "bind mount at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "named volume at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "bind mount at restores suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/restores", Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "unsupported restores mount still suppresses warning for compatibility",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "missing backups mount warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/other", Destination: "/other"},
			},
			want: backupMountMissingWarning,
		},
		{
			name: "unsupported backups mount type warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			want: backupMountMissingWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, backupMountWarningFromArcaneMountsInternal(tt.mounts))
		})
	}
}

func TestBackupArchiveFilenameInternal(t *testing.T) {
	svc := &VolumeService{}

	tests := []struct {
		name     string
		backupID string
		want     string
		wantErr  bool
	}{
		{
			name:     "valid backup id",
			backupID: "volume-123-abc",
			want:     "volume-123-abc.tar.gz",
		},
		{
			name:     "trims surrounding whitespace",
			backupID: "  volume-123-abc  ",
			want:     "volume-123-abc.tar.gz",
		},
		{
			name:     "rejects traversal attempts",
			backupID: "../../bin/busybox",
			wantErr:  true,
		},
		{
			name:     "rejects path separators",
			backupID: "nested/path",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.backupArchiveFilenameInternal(tt.backupID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCollectStaleHelperIDsInternal(t *testing.T) {
	now := time.Now()
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"fresh":  {id: "c-fresh", lastUsedAt: now.Add(-1 * time.Minute)},
			"stale":  {id: "c-stale", lastUsedAt: now.Add(-11 * time.Minute)},
			"atedge": {id: "c-atedge", lastUsedAt: now.Add(-10 * time.Minute)},
			"nilent": nil,
		},
	}

	stale := s.collectStaleHelperIDsInternal(now, 10*time.Minute)

	require.ElementsMatch(t, []string{"c-stale", "c-atedge"}, stale,
		"helpers idle >= timeout (and exactly at the edge) should be collected")

	// Only the fresh entry survives; stale, at-edge, and nil entries are dropped.
	require.Len(t, s.helperByVolume, 1)
	require.Contains(t, s.helperByVolume, "fresh")
}

// A helper serving a request longer than the idle timeout (e.g. a slow download)
// must not be reaped mid-stream; it becomes collectible once released, with the
// idle clock measured from the release.
func TestCollectStaleHelperIDsInternalSkipsInUseHelpers(t *testing.T) {
	now := time.Now()
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: now.Add(-30 * time.Minute)},
		},
	}

	release, ok := s.acquireHelperInternal("vol-a", "c-a")
	require.True(t, ok)

	require.Empty(t, s.collectStaleHelperIDsInternal(now, 10*time.Minute),
		"an in-use helper must survive the reaper regardless of lastUsedAt")
	require.Contains(t, s.helperByVolume, "vol-a")

	release()
	require.Empty(t, s.collectStaleHelperIDsInternal(now, 10*time.Minute),
		"release refreshes the idle clock, so the helper is fresh again")
	stale := s.collectStaleHelperIDsInternal(now.Add(11*time.Minute), 10*time.Minute)
	require.Equal(t, []string{"c-a"}, stale)

	// Acquiring a helper that was reaped (or replaced) since resolve must fail
	// so the caller re-resolves instead of using a dead container.
	_, ok = s.acquireHelperInternal("vol-a", "c-a")
	require.False(t, ok)
}

func TestTakeHelperIDInternal(t *testing.T) {
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: time.Now()},
		},
	}

	// Present: returns id and removes the entry.
	require.Equal(t, "c-a", s.takeHelperIDInternal("vol-a"))
	require.NotContains(t, s.helperByVolume, "vol-a")

	// Absent (idempotent): returns "" without panicking.
	require.Empty(t, s.takeHelperIDInternal("vol-a"))
	require.Empty(t, s.takeHelperIDInternal("never-existed"))
}

func TestTouchHelperInternal(t *testing.T) {
	old := time.Now().Add(-30 * time.Minute)
	s := &VolumeService{
		helperByVolume: map[string]*volumeHelper{
			"vol-a": {id: "c-a", lastUsedAt: old},
		},
	}

	s.touchHelperInternal("vol-a")
	require.True(t, s.helperByVolume["vol-a"].lastUsedAt.After(old),
		"touch should reset the idle clock forward")

	// Missing volume is a no-op (must not panic or create an entry).
	s.touchHelperInternal("missing")
	require.NotContains(t, s.helperByVolume, "missing")
}
