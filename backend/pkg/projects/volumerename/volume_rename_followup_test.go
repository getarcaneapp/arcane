package volumerename

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerProjectVolumeRenameMigrationInternal_RollbackExplainsPreservedTargetWhenSourceMissing(t *testing.T) {
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
		dockerClient: newTestDockerClient(t, server),
		createdNew: []projectVolumeRenameEntryInternal{
			{OldName: "nginx_data", NewName: "web_data"},
		},
	}

	err := migration.Rollback(context.Background())

	require.Error(t, err)
	var preserved *TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.Contains(t, err.Error(), "avoid data loss")
	require.Contains(t, err.Error(), "only remaining data copy")
	require.False(t, targetRemoved.Load())
}

func TestRollbackVolume_ExplainsPreservedTargetWhenSourceMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/nginx_data"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/web_data"):
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Name": "web_data"}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	err := RollbackVolume(context.Background(), newTestDockerClient(t, server), JournalVolume{
		OldName: "nginx_data",
		NewName: "web_data",
	})

	require.Error(t, err)
	var preserved *TargetPreservedDuringRollbackError
	require.ErrorAs(t, err, &preserved)
	require.Contains(t, err.Error(), "avoid data loss")
	require.Contains(t, err.Error(), "only remaining data copy")
}
