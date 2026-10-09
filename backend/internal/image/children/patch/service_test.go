package patch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/features"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

func TestResolvePatchedRef(t *testing.T) {
	tests := []struct {
		name       string
		imageRef   string
		patchedTag string
		suffix     string
		want       string
	}{
		{
			name:     "default suffix",
			imageRef: "nginx:1.25",
			suffix:   "patched",
			want:     "docker.io/library/nginx:1.25-patched",
		},
		{
			name:       "explicit tag override with registry port",
			imageRef:   "registry.local:5000/app:2.0",
			patchedTag: "2.0-hardened",
			suffix:     "patched",
			want:       "registry.local:5000/app:2.0-hardened",
		},
		{
			name:     "custom suffix",
			imageRef: "ghcr.io/acme/api:v3",
			suffix:   "fixed",
			want:     "ghcr.io/acme/api:v3-fixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePatchedRef(tt.imageRef, tt.patchedTag, tt.suffix)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestListPatchTargets_ExcludesUntaggedImages(t *testing.T) {
	ctx := t.Context()
	dsn := fmt.Sprintf("file:image-patch-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&vulnerability.VulnerabilityScanRecord{}, &vulnerability.VulnerabilityReportRecord{}, &ImagePatchRecord{}))
	db := &database.DB{DB: gdb}

	svc := &Service{
		db:            db,
		dockerService: docker.NewDockerClientService(ctx, nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-test.sock"}, nil),
	}

	fixable := 3
	records := []vulnerability.VulnerabilityScanRecord{
		{ID: "sha256:aaa", ImageName: "nginx:latest", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
		{ID: "sha256:bbb", ImageName: "sha256:bbb", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
		{ID: "sha256:ccc", ImageName: "<none>:<none>", Status: vulnerability.ScanStatusCompleted, ScanTime: time.Now(), FixableCount: &fixable},
	}
	for i := range records {
		require.NoError(t, db.Create(&records[i]).Error)
		require.NoError(t, db.Create(&vulnerability.VulnerabilityReportRecord{ImageID: records[i].ID, Data: "{}"}).Error)
	}

	targets, _, err := svc.ListPatchTargets(ctx, "0", pagination.QueryParams{Limit: 20})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "nginx:latest", targets[0].ImageRef)
}

func patchFeatureSettings(t *testing.T) (*settings.SettingsService, *database.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&settings.SettingVariable{}, &vulnerability.VulnerabilityScanRecord{}, &vulnerability.VulnerabilityReportRecord{}, &ImagePatchRecord{}))
	db := &database.DB{DB: gdb}
	svc, err := settings.NewSettingsService(t.Context(), db)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	require.NoError(t, err)
	return svc, db
}

func TestVulnerabilityFeatureBlocksReportPatchesOnly(t *testing.T) {
	settingsSvc, db := patchFeatureSettings(t)
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), features.VulnerabilityManagementSettingKey, false))
	svc := &Service{
		settingsService: settingsSvc,
		db:              db,
		dockerService:   docker.NewDockerClientService(t.Context(), nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-feature-test.sock"}, nil),
	}
	_, err := svc.PatchImage(t.Context(), "0", "test-image", imagepatch.PatchOptions{ScanID: "test-image"}, user.Actor{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, _, err = svc.ListPatchTargets(t.Context(), "0", pagination.QueryParams{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, err = svc.Targets(t.Context(), "0")
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	// Standalone patching still reaches Docker; this test deliberately has no daemon.
	_, err = svc.PatchImage(t.Context(), "0", "test-image", imagepatch.PatchOptions{}, user.Actor{})
	require.Error(t, err)
	require.NotErrorIs(t, err, common.ErrFeatureDisabled)
	require.Contains(t, err.Error(), "failed to connect to Docker")
}

func TestQueuedReportPatchStopsWhenFeatureDisabled(t *testing.T) {
	settingsSvc, db := patchFeatureSettings(t)
	svc := &Service{settingsService: settingsSvc, db: db, patchSlot: make(chan struct{}, 1)}
	record := &ImagePatchRecord{EnvironmentID: "0", OriginalImageID: "test-image", Mode: string(imagepatch.PatchModeReport), Status: string(imagepatch.PatchStatusPatching)}
	require.NoError(t, db.Create(record).Error)
	svc.patchSlot <- struct{}{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.runPatch(t.Context(), record, imagepatch.PatchOptions{ScanID: "test-image"}, nil, "", "")
	}()
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), features.VulnerabilityManagementSettingKey, false))
	// The active patch owns its slot until it finishes normally.
	require.Len(t, svc.patchSlot, 1)
	<-svc.patchSlot
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queued patch did not stop")
	}
	var stored ImagePatchRecord
	require.NoError(t, db.First(&stored, "id = ?", record.ID).Error)
	require.Equal(t, string(imagepatch.PatchStatusFailed), stored.Status)
	require.NotNil(t, stored.Error)
	require.Contains(t, *stored.Error, string(features.VulnerabilityManagement))
	require.Empty(t, svc.patchSlot)
}

func TestDisabledFeatureSkipsPatchVerificationScan(t *testing.T) {
	settingsSvc, _ := patchFeatureSettings(t)
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), features.VulnerabilityManagementSettingKey, false))
	var inspected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inspected.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"patched-image"}`))
	}))
	defer server.Close()
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	defer func() { assert.NoError(t, dockerClient.Close()) }()
	svc := &Service{
		settingsService: settingsSvc,
		dockerService:   &docker.DockerClientService{Client: dockerClient},
		// Any attempt to scan would access unconfigured dependencies and fail.
		vulnerabilityService: &vulnerability.VulnerabilityService{},
	}
	require.NotPanics(t, func() {
		svc.verifyPatchedImage(t.Context(), &ImagePatchRecord{EnvironmentID: "0", PatchedRef: "test:patched"}, "")
	})
	require.Equal(t, int32(1), inspected.Load())
}
