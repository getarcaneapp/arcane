package patch

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
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

	svc := NewService(db, docker.NewDockerClientService(ctx, nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-test.sock"}, nil), nil, nil, nil, nil)

	records := []vulnerability.VulnerabilityScanRecord{
		{
			ID:           "sha256:aaa",
			ImageName:    "nginx:latest",
			Status:       vulnerability.ScanStatusCompleted,
			ScanTime:     time.Now(),
			FixableCount: new(3),
		},
		{
			ID:           "sha256:bbb",
			ImageName:    "sha256:bbb",
			Status:       vulnerability.ScanStatusCompleted,
			ScanTime:     time.Now(),
			FixableCount: new(3),
		},
		{
			ID:           "sha256:ccc",
			ImageName:    "<none>:<none>",
			Status:       vulnerability.ScanStatusCompleted,
			ScanTime:     time.Now(),
			FixableCount: new(3),
		},
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
	svc, err := settings.NewSettingsService(t.Context(), db, &config.Config{})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	require.NoError(t, err)
	return svc, db
}

func TestVulnerabilityFeatureBlocksReportPatchesOnly(t *testing.T) {
	settingsSvc, db := patchFeatureSettings(t)
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), types.FeatureVulnerabilityManagementSettingKey, false))
	svc := NewService(
		db,
		docker.NewDockerClientService(t.Context(), nil, &config.Config{DockerHost: "unix:///nonexistent-arcane-feature-test.sock"}, nil),
		settingsSvc,
		nil,
		nil,
		nil,
	)
	_, err := svc.PatchImage(t.Context(), "0", "test-image", types.PatchOptions{ScanID: "test-image"}, types.Actor{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, _, err = svc.ListPatchTargets(t.Context(), "0", pagination.QueryParams{})
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	_, err = svc.Targets(t.Context(), "0")
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	// Standalone patching still reaches Docker; this test deliberately has no daemon.
	_, err = svc.PatchImage(t.Context(), "0", "test-image", types.PatchOptions{}, types.Actor{})
	require.Error(t, err)
	require.NotErrorIs(t, err, common.ErrFeatureDisabled)
	require.Contains(t, err.Error(), "failed to connect to Docker")
}

func TestQueuedReportPatchStopsWhenFeatureDisabled(t *testing.T) {
	settingsSvc, db := patchFeatureSettings(t)
	svc := NewService(db, nil, settingsSvc, nil, nil, nil)
	record := &ImagePatchRecord{
		EnvironmentID:   "0",
		OriginalImageID: "test-image",
		Mode:            string(types.PatchModeReport),
		Status:          string(types.PatchStatusPatching),
	}
	require.NoError(t, db.Create(record).Error)
	svc.patchSlot <- struct{}{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.runPatch(t.Context(), record, types.PatchOptions{ScanID: "test-image"}, nil, "", "")
	}()
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), types.FeatureVulnerabilityManagementSettingKey, false))
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
	require.Equal(t, string(types.PatchStatusFailed), stored.Status)
	require.NotNil(t, stored.Error)
	require.Contains(t, *stored.Error, string(types.FeatureVulnerabilityManagement))
	require.Empty(t, svc.patchSlot)
}

func TestDisabledFeatureSkipsPatchVerificationScan(t *testing.T) {
	settingsSvc, _ := patchFeatureSettings(t)
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), types.FeatureVulnerabilityManagementSettingKey, false))
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
	svc := NewService(
		nil,
		&docker.DockerClientService{Client: dockerClient},
		settingsSvc,
		nil,
		nil,
		// Any attempt to scan would access unconfigured dependencies and fail.
		&vulnerability.VulnerabilityService{},
	)
	require.NotPanics(t, func() {
		svc.verifyPatchedImage(t.Context(), &ImagePatchRecord{EnvironmentID: "0", PatchedRef: "test:patched"}, "")
	})
	require.Equal(t, int32(1), inspected.Load())
}

func TestAggregatePatchTargetsMergesEnvironments(t *testing.T) {
	scanned := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	bodies := map[string]types.Paginated[types.PatchTarget]{
		"a": {
			Success: true,
			Data: []types.PatchTarget{
				{
					ImageID:      "sha256:a1",
					ImageRef:     "nginx:1",
					FixableCount: 3,
					ScanTime:     scanned.Add(2 * time.Hour),
				},
				{
					ImageID:      "sha256:a2",
					ImageRef:     "redis:7",
					FixableCount: 1,
					ScanTime:     scanned,
				},
			},
			Pagination: types.PaginationResponse{TotalItems: 2, GrandTotalItems: 4},
		},
		"b": {
			Success: true,
			Data: []types.PatchTarget{
				{
					ImageID:      "sha256:b1",
					ImageRef:     "alpine:3",
					FixableCount: 5,
					ScanTime:     scanned.Add(time.Hour),
				},
			},
			Pagination: types.PaginationResponse{TotalItems: 1},
		},
	}
	var queries sync.Map
	ctx := httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, "/api/environments/a,b,c/images/patches/targets?start=0&limit=20&order=desc", http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/environments/"), "/")[0]
			queries.Store(id, r.URL.Query())
			body, ok := bodies[id]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			payload, err := json.Marshal(body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
	rowLabels := func(items []types.PatchTarget) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, item.EnvironmentID+"/"+item.ImageRef)
		}
		return out
	}
	svc := NewService(nil, nil, nil, nil, nil, nil)
	merge := func(sort string, order pagination.SortOrder, start, limit int) types.Paginated[types.PatchTarget] {
		body, err := svc.AggregatePatchTargets(ctx, pagination.QueryParams{
			SortParams: pagination.SortParams{Sort: sort, Order: order},
			Params:     pagination.Params{Start: start, Limit: limit},
		})
		require.NoError(t, err)
		return body
	}

	defaults := merge("", pagination.SortDesc, 0, 20)
	assert.True(t, defaults.Success)
	assert.Equal(t, []string{"a/nginx:1", "b/alpine:3", "a/redis:7"}, rowLabels(defaults.Data))
	assert.Equal(t, "Env a", defaults.Data[0].EnvironmentName)
	assert.Equal(t, int64(3), defaults.Pagination.TotalItems)
	assert.Equal(t, int64(5), defaults.Pagination.GrandTotalItems)
	require.Len(t, defaults.FailedEnvironments, 1)
	assert.Equal(t, "c", defaults.FailedEnvironments[0].Environment.ID)
	for _, id := range []string{"a", "b"} {
		query, ok := queries.Load(id)
		require.True(t, ok)
		assert.Equal(t, "0", query.(url.Values).Get("start"))
		assert.Equal(t, "-1", query.(url.Values).Get("limit"))
	}

	assert.Equal(t, []string{"b/alpine:3", "a/nginx:1", "a/redis:7"}, rowLabels(merge("imageName", pagination.SortAsc, 0, -1).Data))
	assert.Equal(t, []string{"b/alpine:3", "a/nginx:1"}, rowLabels(merge("fixableCount", pagination.SortDesc, 0, 2).Data))
	assert.Equal(t, []string{"a/redis:7", "a/nginx:1", "b/alpine:3"}, rowLabels(merge("environment", pagination.SortAsc, 0, -1).Data))
}

func TestRecoverInterruptedPatchesFailsLeftoverPatchingRecords(t *testing.T) {
	_, db := patchFeatureSettings(t)
	svc := &Service{db: db}
	for _, record := range []ImagePatchRecord{
		{
			BaseModel: database.BaseModel{ID: "stuck"},
			Status:    string(types.PatchStatusPatching),
		},
		{
			BaseModel: database.BaseModel{ID: "done"},
			Status:    string(types.PatchStatusCompleted),
		},
	} {
		require.NoError(t, db.Create(&record).Error)
	}

	require.NoError(t, svc.RecoverInterruptedPatches(t.Context()))

	var stuck, done ImagePatchRecord
	require.NoError(t, db.First(&stuck, "id = ?", "stuck").Error)
	require.NoError(t, db.First(&done, "id = ?", "done").Error)
	assert.Equal(t, string(types.PatchStatusFailed), stuck.Status)
	assert.Equal(t, errPatchInterrupted.Error(), kit.FromPtr(stuck.Error))
	assert.Equal(t, string(types.PatchStatusCompleted), done.Status)
	assert.Nil(t, done.Error)
}
