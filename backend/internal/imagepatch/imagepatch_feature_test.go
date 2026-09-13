package imagepatch

import (
	"context"
	"github.com/moby/moby/client"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/features"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func patchFeatureSettingsInternal(t *testing.T) (*settings.SettingsService, *database.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&settings.SettingVariable{}, &vulnerability.VulnerabilityScanRecord{}, &vulnerability.VulnerabilityReportRecord{}, &ImagePatchRecord{}))
	db := &database.DB{DB: gdb}
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	writes, err := actors.NewExecutor(t.Context(), runtime, "patch-feature-writes", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "patch-feature-effects", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, writes.Stop(ctx))
		require.NoError(t, effects.Stop(ctx))
		require.NoError(t, lifecycle.Stop(ctx))
	})
	svc, err := settings.NewSettingsService(t.Context(), db, writes, effects)
	require.NoError(t, err)
	return svc, db
}

func TestVulnerabilityFeatureBlocksReportPatchesOnly(t *testing.T) {
	settingsSvc, db := patchFeatureSettingsInternal(t)
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), features.VulnerabilityManagementSettingKey, false))
	svc := &ImagePatchService{
		settingsService: settingsSvc,
		db:              db,
		dockerService:   docker.NewDockerClientService(t.Context(), nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-feature-test.sock"}, nil),
	}
	_, err := svc.PatchImage(t.Context(), "0", "test-image", imagepatch.PatchOptions{ScanID: "test-image"}, common.User{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, _, err = svc.ListPatchTargets(t.Context(), "0", pagination.QueryParams{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, _, err = svc.PatchFlaggedImages(t.Context(), "0", common.User{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	// Standalone patching still reaches Docker; this test deliberately has no daemon.
	_, err = svc.PatchImage(t.Context(), "0", "test-image", imagepatch.PatchOptions{}, common.User{})
	require.Error(t, err)
	require.NotErrorIs(t, err, common.ErrFeatureDisabled)
	require.Contains(t, err.Error(), "failed to connect to Docker")
}

func TestQueuedReportPatchStopsWhenFeatureDisabled(t *testing.T) {
	settingsSvc, db := patchFeatureSettingsInternal(t)
	svc := &ImagePatchService{settingsService: settingsSvc, db: db, patchSlot: make(chan struct{}, 1)}
	record := &ImagePatchRecord{EnvironmentID: "0", OriginalImageID: "test-image", Mode: string(imagepatch.PatchModeReport), Status: string(imagepatch.PatchStatusPatching)}
	require.NoError(t, db.Create(record).Error)
	svc.patchSlot <- struct{}{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.patchInBackgroundInternal(t.Context(), record, imagepatch.PatchOptions{ScanID: "test-image"}, nil, "", "")
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
	settingsSvc, _ := patchFeatureSettingsInternal(t)
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
	defer dockerClient.Close()
	svc := &ImagePatchService{
		settingsService: settingsSvc,
		dockerService:   &docker.DockerClientService{Client: dockerClient},
		// Any attempt to scan would access unconfigured dependencies and fail.
		vulnerabilityService: &vulnerability.VulnerabilityService{},
	}
	require.NotPanics(t, func() {
		svc.verifyPatchedImageInternal(t.Context(), &ImagePatchRecord{EnvironmentID: "0", PatchedRef: "test:patched"}, "")
	})
	require.Equal(t, int32(1), inspected.Load())
}
