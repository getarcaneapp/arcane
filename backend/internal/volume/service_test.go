package volume

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	containerdomain "github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumehelper"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func newVolumeBackupEngineForTestInternal(t testing.TB) *backup.Engine {
	t.Helper()
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "volume-backup-test-admission", t.Name())
	require.NoError(t, err)
	engine := backup.NewEngine(t.Context(), runtime, gate, nil)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, engine.Stop(stopCtx))
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	return engine
}

func newVolumeServiceTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
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

	dockerService := docker.NewDockerClientService(context.Background(), nil, nil, nil).WithClient(newVolumeServiceTestDockerClientInternal(t, server))
	service := &VolumeService{dockerService: dockerService}

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
		dockerService:  docker.NewDockerClientService(context.Background(), nil, nil, nil).WithClient(newVolumeServiceTestDockerClientInternal(t, server)),
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
			got := enrichVolumesWithUsageDataInternal(tt.volumes, tt.usageVolumes)
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

func TestRenameVolumeRejectsInvalidAndProtectedNames(t *testing.T) {
	service := &VolumeService{backupVolumeName: "arcane-backups"}

	_, err := service.RenameVolume(t.Context(), "data", "data", common.User{})
	require.ErrorIs(t, err, ErrVolumeRenameInvalid)

	_, err = service.RenameVolume(t.Context(), "arcane-backups", "renamed-backups", common.User{})
	require.ErrorIs(t, err, ErrVolumeRenameProtected)

	_, err = service.RenameVolume(t.Context(), "data", "arcane-backups", common.User{})
	require.ErrorIs(t, err, ErrVolumeRenameProtected)
}

func TestRenameVolumeMetadataInternalPreservesPoliciesAndHistory(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-rename-metadata?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &VolumeBackup{}))

	policy := VolumeBackupPolicy{VolumeName: "source-data", Schedule: defaultVolumeBackupSchedule, LocalEnabled: true}
	require.NoError(t, gormDB.Create(&policy).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "source-data", PolicyID: policy.ID}).Error)

	service := &VolumeService{db: &database.DB{DB: gormDB}}
	policies, err := service.renameVolumeMetadataInternal(t.Context(), "source-data", "renamed-data")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "renamed-data", policies[0].VolumeName)

	var renamedPolicy VolumeBackupPolicy
	require.NoError(t, gormDB.Where("id = ?", policy.ID).First(&renamedPolicy).Error)
	require.Equal(t, "renamed-data", renamedPolicy.VolumeName)

	var backupEntry VolumeBackup
	require.NoError(t, gormDB.Where("policy_id = ?", policy.ID).First(&backupEntry).Error)
	require.Equal(t, "renamed-data", backupEntry.VolumeName)
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

type volumeBackupPolicySchedulerInternal struct {
	jobs map[string]schedulertypes.Job
}

func (s *volumeBackupPolicySchedulerInternal) AddJob(_ context.Context, job schedulertypes.Job) error {
	s.jobs[job.Name()] = job
	return nil
}

func (s *volumeBackupPolicySchedulerInternal) RemoveJob(_ context.Context, name string) {
	delete(s.jobs, name)
}

func (s *volumeBackupPolicySchedulerInternal) HasJob(name string) bool {
	_, ok := s.jobs[name]
	return ok
}

func TestVolumeBackupPolicy_UpdateRegistersIndependentJobsAndSettings(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-schedule?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &VolumeBackup{}))
	db := &database.DB{DB: gormDB}
	scheduler := &volumeBackupPolicySchedulerInternal{jobs: make(map[string]schedulertypes.Job)}
	service := &VolumeService{db: db, jobs: entityjobs.New("volume-backup:", backup.VolumeAdmissionScope)}
	fxLifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), fxLifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "volume-policy-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, fxLifecycle.Stop(stopCtx))
	})
	require.NoError(t, service.SetScheduler(context.Background(), scheduler, gate))

	collection, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{
		{Enabled: true, Schedule: "0 */15 * * * *", RetentionCount: 5, StopContainers: true, LocalEnabled: true},
		{Enabled: true, Schedule: "0 0 2 * * *", RetentionCount: 30, LocalEnabled: true},
	})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, 5, collection.Policies[0].RetentionCount)
	require.True(t, collection.Policies[0].StopContainers)
	require.Equal(t, 30, collection.Policies[1].RetentionCount)
	require.Len(t, scheduler.jobs, 2)

	firstID := collection.Policies[0].ID
	collection, err = service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{
		ID: firstID, Schedule: "0 */30 * * * *", RetentionCount: 9, LocalEnabled: true,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	require.Equal(t, firstID, collection.Policies[0].ID)
	require.Equal(t, 9, collection.Policies[0].RetentionCount)
	require.Empty(t, scheduler.jobs)
}

func TestVolumeBackupPolicy_GetReturnsLastRunForEachPolicy(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-last-runs?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &VolumeBackup{}))
	first := &VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 2 * * *", LocalEnabled: true}
	second := &VolumeBackupPolicy{VolumeName: "app-data", Schedule: "0 0 14 * * *", LocalEnabled: true}
	require.NoError(t, gormDB.Create(first).Error)
	require.NoError(t, gormDB.Create(second).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: first.ID, Status: VolumeBackupStatusSucceeded}).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{VolumeName: "app-data", PolicyID: second.ID, Status: VolumeBackupStatusFailed}).Error)

	service := &VolumeService{db: &database.DB{DB: gormDB}}
	collection, err := service.GetBackupPolicies(context.Background(), "app-data")
	require.NoError(t, err)
	require.Len(t, collection.Policies, 2)
	require.Equal(t, "succeeded", collection.Policies[0].LastRun.Status)
	require.Equal(t, "failed", collection.Policies[1].LastRun.Status)
}

func TestVolumeBackupPolicy_RetentionIgnoresFailedRuns(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-retention-failed?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}))

	policyID := "policy-1"
	require.NoError(t, gormDB.Create(&VolumeBackup{
		VolumeName: "app-data", PolicyID: policyID, Status: VolumeBackupStatusSucceeded,
		LocalSnapshotID: "snapshot-1", CreatedAt: time.Now().Add(-time.Hour),
	}).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{
		VolumeName: "app-data", PolicyID: policyID, Status: VolumeBackupStatusFailed,
		CreatedAt: time.Now(),
	}).Error)

	service := &VolumeService{db: &database.DB{DB: gormDB}}
	require.NoError(t, service.applyVolumeBackupRetentionInternal(context.Background(), policyID, 1))

	var backups []VolumeBackup
	require.NoError(t, gormDB.Order("created_at ASC").Find(&backups).Error)
	require.Len(t, backups, 2)
	require.Equal(t, "snapshot-1", backups[0].LocalSnapshotID)
	require.Equal(t, VolumeBackupStatusFailed, backups[1].Status)
}

func TestVolumeBackup_ValidationErrors(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-validation?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}))
	service := &VolumeService{db: &database.DB{DB: gormDB}}
	ctx := context.Background()

	_, err = service.UpdateBackupPolicies(ctx, "app-data", []volumetypes.UpdateBackupPolicy{{Schedule: "not a cron", LocalEnabled: true}})
	require.ErrorContains(t, err, "invalid volume backup schedule")

	_, err = service.UpdateBackupPolicies(ctx, "app-data", []volumetypes.UpdateBackupPolicy{{Schedule: "0 0 2 * * *", RetentionCount: 7}})
	require.ErrorContains(t, err, "select at least one volume backup destination")

	_, err = service.CreateBackup(ctx, "app-data", common.User{}, VolumeBackupTriggerManual, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestination("invalid")})
	require.EqualError(t, err, "invalid volume backup destination")

	_, err = service.CreateBackup(ctx, "app-data", common.User{}, VolumeBackupTriggerManual, volumetypes.CreateBackupRequest{Destination: volumetypes.BackupDestinationS3})
	require.EqualError(t, err, "select an S3 destination for the volume backup")
}

func TestVolumeBackupPolicy_ScheduledRunCreatesActivity(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-scheduled-activity?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &VolumeBackup{}, &activity.Activity{}))
	db := &database.DB{DB: gormDB}
	policy := &VolumeBackupPolicy{
		VolumeName:     "app-data",
		Enabled:        true,
		Schedule:       "0 0 2 * * *",
		RetentionCount: 7,
		LocalEnabled:   true,
	}
	require.NoError(t, gormDB.Create(policy).Error)
	engine := newVolumeBackupEngineForTestInternal(t)
	service := &VolumeService{
		db:              db,
		activityService: activity.NewActivityService(db, nil),
		engine:          engine,
		jobs:            entityjobs.New("volume-backup:", backup.VolumeAdmissionScope),
	}
	// Holding the volume's admission lease makes the scheduled run fail with
	// "already running", which still must record a failed activity.
	lease, admitted, err := engine.TryAcquireRun(context.Background(), backup.VolumeAdmissionScope, policy.VolumeName)
	require.NoError(t, err)
	require.True(t, admitted)
	defer lease.Release()

	service.runScheduledBackupInternal(context.Background(), policy.ID)

	var activity activity.Activity
	require.NoError(t, gormDB.Where("resource_type = ?", "volume_backup").First(&activity).Error)
	require.Equal(t, activitytypes.StatusFailed, activity.Status)
	require.Equal(t, "scheduled_volume_backup", activity.Metadata["action"])
	require.Equal(t, policy.Schedule, activity.Metadata["schedule"])
}

func TestVolumeBackupPolicy_UpdateUsesSelectedS3Destination(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-s3-secret?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackupPolicy{}, &VolumeBackup{}, &s3domain.S3Destination{}))
	db := &database.DB{DB: gormDB}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: "test-encryption-key-for-volume-backups-32bytes",
		Environment:   "test",
	})
	encryptedSecret, err := crypto.Encrypt("destination-s3-secret")
	require.NoError(t, err)
	destination := &s3domain.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "destination-access-key",
		SecretAccessKey: encryptedSecret,
		UseSSL:          true,
	}
	require.NoError(t, gormDB.Create(destination).Error)
	service := &VolumeService{
		db:             db,
		s3Destinations: s3domain.NewS3DestinationService(db),
		jobs:           entityjobs.New("volume-backup:", backup.VolumeAdmissionScope),
	}
	collection, err := service.UpdateBackupPolicies(context.Background(), "app-data", []volumetypes.UpdateBackupPolicy{{
		Schedule:        "0 0 2 * * *",
		RetentionCount:  7,
		S3Enabled:       true,
		S3DestinationID: destination.ID,
	}})
	require.NoError(t, err)
	require.Len(t, collection.Policies, 1)
	policy := collection.Policies[0]
	require.False(t, policy.LocalEnabled)
	require.True(t, policy.S3Enabled)
	require.True(t, policy.S3Available)
	require.Equal(t, destination.ID, policy.S3DestinationID)
	require.Equal(t, "Offsite", policy.S3DestinationName)
	require.Equal(t, "volume-backups", policy.S3Bucket)

	var stored VolumeBackupPolicy
	require.NoError(t, gormDB.Where("volume_name = ?", "app-data").First(&stored).Error)
	require.False(t, stored.LocalEnabled)
	require.True(t, stored.S3Enabled)
	require.Equal(t, destination.ID, stored.S3DestinationID)
}

func TestVolumeBackup_ListResolvesDestinationName(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open("file:volume-backup-destination-name?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&VolumeBackup{}, &s3domain.S3Destination{}))
	db := &database.DB{DB: gormDB}
	destination := &s3domain.S3Destination{
		Name:            "Offsite",
		Bucket:          "volume-backups",
		Region:          "us-east-1",
		AccessKeyID:     "access-key",
		SecretAccessKey: "encrypted-secret",
	}
	require.NoError(t, gormDB.Create(destination).Error)
	require.NoError(t, gormDB.Create(&VolumeBackup{
		VolumeName:      "app-data",
		Status:          VolumeBackupStatusSucceeded,
		Trigger:         VolumeBackupTriggerManual,
		Destination:     volumetypes.BackupDestinationLocalS3,
		S3DestinationID: destination.ID,
	}).Error)

	service := &VolumeService{db: db, s3Destinations: s3domain.NewS3DestinationService(db)}
	backups, _, err := service.ListBackupsPaginated(context.Background(), "app-data", pagination.QueryParams{})
	require.NoError(t, err)
	require.Len(t, backups, 1)
	require.Equal(t, volumetypes.BackupDestinationLocalS3, backups[0].Destination)
	require.Equal(t, "Offsite", backups[0].S3DestinationName)
}

func setupVolumeBackupLifecycleTestInternal(t *testing.T, handler http.Handler) (*VolumeService, *client.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}))
	db := &database.DB{DB: gormDB}
	dockerService := docker.NewDockerClientService(t.Context(), db, &config.Config{}, nil).WithClient(dockerClient)
	eventService := event.NewEventService(db, &config.Config{}, nil)
	containerService := containerdomain.NewContainerService(t.Context(), eventService, dockerService, nil, nil, nil)
	return &VolumeService{containerService: containerService}, dockerClient
}

func TestVolumeBackupContainerLifecycleStopsAndRestartsOnlyRunningContainersUsingVolume(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: "uses-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "other-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "other-data"}}},
				{ID: "arcane", Labels: map[string]string{"com.getarcaneapp.arcane": "true"}, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/stop"):
			mu.Lock()
			operations = append(operations, "stop:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/start"):
			mu.Lock()
			operations = append(operations, "start:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTestInternal(t, serverHandler)
	actor := common.User{BaseModel: database.BaseModel{ID: "user-1"}, Username: "tester"}
	stopped, err := service.stopRunningContainersForBackupInternal(context.Background(), dockerClient, "app-data", actor, false)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "uses-volume", stopped[0].ID)

	remaining, err := service.startContainersAfterBackupInternal(context.Background(), dockerClient, stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:uses-volume", "start:uses-volume"}, operations)
}

func TestVolumeBackupContainerLifecycleRollsBackStoppedContainersOnStopFailure(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: "first", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "second", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/first/stop"):
			mu.Lock()
			operations = append(operations, "stop:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/second/stop"):
			mu.Lock()
			operations = append(operations, "stop:second")
			mu.Unlock()
			http.Error(w, "stop failed", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/first/start"):
			mu.Lock()
			operations = append(operations, "start:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTestInternal(t, serverHandler)
	actor := common.User{BaseModel: database.BaseModel{ID: "user-1"}, Username: "tester"}
	stillStopped, err := service.stopRunningContainersForBackupInternal(context.Background(), dockerClient, "app-data", actor, false)
	require.ErrorContains(t, err, "failed to stop container second")
	require.Empty(t, stillStopped)
	require.Equal(t, []string{"stop:first", "stop:second", "start:first"}, operations)
}

func TestVolumeBackupContainerLifecycleWaitsForRunningComposeReplacement(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	listCalls := 0
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			mu.Lock()
			listCalls++
			call := listCalls
			mu.Unlock()
			if call == 1 {
				require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
					{
						ID:     "old-id",
						Names:  []string{"/old-name"},
						State:  container.StateRunning,
						Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
						Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
					},
				}))
				return
			}
			if call == 2 {
				require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:     "new-id",
					Names:  []string{"/new-name"},
					State:  container.StateRunning,
					Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
					Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
				},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/old-id/stop"):
			mu.Lock()
			operations = append(operations, "stop:old-id")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/start"):
			mu.Lock()
			operations = append(operations, "unexpected:"+r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTestInternal(t, serverHandler)
	actor := common.User{BaseModel: database.BaseModel{ID: "user-1"}, Username: "tester"}
	stopped, err := service.stopRunningContainersForBackupInternal(context.Background(), dockerClient, "app-data", actor, false)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "old-id", stopped[0].ID)

	remaining, err := service.startContainersAfterBackupInternal(context.Background(), dockerClient, stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:old-id"}, operations)
}
