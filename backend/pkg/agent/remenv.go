package agent

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
)

const (
	HeaderAPIKey           = "X-Api-Key" // #nosec G101: header name, not a credential
	HeaderAuthorization    = "Authorization"
	HeaderCookie           = "Cookie"
	HeaderAgentToken       = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderEnrollmentToken  = "X-Arcane-Enrollment-Token"
	HeaderUpgrade          = "Upgrade"
	HeaderConnection       = "Connection"
	ConnectionUpgradeToken = "upgrade"
	bearerScheme           = "Bearer "
)

var (
	// hopByHopHeaders is shared and read-only; CopyResponseHeaders extends a private copy.
	hopByHopHeaders = map[string]struct{}{
		"Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {}, "Proxy-Authorization": {},
		"Te": {}, "Trailers": {}, "Trailer": {}, "Transfer-Encoding": {}, "Upgrade": {},
	}
	skipHeaders = map[string]struct{}{
		"Host": {}, "Connection": {}, "Keep-Alive": {}, "Proxy-Authenticate": {},
		"Proxy-Authorization": {}, "Te": {}, "Trailer": {}, "Transfer-Encoding": {},
		"Upgrade": {}, "Content-Length": {}, "Accept-Encoding": {}, "Origin": {}, "Referer": {},
		"Access-Control-Request-Method": {}, "Access-Control-Request-Headers": {}, "Cookie": {},
		HeaderAuthorization: {}, HeaderAPIKey: {},
	}
	copyBufferPool = sync.Pool{
		New: func() any {
			return new(make([]byte, 64<<10))
		},
	}
)

func CopyRequestHeaders(from, to http.Header) {
	for k, vs := range from {
		ck := http.CanonicalHeaderKey(k)
		if _, ok := skipHeaders[ck]; ok {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}

// SetAgentToken sets the agent token in both X- header forms.
func SetAgentToken(headers http.Header, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	headers.Set(HeaderAgentToken, token)
	headers.Set(HeaderAPIKey, token)
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

// CopyResponseBody streams resp's body to w, flushing after each write for event streams and responses of unknown
// length. It returns the first error other than io.EOF, so a response cut short upstream is not mistaken for a complete one.
func CopyResponseBody(w http.ResponseWriter, resp *http.Response) error {
	buf := copyBufferPool.Get().(*[]byte)
	defer copyBufferPool.Put(buf)
	flusher, canFlush := w.(http.Flusher)
	flushEachWrite := canFlush && (resp.ContentLength < 0 || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"))

	for {
		n, readErr := resp.Body.Read(*buf)
		if n > 0 {
			if _, writeErr := w.Write((*buf)[:n]); writeErr != nil {
				return writeErr
			}
			if flushEachWrite {
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

func CopyResponseHeaders(from, to http.Header) {
	hop := maps.Clone(hopByHopHeaders)
	for _, connVal := range from.Values("Connection") {
		for token := range strings.SplitSeq(connVal, ",") {
			if t := strings.TrimSpace(token); t != "" {
				hop[http.CanonicalHeaderKey(t)] = struct{}{}
			}
		}
	}

	for k, vs := range from {
		if _, ok := hop[http.CanonicalHeaderKey(k)]; ok {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}
