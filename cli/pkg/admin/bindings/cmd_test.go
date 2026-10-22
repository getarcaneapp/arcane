package bindings_test

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

type capturedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
}

func newCaptureServer(t *testing.T, response string) (*httptest.Server, chan capturedRequest) {
	t.Helper()
	requests := make(chan capturedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- capturedRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
			body:   body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

func TestAdminBindingsListSendsSubjectFilters(t *testing.T) {
	srv, requests := newCaptureServer(t, `{"success":true,"data":[],"pagination":{"totalPages":0,"totalItems":0,"currentPage":1,"itemsPerPage":20}}`)

	_, errOut, err := testutil.Execute(t, []string{
		"--config", testutil.WriteConfig(t, srv.URL), "admin", "bindings", "list", "--json", "--user", "u1", "--role", "r1", "--environment", "0",
	})
	require.NoError(t, err, errOut)

	got := <-requests
	require.Equal(t, "/api/roles/bindings", got.path)
	require.Equal(t, "user", got.query.Get("subjectType"))
	require.Equal(t, "u1", got.query.Get("subjectId"))
	require.Equal(t, "r1", got.query.Get("roleId"))
	require.Equal(t, "0", got.query.Get("environmentId"))
}

func TestAdminBindingsCreateSendsBinding(t *testing.T) {
	srv, requests := newCaptureServer(t, `{"success":true,"data":{"id":"b1","subjectType":"group","subjectId":"g1","subjectName":"ops",`+
		`"roleId":"r1","roleName":"Operator","source":"manual","createdAt":"2026-10-21T00:00:00Z"}}`)

	_, errOut, err := testutil.Execute(t, []string{
		"--config", testutil.WriteConfig(t, srv.URL), "admin", "bindings", "create", "--json", "--group", "g1", "--role", "r1", "--environment", "0",
	})
	require.NoError(t, err, errOut)

	got := <-requests
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/api/roles/bindings", got.path)
	var body types.CreateRoleBinding
	require.NoError(t, json.Unmarshal(got.body, &body))
	require.Equal(t, types.CreateRoleBinding{
		SubjectType:   "group",
		SubjectID:     "g1",
		RoleID:        "r1",
		EnvironmentID: new("0"),
	}, body)
}
