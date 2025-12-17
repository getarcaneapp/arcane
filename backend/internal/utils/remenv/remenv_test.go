package remenv

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type chunkReader struct {
	chunks [][]byte
	idx    int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	c := r.chunks[r.idx]
	r.idx++
	return copy(p, c), nil
}

type flushResponseWriter struct {
	header  http.Header
	buf     bytes.Buffer
	status  int
	flushes int
}

func newFlushResponseWriter() *flushResponseWriter {
	return &flushResponseWriter{header: make(http.Header)}
}

func (w *flushResponseWriter) Header() http.Header { return w.header }

func (w *flushResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *flushResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

func (w *flushResponseWriter) Flush() { w.flushes++ }

type noFlushResponseWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newNoFlushResponseWriter() *noFlushResponseWriter {
	return &noFlushResponseWriter{header: make(http.Header)}
}

func (w *noFlushResponseWriter) Header() http.Header { return w.header }

func (w *noFlushResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *noFlushResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

func TestCopyRequestHeaders_SkipsExpectedHeaders(t *testing.T) {
	skip := GetSkipHeaders()
	from := http.Header{}
	from.Add("X-Test", "a")
	from.Add("X-Test", "b")
	from.Set(HeaderAuthorization, "Bearer should-not-copy")
	from.Set(HeaderAPIToken, "token-should-not-copy")
	from.Set("Host", "example.com")
	from.Set(HeaderCookie, "session=abc")
	from.Set("Transfer-Encoding", "chunked")

	to := http.Header{}
	CopyRequestHeaders(from, to, skip)

	require.Equal(t, []string{"a", "b"}, to.Values("X-Test"))
	require.Empty(t, to.Get(HeaderAuthorization))
	require.Empty(t, to.Get(HeaderAPIToken))
	require.Empty(t, to.Get("Host"))
	require.Empty(t, to.Get(HeaderCookie))
	require.Empty(t, to.Get("Transfer-Encoding"))
}

func TestSetAuthHeader_ForwardsAPITokenAndAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	c.Request.Header.Set(HeaderAPIToken, "api-token")
	c.Request.Header.Set(HeaderAuthorization, "Bearer auth")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://remote", nil)
	require.NoError(t, err)

	SetAuthHeader(req, c)
	require.Equal(t, "api-token", req.Header.Get(HeaderAPIToken))
	require.Equal(t, "Bearer auth", req.Header.Get(HeaderAuthorization))
}

func TestSetAuthHeader_UsesCookieTokenWhenNoAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	c.Request.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://remote", nil)
	require.NoError(t, err)

	SetAuthHeader(req, c)
	require.Equal(t, "Bearer cookie-token", req.Header.Get(HeaderAuthorization))
}

func TestSetAgentToken(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://remote", nil)
	require.NoError(t, err)

	SetAgentToken(req, nil)
	require.Empty(t, req.Header.Get(HeaderAgentToken))

	tok := "agent-token"
	SetAgentToken(req, &tok)
	require.Equal(t, tok, req.Header.Get(HeaderAgentToken))
}

func TestSetForwardedHeaders(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://remote", nil)
	require.NoError(t, err)

	SetForwardedHeaders(req, "1.2.3.4", "example.com")
	require.Equal(t, "1.2.3.4", req.Header.Get("X-Forwarded-For"))
	require.Equal(t, "example.com", req.Header.Get("X-Forwarded-Host"))
}

func TestGetHopByHopHeaders_IncludesStandardHeaders(t *testing.T) {
	h := GetHopByHopHeaders()
	_, ok := h[http.CanonicalHeaderKey("Connection")]
	require.True(t, ok)
	_, ok = h[http.CanonicalHeaderKey("Transfer-Encoding")]
	require.True(t, ok)
	_, ok = h[http.CanonicalHeaderKey("Upgrade")]
	require.True(t, ok)
}

func TestBuildHopByHopHeaders_AddsConnectionTokens(t *testing.T) {
	respHeader := http.Header{}
	respHeader.Add("Connection", "X-Foo, keep-alive")
	respHeader.Add("Connection", "x-bar")

	hop := BuildHopByHopHeaders(respHeader)
	require.Contains(t, hop, http.CanonicalHeaderKey("X-Foo"))
	require.Contains(t, hop, http.CanonicalHeaderKey("Keep-Alive"))
	require.Contains(t, hop, http.CanonicalHeaderKey("X-Bar"))
}

func TestCopyResponseHeaders_SkipsHopByHopAndConnectionNamedHeaders(t *testing.T) {
	from := http.Header{}
	from.Set("Content-Type", "application/json")
	from.Set("Transfer-Encoding", "chunked")
	from.Set("X-Foo", "bar")
	from.Add("Connection", "X-Foo")

	hop := BuildHopByHopHeaders(from)
	to := http.Header{}
	CopyResponseHeaders(from, to, hop)

	require.Equal(t, "application/json", to.Get("Content-Type"))
	require.Empty(t, to.Get("Transfer-Encoding"))
	require.Empty(t, to.Get("X-Foo"))
	require.Empty(t, to.Get("Connection"))
}

func TestGetSkipHeaders_ContainsExpectedEntries(t *testing.T) {
	skip := GetSkipHeaders()
	require.Contains(t, skip, "Host")
	require.Contains(t, skip, "Connection")
	require.Contains(t, skip, "Transfer-Encoding")
	require.Contains(t, skip, "Upgrade")
	require.Contains(t, skip, "Content-Length")
	require.Contains(t, skip, "Cookie")
	require.Contains(t, skip, "Origin")
	require.Contains(t, skip, "Referer")
}

func TestBuildWebSocketHeaders_UsesAuthorizationHeaderAndAddsAgentToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	c.Request.Header.Set(HeaderAPIKey, "api-key")
	c.Request.Header.Set(HeaderAuthorization, "Bearer auth")
	c.Request.Header.Set(HeaderCookie, "session=abc")

	agent := "agent-token"
	headers := BuildWebSocketHeaders(c, &agent)

	require.Equal(t, "api-key", headers.Get(HeaderAPIKey))
	require.Equal(t, "Bearer auth", headers.Get(HeaderAuthorization))
	require.Empty(t, headers.Get(HeaderCookie))
	require.Equal(t, agent, headers.Get(HeaderAgentToken))
}

func TestBuildWebSocketHeaders_UsesCookieTokenAsBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	c.Request.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})

	headers := BuildWebSocketHeaders(c, nil)
	require.Equal(t, "Bearer cookie-token", headers.Get(HeaderAuthorization))
}

func TestBuildWebSocketHeaders_ForwardsCookieHeaderWhenNoAuthPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	c.Request.Header.Set(HeaderCookie, "session=abc")

	headers := BuildWebSocketHeaders(c, nil)
	require.Equal(t, "session=abc", headers.Get(HeaderCookie))
}

func TestHTTPToWebSocketURL(t *testing.T) {
	require.Equal(t, "wss://example.com/path", HTTPToWebSocketURL("https://example.com/path"))
	require.Equal(t, "ws://example.com/path", HTTPToWebSocketURL("http://example.com/path"))
	require.Equal(t, "ws://already", HTTPToWebSocketURL("ws://already"))
}

func TestCopyBodyWithFlush_FlushesWhenSupported(t *testing.T) {
	w := newFlushResponseWriter()
	body := &chunkReader{chunks: [][]byte{[]byte("hello "), []byte("world")}}

	CopyBodyWithFlush(w, body)
	require.Equal(t, "hello world", w.buf.String())
	require.Equal(t, 2, w.flushes)
}

func TestCopyBodyWithFlush_DoesNotRequireFlusher(t *testing.T) {
	w := newNoFlushResponseWriter()
	body := &chunkReader{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}}

	CopyBodyWithFlush(w, body)
	require.Equal(t, "abc", w.buf.String())
}
