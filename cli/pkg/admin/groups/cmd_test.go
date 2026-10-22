package groups_test

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestAdminGroupsMembersAddSendsUserIDs(t *testing.T) {
	type request struct {
		method string
		path   string
		body   []byte
	}
	requests := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{
			method: r.Method,
			path:   r.URL.Path,
			body:   body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer srv.Close()

	_, errOut, err := testutil.Execute(t, []string{
		"--config", testutil.WriteConfig(t, srv.URL), "admin", "groups", "members", "add", "--json", "g1", "u1", "u2",
	})
	require.NoError(t, err, errOut)

	got := <-requests
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/api/groups/g1/members", got.path)
	var body types.AddGroupMembers
	require.NoError(t, json.Unmarshal(got.body, &body))
	require.Equal(t, []string{"u1", "u2"}, body.UserIDs)
}
