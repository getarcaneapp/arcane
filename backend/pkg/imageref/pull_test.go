package imageref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullAsTag(t *testing.T) {
	const pulled = "ghcr.io/getarcaneapp/arcane@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const selected = "ghcr.io/getarcaneapp/arcane:next"
	noPull := func(context.Context) error { return nil }

	t.Run("rejects a pull from another repository", func(t *testing.T) {
		err := PullAsTag(t.Context(), nil, "ghcr.io/getarcaneapp/arcane-agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", selected, noPull)
		require.ErrorContains(t, err, "does not belong to repository")
	})

	newClient := func(t *testing.T, selectedID string, tagStatus int) *client.Client {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tag"):
				assert.Equal(t, "next", r.URL.Query().Get("tag"))
				w.WriteHeader(tagStatus)
			case strings.Contains(r.URL.Path, "@sha256:") && strings.HasSuffix(r.URL.Path, "/json"):
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"Id":"sha256:pulled"}`))
				assert.NoError(t, err)
			case strings.Contains(r.URL.Path, "arcane:next/json"):
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"Id":"` + selectedID + `"}`))
				assert.NoError(t, err)
			default:
				assert.Failf(t, "unexpected failure", "unexpected Docker API request: %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		dockerClient, err := client.New(
			client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
			client.WithAPIVersion("1.41"),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = dockerClient.Close() })
		return dockerClient
	}

	t.Run("tags and verifies the selected reference", func(t *testing.T) {
		require.NoError(t, PullAsTag(t.Context(), newClient(t, "sha256:pulled", http.StatusCreated), pulled, selected, noPull))
	})

	t.Run("fails when tagging fails", func(t *testing.T) {
		require.Error(t, PullAsTag(t.Context(), newClient(t, "sha256:pulled", http.StatusInternalServerError), pulled, selected, noPull))
	})

	t.Run("fails when the tag resolves elsewhere", func(t *testing.T) {
		err := PullAsTag(t.Context(), newClient(t, "sha256:other", http.StatusCreated), pulled, selected, noPull)
		require.ErrorContains(t, err, "resolves to sha256:other")
	})
}
