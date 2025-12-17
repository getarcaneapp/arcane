package remenv

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	HeaderAPIKey        = "X-Api-Key"   // #nosec G101: header name, not a credential
	HeaderAPIToken      = "X-Api-Token" // #nosec G101: header name, not a credential
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
		if _, ok := skip[ck]; ok || ck == http.CanonicalHeaderKey(HeaderAuthorization) || ck == http.CanonicalHeaderKey(HeaderAPIToken) {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}

func SetAuthHeader(req *http.Request, c *gin.Context) {
	// Forward API key header if present
	if apiKey := c.GetHeader(HeaderAPIToken); apiKey != "" {
		req.Header.Set(HeaderAPIToken, apiKey)
	}

	// Forward Authorization header or cookie token
	if auth := c.GetHeader(HeaderAuthorization); auth != "" {
		req.Header.Set(HeaderAuthorization, auth)
	} else if cookieToken, err := c.Cookie("token"); err == nil && cookieToken != "" {
		req.Header.Set(HeaderAuthorization, "Bearer "+cookieToken)
	}
}

func SetAgentToken(req *http.Request, accessToken *string) {
	if accessToken != nil && *accessToken != "" {
		req.Header.Set(HeaderAgentToken, *accessToken)
	}
}

// BuildWebSocketHeaders constructs a header set for proxying WebSocket requests
// to a remote environment, forwarding authentication in the same way as HTTP proxying.
func BuildWebSocketHeaders(c *gin.Context, accessToken *string) http.Header {
	headers := http.Header{}

	// Forward API key if present
	if apiKey := c.GetHeader(HeaderAPIKey); apiKey != "" {
		headers.Set(HeaderAPIKey, apiKey)
	}

	// Forward authorization (header or cookie)
	if auth := c.GetHeader(HeaderAuthorization); auth != "" {
		headers.Set(HeaderAuthorization, auth)
	} else if token, err := c.Cookie("token"); err == nil && token != "" {
		headers.Set(HeaderAuthorization, "Bearer "+token)
	}

	// Forward cookies if no other auth is present
	if headers.Get(HeaderAuthorization) == "" && headers.Get(HeaderAPIToken) == "" {
		if cookies := c.Request.Header.Get(HeaderCookie); cookies != "" {
			headers.Set(HeaderCookie, cookies)
		}
	}

	// Set agent token for remote environment authentication
	if accessToken != nil && *accessToken != "" {
		headers.Set(HeaderAgentToken, *accessToken)
	}

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
		for _, token := range strings.Split(connVal, ",") {
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
		"Upgrade": {}, "Content-Length": {}, "Origin": {}, "Referer": {},
		"Access-Control-Request-Method": {}, "Access-Control-Request-Headers": {}, "Cookie": {},
	}
}
