package events_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestAdminEventsListEnvSendsLimitAndStart(t *testing.T) {
	var (
		mu       sync.Mutex
		gotPath  string
		gotQuery string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id":"evt-1",
					"type":"container.start",
					"severity":"info",
					"title":"Container started",
					"timestamp":"2026-04-08T12:00:00Z",
					"createdAt":"2026-04-08T12:00:00Z"
				}
			],
			"pagination": {"totalPages":5,"totalItems":15,"currentPage":3,"itemsPerPage":3}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "admin", "events", "list", "--environment", "--json", "--limit", "3", "--start", "6"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/events/environment/0", gotPath,
		"path = %q, want %q", gotPath, "/api/events/environment/0")

	require.True(t, strings.Contains(gotQuery, "limit=3") && strings.Contains(gotQuery, "start=6"),
		"query = %q, want limit=3 and start=6", gotQuery)
}
