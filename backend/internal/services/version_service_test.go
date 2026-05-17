package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/pkg/utils/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionService_ManualUpdateRequirement(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{
			name:    "pre boundary to boundary requires manual update",
			current: "v1.19.2",
			target:  "v1.20.0",
			want:    true,
		},
		{
			name:    "pre boundary to newer requires manual update",
			current: "1.19.2",
			target:  "1.21.0",
			want:    true,
		},
		{
			name:    "boundary to newer can use automatic update",
			current: "v1.20.0",
			target:  "v1.21.0",
			want:    false,
		},
		{
			name:    "non semver current does not force manual update",
			current: "next",
			target:  "v1.20.0",
			want:    false,
		},
		{
			name:    "older target does not force manual update",
			current: "v1.18.0",
			target:  "v1.19.2",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestVersionService(t, "v1.19.2", manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
				SchemaVersion: 1,
				ManualUpdateBoundaries: []manualUpdateBoundary{
					{Version: "v1.20.0", Message: "v1.20 manual step"},
				},
			}), "v1.21.0")

			required, message := svc.ManualUpdateRequirement(ctx, tt.current, tt.target)

			assert.Equal(t, tt.want, required)
			if tt.want {
				assert.NotEmpty(t, message)
			} else {
				assert.Empty(t, message)
			}
		})
	}
}

func TestVersionService_ManualUpdateRequirement_MultipleBoundaries(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
		SchemaVersion: 1,
		ManualUpdateBoundaries: []manualUpdateBoundary{
			{Version: "v2.0.0", Message: "v2 manual step"},
			{Version: "v1.20.0", Message: "v1.20 manual step"},
		},
	}), "v2.1.0")

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "v2.1.0")

	assert.True(t, required)
	assert.Equal(t, "v1.20 manual step", message)
}

func TestVersionService_ManualUpdateRequirement_EmptyManifestAllowsAutomaticUpdate(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
		SchemaVersion:          1,
		ManualUpdateBoundaries: []manualUpdateBoundary{},
	}), "v1.20.0")

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "v1.20.0")

	assert.False(t, required)
	assert.Empty(t, message)
}

func TestVersionService_ManualUpdateRequirement_FetchesLatestWhenTargetMissing(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
		SchemaVersion: 1,
		ManualUpdateBoundaries: []manualUpdateBoundary{
			{Version: "v1.20.0", Message: "v1.20 manual step"},
		},
	}), "v1.20.0")

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "")

	assert.True(t, required)
	assert.Equal(t, "v1.20 manual step", message)
}

func TestVersionService_ManualUpdateRequirement_FailsClosedWhenTargetMissingAndUpdateChecksDisabled(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
		SchemaVersion: 1,
		ManualUpdateBoundaries: []manualUpdateBoundary{
			{Version: "v1.20.0", Message: "v1.20 manual step"},
		},
	}), "v1.20.0")
	svc.disabled = true

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "")

	assert.True(t, required)
	assert.Equal(t, "v1.20 manual step", message)
}

func TestVersionService_ManualUpdateRequirement_FailsClosedWhenManifestUnavailable(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}, "v1.20.0")

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "v1.20.0")

	assert.True(t, required)
	assert.Contains(t, message, "could not verify the remote manual update manifest")
}

func TestVersionService_ManualUpdateRequirement_FailsClosedWhenManifestInvalid(t *testing.T) {
	ctx := context.Background()
	svc := newTestVersionService(t, "v1.19.2", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"schemaVersion":2,"manualUpdateBoundaries":[]}`))
	}, "v1.20.0")

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "v1.20.0")

	assert.True(t, required)
	assert.Contains(t, message, "could not verify the remote manual update manifest")
}

func TestVersionService_ManualUpdateRequirement_UsesStaleManifestOnRefreshFailure(t *testing.T) {
	ctx := context.Background()
	var failRefresh atomic.Bool

	svc := newTestVersionService(t, "v1.19.2", func(w http.ResponseWriter, r *http.Request) {
		if failRefresh.Load() {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		manualUpdateManifestHandler(t, http.StatusOK, manualUpdateManifest{
			SchemaVersion: 1,
			ManualUpdateBoundaries: []manualUpdateBoundary{
				{Version: "v1.20.0", Message: "v1.20 manual step"},
			},
		})(w, r)
	}, "v1.21.0")
	svc.manualUpdatesCache = cache.New[manualUpdateManifest](time.Nanosecond)

	required, message := svc.ManualUpdateRequirement(ctx, "v1.19.2", "v1.20.0")
	require.True(t, required)
	require.Equal(t, "v1.20 manual step", message)

	failRefresh.Store(true)
	time.Sleep(2 * time.Millisecond)

	required, message = svc.ManualUpdateRequirement(ctx, "v1.19.2", "v1.21.0")

	assert.True(t, required)
	assert.Equal(t, "v1.20 manual step", message)
}

func newTestVersionService(t *testing.T, currentVersion string, manualUpdatesHandler http.HandlerFunc, latestVersion string) *VersionService {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/manual_updates.json", manualUpdatesHandler)
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"tag_name": latestVersion,
		}))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	svc := NewVersionService(server.Client(), false, currentVersion, "test", nil, nil)
	svc.latestReleaseURL = server.URL + "/latest"
	svc.manualUpdatesURL = server.URL + "/manual_updates.json"
	return svc
}

func manualUpdateManifestHandler(t *testing.T, statusCode int, manifest manualUpdateManifest) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			require.NoError(t, json.NewEncoder(w).Encode(manifest))
		}
	}
}
