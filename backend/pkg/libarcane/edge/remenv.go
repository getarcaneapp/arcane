package edge

import (
	"io"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/pkg/remenv"
	"github.com/labstack/echo/v4"
)

const (
	HeaderAPIKey        = "X-API-Key" // #nosec G101: header name, not a credential
	HeaderAuthorization = "Authorization"
	HeaderCookie        = "Cookie"
	HeaderAgentToken    = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderUpgrade       = "Upgrade"
	HeaderConnection    = "Connection"

	ConnectionUpgradeToken = "upgrade"
)

func CopyRequestHeaders(from http.Header, to http.Header, skip map[string]struct{}) {
	for k, vs := range from {
		ck := http.CanonicalHeaderKey(k)
		if _, ok := skip[ck]; ok || ck == http.CanonicalHeaderKey(HeaderAuthorization) || ck == http.CanonicalHeaderKey(HeaderAPIKey) {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}

func SetAuthHeader(req *http.Request, c echo.Context) {
	// Forward API key header if present
	if apiKey := c.Request().Header.Get(HeaderAPIKey); apiKey != "" {
		req.Header.Set(HeaderAPIKey, apiKey)
	}

	// Forward Authorization header or cookie token
	if auth := c.Request().Header.Get(HeaderAuthorization); auth != "" {
		req.Header.Set(HeaderAuthorization, auth)
	} else if ck, err := c.Cookie("token"); err == nil && ck != nil && ck.Value != "" {
		req.Header.Set(HeaderAuthorization, "Bearer "+ck.Value)
	}
}

func SetAgentToken(req *http.Request, accessToken *string) {
	remenv.ApplyAgentTokenHeaders(req.Header, accessToken)
}

// BuildWebSocketHeaders constructs a header set for proxying WebSocket requests
// to a remote environment, forwarding authentication in the same way as HTTP proxying.
func BuildWebSocketHeaders(c echo.Context, accessToken *string) http.Header {
	headers := http.Header{}
	req := c.Request()

	// Forward API key if present
	if apiKey := req.Header.Get(HeaderAPIKey); apiKey != "" {
		headers.Set(HeaderAPIKey, apiKey)
	}

	// Forward authorization (header or cookie)
	if auth := req.Header.Get(HeaderAuthorization); auth != "" {
		headers.Set(HeaderAuthorization, auth)
	} else if ck, err := c.Cookie("token"); err == nil && ck != nil && ck.Value != "" {
		headers.Set(HeaderAuthorization, "Bearer "+ck.Value)
	}

	// Forward cookies if no other auth is present
	if headers.Get(HeaderAuthorization) == "" && headers.Get(HeaderAPIKey) == "" {
		if cookies := req.Header.Get(HeaderCookie); cookies != "" {
			headers.Set(HeaderCookie, cookies)
		}
	}

	// Set agent token for remote environment authentication
	remenv.ApplyAgentTokenHeaders(headers, accessToken)

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

// CopyBodyWithFlush streams bytes from body to w, flushing when supported.
// Useful for progress/streaming endpoints where incremental delivery matters.
func CopyBodyWithFlush(w http.ResponseWriter, body io.Reader) {
	buf := make([]byte, 32*1024)
	flusher, canFlush := w.(http.Flusher)

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func SetForwardedHeaders(req *http.Request, clientIP, host string) {
	req.Header.Set("X-Forwarded-For", clientIP)
	req.Header.Set("X-Forwarded-Host", host)
}

func GetHopByHopHeaders() map[string]struct{} {
	return map[string]struct{}{
		http.CanonicalHeaderKey("Connection"):          {},
		http.CanonicalHeaderKey("Keep-Alive"):          {},
		http.CanonicalHeaderKey("Proxy-Authenticate"):  {},
		http.CanonicalHeaderKey("Proxy-Authorization"): {},
		http.CanonicalHeaderKey("TE"):                  {},
		http.CanonicalHeaderKey("Trailers"):            {},
		http.CanonicalHeaderKey("Trailer"):             {},
		http.CanonicalHeaderKey("Transfer-Encoding"):   {},
		http.CanonicalHeaderKey("Upgrade"):             {},
	}
}

func BuildHopByHopHeaders(respHeader http.Header) map[string]struct{} {
	hop := GetHopByHopHeaders()

	for _, connVal := range respHeader.Values("Connection") {
		for token := range strings.SplitSeq(connVal, ",") {
			if t := strings.TrimSpace(token); t != "" {
				hop[http.CanonicalHeaderKey(t)] = struct{}{}
			}
		}
	}

	return hop
}

func CopyResponseHeaders(from http.Header, to http.Header, hop map[string]struct{}) {
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

func GetSkipHeaders() map[string]struct{} {
	return map[string]struct{}{
		"Host": {}, "Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {},
		"Proxy-Authorization": {}, "Te": {}, "Trailer": {}, "Transfer-Encoding": {},
		"Upgrade": {}, "Content-Length": {}, "Accept-Encoding": {}, "Origin": {}, "Referer": {},
		"Access-Control-Request-Method": {}, "Access-Control-Request-Headers": {}, "Cookie": {},
	}
}
