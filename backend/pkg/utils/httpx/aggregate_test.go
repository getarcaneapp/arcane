package httpx

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

type aggregateRow struct {
	ID string `json:"id"`
}

func aggregateContext(t *testing.T, handler http.Handler, ids ...string) context.Context {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/environments/"+strings.Join(ids, ",")+"/containers?search=web&sort=environment&order=desc&drop=1", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("If-None-Match", `"etag"`)
	return WithEnvironmentScope(t.Context(), &EnvironmentScope{
		IDs:     ids,
		Request: request,
		Handler: handler,
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func aggregateEnvironmentID(r *http.Request) string {
	id, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, environmentsPathPrefix), "/")
	return id
}

func TestCollectEnvironmentsRequiresScope(t *testing.T) {
	_, err := CollectEnvironments[aggregateRow](t.Context(), nil)
	require.Error(t, err)
}

func TestCollectEnvironmentsDispatchesEachEnvironmentInScopeOrder(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]*http.Request{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := aggregateEnvironmentID(r)
		if id == "b" {
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		requests[id] = r
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	})

	collection, err := CollectEnvironments[aggregateRow](aggregateContext(t, handler, "b", "a", "c"), url.Values{"limit": {"-1"}, "drop": nil})

	require.NoError(t, err)
	require.Empty(t, collection.Failures)
	require.Len(t, collection.Results, 3)
	for i, id := range []string{"b", "a", "c"} {
		require.Equal(t, types.EnvironmentRef{ID: id, Name: "Env " + id}, collection.Results[i].Environment)
		require.Equal(t, id, collection.Results[i].Value.ID)
	}

	request := requests["a"]
	require.Equal(t, "/api/environments/a/containers", request.URL.Path)
	require.Equal(t, url.Values{"search": {"web"}, "limit": {"-1"}}, request.URL.Query())
	require.Equal(t, request.URL.RequestURI(), request.RequestURI)
	require.Equal(t, "Bearer token", request.Header.Get("Authorization"))
	require.Equal(t, "application/json", request.Header.Get("Accept"))
	require.Empty(t, request.Header.Get("Accept-Encoding"))
	require.Empty(t, request.Header.Get("If-None-Match"))
	_, scoped := EnvironmentScopeFromContext(request.Context())
	require.False(t, scoped, "sub-requests must not fan out again")
	_, hasDeadline := request.Context().Deadline()
	require.True(t, hasDeadline)
}

func TestCollectEnvironmentsMapsFailures(t *testing.T) {
	bodies := map[string]struct {
		status int
		body   string
	}{
		"huma":      {status: http.StatusUnprocessableEntity, body: `{"title":"Unprocessable Entity","detail":"invalid sort"}`},
		"api":       {status: http.StatusInternalServerError, body: `{"message":"docker unavailable"}`},
		"proxy":     {status: http.StatusBadGateway, body: `{"success":false,"data":{"error":"Agent is not connected"}}`},
		"forbidden": {status: http.StatusForbidden, body: `{"success":false,"data":{"error":"denied"}}`},
		"empty":     {status: http.StatusServiceUnavailable},
		"ok":        {status: http.StatusOK, body: `{"id":"ok"}`},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := bodies[aggregateEnvironmentID(r)]
		w.WriteHeader(response.status)
		_, _ = w.Write([]byte(response.body))
	})

	collection, err := CollectEnvironments[aggregateRow](aggregateContext(t, handler, "huma", "api", "proxy", "forbidden", "empty", "ok"), nil)

	require.NoError(t, err)
	require.Len(t, collection.Results, 1)
	require.Equal(t, []types.EnvironmentFailure{
		{Environment: types.EnvironmentRef{ID: "huma", Name: "huma"}, Message: "invalid sort"},
		{Environment: types.EnvironmentRef{ID: "api", Name: "api"}, Message: "docker unavailable"},
		{Environment: types.EnvironmentRef{ID: "proxy", Name: "proxy"}, Message: "Agent is not connected"},
		{Environment: types.EnvironmentRef{ID: "forbidden", Name: "forbidden"}, Message: "denied"},
		{Environment: types.EnvironmentRef{ID: "empty", Name: "empty"}, Message: "Service Unavailable"},
	}, collection.Failures)
}

func TestCollectEnvironmentsReturnsFailuresWhenEveryEnvironmentFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	collection, err := CollectEnvironments[aggregateRow](aggregateContext(t, handler, "a", "b"), nil)

	require.NoError(t, err)
	require.NotNil(t, collection.Results)
	require.Empty(t, collection.Results)
	require.Len(t, collection.Failures, 2)
	require.Equal(t, types.EnvironmentRef{ID: "a", Name: "a"}, collection.Failures[0].Environment)
}

func TestCollectEnvironmentsRejectsOversizedAndEncodedResponses(t *testing.T) {
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch aggregateEnvironmentID(r) {
		case "large":
			for range environmentResponseLimit/len(chunk) + 1 {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		case "encoded":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte{0x1f, 0x8b})
		}
	})

	collection, err := CollectEnvironments[aggregateRow](aggregateContext(t, handler, "large", "encoded"), nil)

	require.NoError(t, err)
	require.Empty(t, collection.Results)
	require.Len(t, collection.Failures, 2)
	require.Equal(t, "response too large", collection.Failures[0].Message)
	require.Equal(t, "unexpected encoded response", collection.Failures[1].Message)
}

func TestCollectEnvironmentsRecoversAbortedHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if aggregateEnvironmentID(r) == "aborted" {
			panic(http.ErrAbortHandler)
		}
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	})

	collection, err := CollectEnvironments[aggregateRow](aggregateContext(t, handler, "aborted", "ok"), nil)

	require.NoError(t, err)
	require.Len(t, collection.Results, 1)
	require.Equal(t, "ok", collection.Results[0].Value.ID)
	require.Len(t, collection.Failures, 1)
	require.Equal(t, "aborted", collection.Failures[0].Environment.ID)
	require.Contains(t, collection.Failures[0].Message, http.ErrAbortHandler.Error())
}

func TestCollectEnvironmentDataKeepsBreakdown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch aggregateEnvironmentID(r) {
		case "a":
			_, _ = w.Write([]byte(`{"success":true,"data":2}`))
		case "b":
			_, _ = w.Write([]byte(`{"success":true,"data":3}`))
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	})

	body, err := CollectEnvironmentData(aggregateContext(t, handler, "a", "b", "c"), nil, func(environments []types.EnvironmentData[int]) int {
		total := 0
		for _, environment := range environments {
			total += environment.Data
		}
		return total
	})

	require.NoError(t, err)
	require.True(t, body.Success)
	require.Equal(t, 5, body.Data)
	require.Equal(t, []types.EnvironmentData[int]{
		{Environment: types.EnvironmentRef{ID: "a", Name: "Env a"}, Data: 2},
		{Environment: types.EnvironmentRef{ID: "b", Name: "Env b"}, Data: 3},
	}, body.Environments)
	require.Equal(t, []types.EnvironmentFailure{
		{Environment: types.EnvironmentRef{ID: "c", Name: "c"}, Message: "Bad Gateway"},
	}, body.FailedEnvironments)
}

func TestCollectEnvironmentDataRequiresScope(t *testing.T) {
	_, err := CollectEnvironmentData(t.Context(), nil, func([]types.EnvironmentData[int]) int {
		return 0
	})
	require.Error(t, err)
}
