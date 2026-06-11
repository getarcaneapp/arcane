package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectVolumeCopyCommandInternal_ChecksCapacityBeforeCopy(t *testing.T) {
	command := projectVolumeCopyCommandInternal()

	require.Contains(t, command, "du -sk /from")
	require.Contains(t, command, "df -Pk /to")
	require.Contains(t, command, "required_kb")
	require.Contains(t, command, "exit 99")
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
