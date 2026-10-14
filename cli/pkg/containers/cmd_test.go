package containers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/testutil"
)

func TestContainerStartSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/containers/abc123":
			// Resolution succeeds so the action POST below is reached.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"abc123","name":"nginx"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/containers/abc123/start":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"data":{"error":"permission denied: containers:start"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "containers", "start", "abc123"},
	)

	require.Error(t, err,
		"expected an error for a 403 start response, got success\nstdout=%s\nstderr=%s", outBuf, errOut)

	require.Contains(t, err.Error(), "403",
		"expected the error to mention status 403, got: %v", err)

	require.NotContains(t, outBuf, "successfully",
		"expected no success message on a denied start, got stdout=%s", outBuf)
	for _, tc := range []struct {
		name        string
		identifier  string
		duplicate   bool
		unavailable bool
		search      bool
		prefix      bool
		explicit    bool
	}{
		{name: "unique environment", identifier: "abc123"},
		{name: "duplicate ID", identifier: "abc123", duplicate: true},
		{name: "partial search", identifier: "abc123", unavailable: true},
		{name: "name search", identifier: "nginx", search: true},
		{name: "ID prefix fallback", identifier: "abcd", prefix: true},
		{name: "explicit environment", identifier: "abc123", duplicate: true, explicit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var mutations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/environments" {
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"0","name":"Local","enabled":true},{"id":"remote","name":"Remote","enabled":true}]}`))
					return
				}
				if r.Method == http.MethodPost {
					mu.Lock()
					mutations = append(mutations, r.URL.Path)
					mu.Unlock()
					_, _ = w.Write([]byte(`{"success":true,"data":{"message":"started"}}`))
					return
				}
				local := strings.Contains(r.URL.Path, "/0/")
				if local && tc.unavailable {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"success":false,"error":"search denied"}`))
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers") && !local && (tc.search || tc.prefix && r.URL.Query().Get("search") == ""):
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"abcd1234","names":["/nginx"]}]}`))
					return
				case strings.HasSuffix(r.URL.Path, "/containers"):
					_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
					return
				case (!local || tc.duplicate) && !tc.search && !tc.prefix:
					_, _ = w.Write([]byte(`{"success":true,"data":{"id":"abc123","name":"nginx"}}`))
					return
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			}))
			defer server.Close()
			path := testutil.WriteConfig(t, server.URL, true)
			args := []string{"--config", path, "--json", "--yes"}
			if tc.explicit {
				args = append(args, "--env", "remote")
			}
			out, diagnostics, runErr := testutil.Execute(t, append(args, "containers", "start", tc.identifier))
			mu.Lock()
			paths := append([]string(nil), mutations...)
			mu.Unlock()
			if !tc.explicit && (tc.duplicate || tc.unavailable) {
				require.ErrorContains(t, runErr, "pass --env")
				require.Empty(t, paths)
				require.Empty(t, out)
				return
			}
			require.NoError(t, runErr, diagnostics)
			resourceID := "abc123"
			if tc.search || tc.prefix {
				resourceID = "abcd1234"
			}
			require.Equal(t, []string{"/api/environments/remote/containers/" + resourceID + "/start"}, paths)
			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &result), out)
			require.NotContains(t, result, "environments")
		})
	}
}

func TestContainersListSendsLimitAndStart(t *testing.T) {
	for _, tc := range []struct {
		name         string
		preview, all bool
	}{
		{name: "saved environment"}, {name: "preview pagination", preview: true}, {name: "preview all", preview: true, all: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			queries := make(map[string]string)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/environments" {
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"0","name":"Local","enabled":true},{"id":"remote","name":"Remote","enabled":true}]}`))
					return
				}
				mu.Lock()
				queries[r.URL.Path] = r.URL.RawQuery
				mu.Unlock()
				_, _ = w.Write([]byte(`{"success":true,"data":[],"pagination":{"totalPages":4,"totalItems":20,"currentPage":3,"itemsPerPage":5}}`))
			}))
			defer server.Close()
			args := []string{"--config", testutil.WriteConfig(t, server.URL, tc.preview), "containers", "list", "--json"}
			if tc.all {
				args = append(args, "--all")
			} else {
				args = append(args, "--limit", "5", "--start", "10")
			}
			out, diagnostics, err := testutil.Execute(t, args)
			require.NoError(t, err, diagnostics)
			mu.Lock()
			defer mu.Unlock()
			wantQuery := "limit=5&start=10"
			if tc.all {
				wantQuery = "limit=-1"
			}
			require.Equal(t, map[string]string{kit.Ternary(tc.preview, "/api/environments/0,remote/containers", "/api/environments/0/containers"): wantQuery}, queries)
			require.True(t, json.Valid([]byte(out)))
			require.Contains(t, out, "pagination")
		})
	}
}

func TestContainersListExplicitStartZeroSendsStartZero(t *testing.T) {
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
			"data": [],
			"pagination": {"totalPages":2,"totalItems":26,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "containers", "list", "--json", "--start", "0"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/environments/0/containers", gotPath,
		"path = %q, want %q", gotPath, "/api/environments/0/containers")

	require.True(t, strings.Contains(gotQuery, "start=0") && strings.Contains(gotQuery, "limit=20"),
		"query = %q, want start=0 and limit=20", gotQuery)
}

func TestContainersListTextShowsShowingSummary(t *testing.T) {
	const localRow = `{"environmentId":"0","environmentName":"Local","id":"same-id","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}`
	for _, tc := range []struct {
		name                                  string
		command                               []string
		preview, unavailable, empty, explicit bool
		summary                               string
	}{
		{name: "single environment", summary: "Showing: 1/26 containers"},
		{name: "combined environments", preview: true, summary: "Showing: 2/52 containers"},
		{name: "partial failure", preview: true, unavailable: true, summary: "Showing: 1/26 containers"},
		{name: "empty resources", preview: true, empty: true, summary: "Showing: 0/0 containers"},
		{name: "explicit override", preview: true, explicit: true, summary: "Showing: 1/26 containers"},
		{name: "custom jobs", preview: true, command: []string{"jobs", "list"}, summary: "Total: 2 jobs"},
		{name: "custom settings", preview: true, command: []string{"settings", "list"}, summary: "Total: 2 settings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/api/environments":
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"remote","name":"Remote","enabled":true},{"id":"0","name":"Local","enabled":true}]}`))
				case tc.empty:
					_, _ = w.Write([]byte(`{"success":true,"data":[],"pagination":{"totalItems":0}}`))
				case r.URL.Path == "/api/environments/0,remote/containers" && tc.unavailable:
					_, _ = w.Write([]byte(`{"success":true,"data":[` + localRow + `],"pagination":{"totalItems":26},` +
						`"failedEnvironments":[{"environment":{"id":"remote","name":"remote"},"message":"resource denied"}]}`))
				case r.URL.Path == "/api/environments/0,remote/containers":
					remoteRow := strings.Replace(localRow, `"0","environmentName":"Local"`, `"remote","environmentName":"Remote"`, 1)
					_, _ = w.Write([]byte(`{"success":true,"data":[` + localRow + `,` + remoteRow + `],"pagination":{"totalItems":52}}`))
				case strings.HasSuffix(r.URL.Path, "/jobs"):
					_, _ = w.Write([]byte(`{"jobs":[{"id":"same-id","name":"nginx"}],"isAgent":false,"durableRuns":true}`))
				case strings.HasSuffix(r.URL.Path, "/settings"):
					_, _ = w.Write([]byte(`[{"key":"nginx","value":"dark","type":"string"}]`))
				default:
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"same-id","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}],"pagination":{"totalItems":26}}`))
				}
			}))
			defer srv.Close()
			configPath := testutil.WriteConfig(t, srv.URL, tc.preview)
			args := []string{"--config", configPath, "--no-color"}
			if tc.explicit {
				args = append(args, "--env", "remote")
			}
			command := tc.command
			if command == nil {
				command = []string{"containers", "list"}
			}
			out, diagnostics, err := testutil.Execute(t, append(args, command...))
			if tc.unavailable {
				require.ErrorContains(t, err, "resource denied")
			} else {
				require.NoError(t, err, diagnostics)
			}
			require.Contains(t, out, tc.summary)
			require.Equal(t, 1, strings.Count(out, tc.summary))
			require.NotContains(t, out, "\x1b")
			if !tc.preview || tc.explicit {
				require.NotContains(t, out, "ENVIRONMENT")
				return
			}
			require.Equal(t, 1, strings.Count(out, "ENVIRONMENT"), out)
			require.Equal(t, 1, strings.Count(out, "TYPE")+strings.Count(out, "NAME"), out)
			if tc.empty {
				require.NotContains(t, out, "nginx")
				return
			}
			require.Contains(t, out, "Local")
			require.NotContains(t, out, "(0)")
			require.NotContains(t, out, "(remote)")
			if tc.unavailable {
				require.NotContains(t, out, "Remote")
				require.Contains(t, diagnostics, "Remote (remote): resource denied")
				return
			}
			require.Contains(t, out, "Remote")
			require.Equal(t, 2, strings.Count(out, "Local")+strings.Count(out, "Remote"))
		})
	}
}

func TestContainersListAllRequestsEveryItem(t *testing.T) {
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
			"data": [],
			"pagination": {"totalPages":1,"totalItems":0,"currentPage":1,"itemsPerPage":0}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "containers", "list", "--json", "--all"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/environments/0/containers", gotPath,
		"path = %q, want %q", gotPath, "/api/environments/0/containers")

	require.Equal(t, "limit=-1", gotQuery,
		"query = %q, want %q", gotQuery, "limit=-1")
}

func TestContainersListJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/environments/0/containers") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{"id":"abc123","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)

	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "--log-level", "debug", "containers", "list", "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Contains(t, errOut, "Sending request")
	require.NotContains(t, outBuf, "Sending request")
	require.NotContains(t, outBuf, "\x1b")
	var got map[string]any
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, unmarshalErr,
			"json parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}

	for _, key := range []string{"success", "data", "pagination"} {
		require.Contains(t, got, key)
	}
	for _, tc := range []struct {
		name             string
		args             []string
		unavailable      bool
		empty            bool
		discoveryFailure bool
		explicit         bool
	}{
		{name: "preview retains envelopes"},
		{name: "preview partial failure", unavailable: true},
		{name: "preview empty", empty: true},
		{name: "preview discovery failure", discoveryFailure: true},
		{name: "preview explicit override", explicit: true},
		{name: "preview custom job response", args: []string{"jobs", "list"}},
		{name: "preview settings response", args: []string{"settings", "list"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requested []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requested = append(requested, r.URL.Path)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/api/environments" && tc.discoveryFailure:
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"success":false,"error":"discovery denied"}`))
					return
				case r.URL.Path == "/api/environments" && tc.empty:
					_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
					return
				case r.URL.Path == "/api/environments":
					assert.Equal(t, "-1", r.URL.Query().Get("limit"))
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"remote","name":"Remote","enabled":true},{"id":"disabled","name":"Disabled","enabled":false},{"id":"0","name":"Local","enabled":true}]}`))
					return
				case tc.unavailable && strings.Contains(r.URL.Path, "/remote/"):
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"success":false,"error":"resource denied"}`))
					return
				case r.URL.Path == "/api/environments/0,remote/containers" && tc.unavailable:
					_, _ = w.Write([]byte(`{"success":true,"data":[{"environmentId":"0","environmentName":"Local","id":"same-id"}],"pagination":{"totalItems":1},` +
						`"failedEnvironments":[{"environment":{"id":"remote","name":"remote"},"message":"resource denied"}]}`))
				case strings.HasSuffix(r.URL.Path, "/jobs"):
					_, _ = w.Write([]byte(`{"jobs":[],"isAgent":false,"durableRuns":true}`))
				case strings.HasSuffix(r.URL.Path, "/settings"):
					_, _ = w.Write([]byte(`[{"key":"theme","value":"dark","type":"string"}]`))
				default:
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"same-id","names":["/nginx"]}],"pagination":{"totalItems":1},"counts":{"running":1},"groups":["preserved"]}`))
				}
			}))
			defer server.Close()
			path := testutil.WriteConfig(t, server.URL, true)
			args := []string{"--config", path, "--json"}
			if tc.explicit {
				args = append(args, "--env", "remote")
			}
			command := tc.args
			if command == nil {
				command = []string{"containers", "list"}
			}
			out, diagnostics, runErr := testutil.Execute(t, append(args, command...))
			if tc.discoveryFailure {
				require.ErrorContains(t, runErr, "discovery denied")
				require.Empty(t, out)
				return
			}
			if tc.unavailable {
				require.ErrorContains(t, runErr, "resource denied")
			} else {
				require.NoError(t, runErr, diagnostics)
			}
			mu.Lock()
			paths := append([]string(nil), requested...)
			mu.Unlock()
			if tc.args == nil && !tc.explicit && !tc.empty {
				require.Equal(t, []string{"/api/environments", "/api/environments/0,remote/containers"}, paths)
				require.True(t, json.Valid([]byte(out)), out)
				if tc.unavailable {
					require.ErrorContains(t, runErr, "Remote (remote): resource denied")
					require.Contains(t, out, "failedEnvironments")
					return
				}
				require.Contains(t, out, "counts")
				require.Contains(t, out, "groups")
				return
			}
			var result struct {
				Success      bool `json:"success"`
				Environments []struct {
					ID     string          `json:"environmentId"`
					Result json.RawMessage `json:"result"`
					Error  string          `json:"error"`
				} `json:"environments"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &result), out)
			if tc.explicit {
				require.NotContains(t, paths, "/api/environments")
				require.Empty(t, result.Environments)
				require.Contains(t, out, "counts")
				return
			}
			require.Equal(t, !tc.unavailable, result.Success)
			require.NotContains(t, strings.Join(paths, ","), "disabled")
			if tc.empty {
				require.Empty(t, result.Environments)
				require.Len(t, paths, 1)
				return
			}
			require.Len(t, result.Environments, 2)
			require.Equal(t, "0", result.Environments[0].ID)
			require.Equal(t, "remote", result.Environments[1].ID)
			require.NotContains(t, out, "Selected")
		})
	}
}

func TestContainersListUpdatesJSONContract(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/containers" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}

		seenUpdatesFilter = r.URL.Query().Get("updates")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id":"abc123",
					"names":["/nginx"],
					"image":"nginx:latest",
					"state":"running",
					"status":"Up 1 hour",
					"updateInfo":{"hasUpdate":true,"latestVersion":"1.28.0"}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "containers", "list", "--updates", "has_update", "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Equal(t, "has_update", seenUpdatesFilter,
		"expected updates filter query param, got %q", seenUpdatesFilter)

	var got map[string]any
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, unmarshalErr,
			"json parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}

	for _, key := range []string{"success", "data", "pagination"} {
		{
			_, ok := got[key]
			require.True(t, ok,
				"missing key %q in output: %v", key, got)
		}
	}
}

func TestContainersUpdatesCommandUsesHasUpdateFilter(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/containers" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}

		seenUpdatesFilter = r.URL.Query().Get("updates")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id":"abc123",
					"names":["/nginx"],
					"image":"nginx:latest",
					"state":"running",
					"status":"Up 1 hour",
					"updateInfo":{"hasUpdate":true,"latestVersion":"1.28.0"}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "--output", "text", "containers", "updates"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Equal(t, "has_update", seenUpdatesFilter,
		"expected updates filter query param, got %q", seenUpdatesFilter)

	require.NotEmpty(t, strings.TrimSpace(outBuf),
		"expected output from containers updates command")
}
