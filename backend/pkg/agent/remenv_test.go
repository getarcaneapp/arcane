package agent

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type (
	chunkReader struct {
		chunks [][]byte
		idx    int
	}

	flushResponseWriter struct {
		header  http.Header
		buf     bytes.Buffer
		status  int
		flushes int
	}

	noFlushResponseWriter struct {
		header http.Header
		buf    bytes.Buffer
		status int
	}
)

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	c := r.chunks[r.idx]
	r.idx++
	return copy(p, c), nil
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

func newNoFlushResponseWriter() *noFlushResponseWriter {
	return &noFlushResponseWriter{header: make(http.Header)}
}

func (w *noFlushResponseWriter) Header() http.Header { return w.header }

func (w *noFlushResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *noFlushResponseWriter) WriteHeader(statusCode int) { w.status = statusCode }

func TestCopyRequestHeaders_SkipsExpectedHeaders(t *testing.T) {
	to := http.Header{}
	CopyRequestHeaders(http.Header{
		"X-Test":            {"a", "b"},
		HeaderAuthorization: {"Bearer should-not-copy"},
		HeaderAPIKey:        {"token-should-not-copy"},
		"Host":              {"example.com"},
		HeaderCookie:        {"session=abc"},
		"Transfer-Encoding": {"chunked"},
	}, to)

	require.Equal(t, []string{"a", "b"}, to.Values("X-Test"))
	require.Empty(t, to.Get(HeaderAuthorization))
	require.Empty(t, to.Get(HeaderAPIKey))
	require.Empty(t, to.Get("Host"))
	require.Empty(t, to.Get(HeaderCookie))
	require.Empty(t, to.Get("Transfer-Encoding"))
}

func TestSetAgentToken(t *testing.T) {
	headers := http.Header{}

	SetAgentToken(headers, " ")
	require.Empty(t, headers.Get(HeaderAgentToken))
	require.Empty(t, headers.Get(HeaderAPIKey))

	SetAgentToken(headers, "agent-token")
	require.Equal(t, "agent-token", headers.Get(HeaderAgentToken))
	require.Equal(t, "agent-token", headers.Get(HeaderAPIKey))
	require.Empty(t, headers.Get(HeaderAuthorization))
}

func TestCopyResponseHeaders_SkipsConnectionTokens(t *testing.T) {
	to := http.Header{}
	CopyResponseHeaders(http.Header{
		"Connection": {"X-Foo, keep-alive", "x-bar"},
		"X-Foo":      {"1"},
		"Keep-Alive": {"timeout=5"},
		"X-Bar":      {"2"},
		"X-Baz":      {"3"},
	}, to)
	require.Equal(t, http.Header{"X-Baz": {"3"}}, to)
}

func TestCopyResponseHeaders_SkipsHopByHopAndConnectionNamedHeaders(t *testing.T) {
	from := http.Header{
		"Content-Type":      {"application/json"},
		"Transfer-Encoding": {"chunked"},
		"X-Foo":             {"bar"},
		"Connection":        {"X-Foo"},
	}

	to := http.Header{}
	CopyResponseHeaders(from, to)

	require.Equal(t, "application/json", to.Get("Content-Type"))
	require.Empty(t, to.Get("Transfer-Encoding"))
	require.Empty(t, to.Get("X-Foo"))
	require.Empty(t, to.Get("Connection"))
}

func TestHTTPToWebSocketURL(t *testing.T) {
	require.Equal(t, "wss://example.com/path", HTTPToWebSocketURL("https://example.com/path"))
	require.Equal(t, "ws://example.com/path", HTTPToWebSocketURL("http://example.com/path"))
	require.Equal(t, "ws://already", HTTPToWebSocketURL("ws://already"))
}

func TestCopyResponseBody_FlushesUnknownLength(t *testing.T) {
	w := newFlushResponseWriter()
	resp := &http.Response{
		ContentLength: -1,
		Header:        http.Header{},
		Body:          io.NopCloser(&chunkReader{chunks: [][]byte{[]byte("hello "), []byte("world")}}),
	}

	require.NoError(t, CopyResponseBody(w, resp))
	require.Equal(t, "hello world", w.buf.String())
	require.Equal(t, 2, w.flushes)
}

func TestCopyResponseBody_FlushesEventStream(t *testing.T) {
	w := newFlushResponseWriter()
	resp := &http.Response{
		ContentLength: 11,
		Header:        http.Header{"Content-Type": {"text/event-stream"}},
		Body:          io.NopCloser(&chunkReader{chunks: [][]byte{[]byte("hello "), []byte("world")}}),
	}

	require.NoError(t, CopyResponseBody(w, resp))
	require.Equal(t, 2, w.flushes)
}

func TestCopyResponseBody_SkipsFlushForKnownLength(t *testing.T) {
	w := newFlushResponseWriter()
	resp := &http.Response{
		ContentLength: 11,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(&chunkReader{chunks: [][]byte{[]byte("hello "), []byte("world")}}),
	}

	require.NoError(t, CopyResponseBody(w, resp))
	require.Equal(t, "hello world", w.buf.String())
	require.Zero(t, w.flushes)
}

func TestCopyResponseBody_DoesNotRequireFlusher(t *testing.T) {
	w := newNoFlushResponseWriter()
	resp := &http.Response{
		ContentLength: -1,
		Header:        http.Header{},
		Body:          io.NopCloser(&chunkReader{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}}),
	}

	require.NoError(t, CopyResponseBody(w, resp))
	require.Equal(t, "abc", w.buf.String())
}

func BenchmarkCopyResponseBody(b *testing.B) {
	payload := make([]byte, 64<<20)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_ = CopyResponseBody(w, &http.Response{
			ContentLength: int64(len(payload)),
			Header:        http.Header{},
			Body:          io.NopCloser(io.LimitReader(bytes.NewReader(payload), int64(len(payload)))),
		})
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	b.Cleanup(server.Close)
	client := server.Client()

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		resp, err := client.Get(server.URL)
		require.NoError(b, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(b, err)
		_ = resp.Body.Close()
	}
}
