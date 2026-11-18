package secretapi

import (
	"context"
	"encoding/json/jsontext"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBaseURL(t *testing.T) {
	parsed, err := ParseBaseURL(" http://openbao:8200/ui/?x=1#y ", "the address", "http://x")
	require.NoError(t, err)
	assert.Equal(t, "http://openbao:8200/ui", parsed.String())

	for _, bad := range []string{"", "ftp://x", "http://", "http://user:pw@host"} {
		_, err := ParseBaseURL(bad, "the address", "http://x")
		assert.Error(t, err, bad)
	}
}

func TestErrorMessagesAreShortAndParsed(t *testing.T) {
	cases := map[string]string{
		`{"errors":["permission denied"]}`:               "permission denied",
		`{"messages":["Invalid token"],"success":false}`: "Invalid token",
		`{"message":"not found"}`:                        "not found",
		`{"error":{"message":"bad"}}`:                    "bad",
		`<html>proxy error</html>`:                       "",
		strings.Repeat("x", 1000):                        strings.Repeat("x", 300),
	}
	for body, want := range cases {
		assert.Equal(t, want, errorMessageInternal([]byte(body)), body)
	}
}

func TestDoReportsStatusAndAuthorizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"no"}`))
			return
		}
		assert.Equal(t, "/base/a%20b/c", r.URL.EscapedPath())
		assert.Equal(t, "1", r.URL.Query().Get("q"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	base, err := ParseBaseURL(server.URL+"/base", "x", "x")
	require.NoError(t, err)

	api := NewRequester(server.Client(), base, "Test")
	err = api.Do(t.Context(), http.MethodGet, "/a%20b/c", map[string][]string{"q": {"1"}}, nil, nil)
	assert.True(t, IsUnauthorized(err))
	assert.Contains(t, err.Error(), "Test returned HTTP 401: no")

	api.Authorize = func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer t")
		return nil
	}
	var out struct {
		OK bool `json:"ok"`
	}
	require.NoError(t, api.Do(t.Context(), http.MethodGet, "/a%20b/c", map[string][]string{"q": {"1"}}, nil, &out))
	assert.True(t, out.OK)
}

func TestStringValues(t *testing.T) {
	values, skipped := StringValues(map[string]jsontext.Value{
		"S": jsontext.Value(`"text"`),
		"N": jsontext.Value(`5432`),
		"B": jsontext.Value(`true`),
		"O": jsontext.Value(`{"a":1}`),
		"A": jsontext.Value(`[1]`),
		"Z": jsontext.Value(`null`),
	})
	assert.Equal(t, map[string]string{"S": "text", "N": "5432", "B": "true"}, values)
	assert.ElementsMatch(t, []string{"O", "A", "Z"}, skipped)
}

func TestPathEscapeSegments(t *testing.T) {
	assert.Equal(t, "apps/my%20app/db", PathEscapeSegments("/apps/my app/db/"))
}
