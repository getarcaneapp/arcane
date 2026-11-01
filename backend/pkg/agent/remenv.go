package agent

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/pkg/cookie"
)

const (
	HeaderAPIKey           = "X-Api-Key" // #nosec G101: header name, not a credential
	HeaderAuthorization    = "Authorization"
	HeaderCookie           = "Cookie"
	HeaderAgentToken       = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderUpgrade          = "Upgrade"
	HeaderConnection       = "Connection"
	ConnectionUpgradeToken = "upgrade"
	bearerScheme           = "Bearer "
)

var (
	// hopByHopHeaders is shared and read-only; BuildHopByHopHeaders returns a private copy.
	hopByHopHeaders = map[string]struct{}{
		"Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {}, "Proxy-Authorization": {},
		"Te": {}, "Trailers": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	}
	skipHeaders = map[string]struct{}{
		"Host": {}, "Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {},
		"Proxy-Authorization": {}, "Te": {}, "Trailer": {}, "Transfer-Encoding": {},
		"Upgrade": {}, "Content-Length": {}, "Accept-Encoding": {}, "Origin": {}, "Referer": {},
		"Access-Control-Request-Method": {}, "Access-Control-Request-Headers": {}, "Cookie": {},
	}
)

func CopyRequestHeaders(from, to http.Header) {
	for k, vs := range from {
		ck := http.CanonicalHeaderKey(k)
		if _, ok := skipHeaders[ck]; ok || ck == http.CanonicalHeaderKey(HeaderAuthorization) || ck == http.CanonicalHeaderKey(HeaderAPIKey) {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}

func SetAuthHeader(headers http.Header, c *echo.Context) {
	// Forward API key header if present
	if apiKey := c.Request().Header.Get(HeaderAPIKey); apiKey != "" {
		headers.Set(HeaderAPIKey, apiKey)
	}

	// Forward Authorization header or cookie token
	if auth := c.Request().Header.Get(HeaderAuthorization); auth != "" {
		headers.Set(HeaderAuthorization, auth)
	} else if token, err := cookie.GetTokenCookie(c.Request()); err == nil {
		SetBearer(headers, token)
	}
}

func SetBearer(headers http.Header, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	headers.Set(HeaderAuthorization, bearerScheme+token)
}

// SetAgentToken sets the agent token in both X- header forms. It never sets
// Authorization, which carries the proxied user's own token.
func SetAgentToken(headers http.Header, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	headers.Set(HeaderAgentToken, token)
	headers.Set(HeaderAPIKey, token)
}

// BuildWebSocketHeaders constructs a header set for proxying WebSocket requests
// to a remote environment, forwarding authentication in the same way as HTTP proxying.
func BuildWebSocketHeaders(c *echo.Context, accessToken *string) http.Header {
	headers := http.Header{}
	SetAuthHeader(headers, c)

	// Forward cookies if no other auth is present
	if headers.Get(HeaderAuthorization) == "" && headers.Get(HeaderAPIKey) == "" {
		if cookies := c.Request().Header.Get(HeaderCookie); cookies != "" {
			headers.Set(HeaderCookie, cookies)
		}
	}

	// Set agent token for remote environment authentication
	SetAgentToken(headers, kit.FromPtr(accessToken))

	return headers
}

// HTTPToWebSocketURL converts an HTTP(S) URL to WS(S).
func HTTPToWebSocketURL(url string) string {
	switch {
	case strings.HasPrefix(url, "https://"):
		return "wss://" + strings.TrimPrefix(url, "https://")
	case strings.HasPrefix(url, "http://"):
		return "ws://" + strings.TrimPrefix(url, "http://")
	default:
		return url
	}
}

// CopyBodyWithFlush streams body to w, flushing after each write when supported. It returns the first error
// other than io.EOF, so a response cut short upstream is not mistaken for a complete one.
func CopyBodyWithFlush(w http.ResponseWriter, body io.Reader) error {
	buf := make([]byte, 32*1024)
	flusher, canFlush := w.(http.Flusher)

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func BuildHopByHopHeaders(respHeader http.Header) map[string]struct{} {
	hop := maps.Clone(hopByHopHeaders)
	for _, connVal := range respHeader.Values("Connection") {
		for token := range strings.SplitSeq(connVal, ",") {
			if t := strings.TrimSpace(token); t != "" {
				hop[http.CanonicalHeaderKey(t)] = struct{}{}
			}
		}
	}

	return hop
}

func CopyResponseHeaders(from, to http.Header, hop map[string]struct{}) {
	for k, vs := range from {
		ck := http.CanonicalHeaderKey(k)
		if _, ok := hop[ck]; ok {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}
