package health

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
)

func newReadyTestService(t *testing.T, shutdown <-chan struct{}) (*Service, *database.DB) {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open("file:health-ready?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db := &database.DB{DB: gormDB}
	t.Cleanup(func() { _ = db.Close() })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Api-Version", "1.41")
		_, _ = w.Write([]byte("OK"))
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	return &Service{
		db:       db,
		docker:   (&docker.DockerClientService{}).WithClient(dockerClient),
		shutdown: shutdown,
	}, db
}

func TestService_Ready(t *testing.T) {
	t.Run("ready when database and docker respond", func(t *testing.T) {
		svc, _ := newReadyTestService(t, make(chan struct{}))

		assert.Equal(t, types.ReadinessResponse{
			Status: readyStatus,
			Checks: map[string]string{
				"database": checkOK,
				"docker":   checkOK,
			},
		}, svc.Ready(t.Context()))
	})

	t.Run("not ready when database ping fails", func(t *testing.T) {
		svc, db := newReadyTestService(t, make(chan struct{}))
		require.NoError(t, db.Close())

		result := svc.Ready(t.Context())
		assert.Equal(t, notReadyStatus, result.Status)
		assert.NotEqual(t, checkOK, result.Checks["database"])
		assert.Equal(t, checkOK, result.Checks["docker"])
	})

	t.Run("not ready while shutting down", func(t *testing.T) {
		shutdown := make(chan struct{})
		close(shutdown)
		svc, _ := newReadyTestService(t, shutdown)

		result := svc.Ready(t.Context())
		assert.Equal(t, notReadyStatus, result.Status)
		assert.Equal(t, map[string]string{"shutdown": "in progress"}, result.Checks)
	})
}
