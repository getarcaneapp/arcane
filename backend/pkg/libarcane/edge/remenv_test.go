package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	from.Set(HeaderAPIKey, "token-should-not-copy")
	from.Set("Host", "example.com")
	from.Set(HeaderCookie, "session=abc")
	from.Set("Transfer-Encoding", "chunked")

	to := http.Header{}
	CopyRequestHeaders(from, to, skip)

	require.Equal(t, []string{"a", "b"}, to.Values("X-Test"))
	require.Empty(t, to.Get(HeaderAuthorization))
	require.Empty(t, to.Get(HeaderAPIKey))
	require.Empty(t, to.Get("Host"))
	require.Empty(t, to.Get(HeaderCookie))
	require.Empty(t, to.Get("Transfer-Encoding"))
}

func TestSetAuthHeader_ForwardsAPIKeyAndAuthorization(t *testing.T) {
	e := echo.New()
	w := httptest.NewRecorder()
	req0 := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req0.Header.Set(HeaderAPIKey, "api-token")
	req0.Header.Set(HeaderAuthorization, "Bearer auth")
	c := e.NewContext(req0, w)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://remote", nil)
	require.NoError(t, err)

	SetAuthHeader(req, c)
	require.Equal(t, "api-token", req.Header.Get(HeaderAPIKey))
	require.Equal(t, "Bearer auth", req.Header.Get(HeaderAuthorization))
}

func TestSetAuthHeader_UsesCookieTokenWhenNoAuthorization(t *testing.T) {
	e := echo.New()
	w := httptest.NewRecorder()
	req0 := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req0.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})
	c := e.NewContext(req0, w)

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
	require.Empty(t, req.Header.Get(HeaderAPIKey))

	tok := "agent-token"
	SetAgentToken(req, &tok)
	require.Equal(t, tok, req.Header.Get(HeaderAgentToken))
	require.Equal(t, tok, req.Header.Get(HeaderAPIKey))
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
	require.Contains(t, skip, "Accept-Encoding")
	require.Contains(t, skip, "Cookie")
	require.Contains(t, skip, "Origin")
	require.Contains(t, skip, "Referer")
}

func TestBuildWebSocketHeaders_UsesAuthorizationHeaderAndAddsAgentToken(t *testing.T) {
	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set(HeaderAPIKey, "api-key")
	req.Header.Set(HeaderAuthorization, "Bearer auth")
	req.Header.Set(HeaderCookie, "session=abc")
	c := e.NewContext(req, w)

	agent := "agent-token"
	headers := BuildWebSocketHeaders(c, &agent)

	require.Equal(t, agent, headers.Get(HeaderAPIKey))
	require.Equal(t, "Bearer auth", headers.Get(HeaderAuthorization))
	require.Empty(t, headers.Get(HeaderCookie))
	require.Equal(t, agent, headers.Get(HeaderAgentToken))
}

func TestBuildWebSocketHeaders_UsesCookieTokenAsBearer(t *testing.T) {
	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "cookie-token"})
	c := e.NewContext(req, w)

	headers := BuildWebSocketHeaders(c, nil)
	require.Equal(t, "Bearer cookie-token", headers.Get(HeaderAuthorization))
}

func TestBuildWebSocketHeaders_ForwardsCookieHeaderWhenNoAuthPresent(t *testing.T) {
	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set(HeaderCookie, "session=abc")
	c := e.NewContext(req, w)

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

func newTestRemenvClientInternal(timeout time.Duration) *remenv.Client {
	return remenv.NewClient(&http.Client{Timeout: timeout}, remenv.TunnelTransportFuncs{
		EnsureAvailableFunc: func(ctx context.Context, envID string) error {
			if HasActiveTunnel(envID) {
				return nil
			}

			if _, ok := RequestTunnelAndWait(ctx, envID, DefaultTunnelDemandTTL, DefaultTunnelAcquireTimeout()); ok {
				return nil
			}

			return fmt.Errorf("edge agent is not connected (no active tunnel)")
		},
		DoFunc: func(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*remenv.Response, error) {
			tunnel, ok := GetRegistry().Get(envID)
			if !ok {
				return nil, fmt.Errorf("no active tunnel for environment %s", envID)
			}
			if tunnel.Conn.IsClosed() {
				return nil, fmt.Errorf("tunnel for environment %s is closed", envID)
			}

			statusCode, respHeaders, respBody, err := ProxyRequest(ctx, tunnel, method, path, "", headers, body)
			if err != nil {
				return nil, fmt.Errorf("tunnel request failed: %w", err)
			}

			return &remenv.Response{
				StatusCode: statusCode,
				Body:       respBody,
				Headers:    respHeaders,
			}, nil
		},
	})
}

func TestRemenvClient_EdgeWithTunnel(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusOK,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"edge":true}`),
		}
	})
	defer server.Close()
	defer func() { _ = tunnel.Close() }()

	envID := "env-edge-1"
	GetRegistry().Register(envID, tunnel)
	defer GetRegistry().Unregister(envID)

	client := newTestRemenvClientInternal(1 * time.Second)

	resp, err := client.Do(context.Background(), remenv.Request{
		EnvironmentID: envID,
		IsEdge:        true,
		Method:        http.MethodGet,
		URL:           "http://ignored/api/health",
		Path:          "/api/health",
		Headers:       map[string]string{"X-H": "v"},
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte(`{"edge":true}`), resp.Body)
}

func TestRemenvClient_EdgeNoTunnel(t *testing.T) {
	client := newTestRemenvClientInternal(1 * time.Second)

	_, err := client.Do(context.Background(), remenv.Request{
		EnvironmentID: "env-edge-missing",
		IsEdge:        true,
		Method:        http.MethodGet,
		URL:           "http://ignored/api/health",
		Path:          "/api/health",
	})

	var transportErr *remenv.TransportError
	require.ErrorAs(t, err, &transportErr)
	assert.Contains(t, err.Error(), "not connected")
}

func TestRemenvClient_EdgeWithGRPCTunnel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-edge-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	clientAPI := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := clientAPI.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			agentErrCh <- err
			return
		}

		req := msg.GetCommandRequest()
		if req == nil {
			agentErrCh <- errors.New("expected command request")
			return
		}

		if req.GetMethod() != http.MethodPost {
			agentErrCh <- errors.New("unexpected method")
			return
		}
		if req.GetPath() != "/api/environments/0/projects" {
			agentErrCh <- errors.New("unexpected path")
			return
		}
		if req.GetHeaders()["X-H"] != "v" {
			agentErrCh <- errors.New("missing X-H header")
			return
		}
		if req.GetHeaders()["Content-Type"] != "application/json" {
			agentErrCh <- errors.New("missing content type")
			return
		}
		if string(req.GetBody()) != `{"edge":true}` {
			agentErrCh <- errors.New("unexpected request body")
			return
		}

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: req.GetCommandId(),
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: req.GetCommandId(),
			Status:    http.StatusCreated,
			Headers:   map[string]string{"Content-Type": "application/json"},
			Body:      []byte(`{"ok":true}`),
		}}})
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, time.Second, 10*time.Millisecond)

	client := newTestRemenvClientInternal(1 * time.Second)
	resp, err := client.Do(ctx, remenv.Request{
		EnvironmentID: envID,
		IsEdge:        true,
		Method:        http.MethodPost,
		URL:           "http://ignored/api/environments/0/projects",
		Path:          "/api/environments/0/projects",
		Headers:       map[string]string{"X-H": "v", "Content-Type": "application/json"},
		Body:          []byte(`{"edge":true}`),
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, []byte(`{"ok":true}`), resp.Body)

	require.NoError(t, <-agentErrCh)
}
