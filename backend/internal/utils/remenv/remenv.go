package remenv

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const headerApiToken = "X-Api-Token" // #nosec G101: header name, not a credential

func CopyRequestHeaders(from http.Header, to http.Header, skip map[string]struct{}) {
	for k, vs := range from {
		ck := http.CanonicalHeaderKey(k)
		if _, ok := skip[ck]; ok || ck == "Authorization" || ck == http.CanonicalHeaderKey(headerApiToken) {
			continue
		}
		for _, v := range vs {
			to.Add(k, v)
		}
	}
}

func SetAuthHeader(req *http.Request, c *gin.Context) {
	// Forward API key header if present
	if apiKey := c.GetHeader(headerApiToken); apiKey != "" {
		req.Header.Set(headerApiToken, apiKey)
	}

	// Forward Authorization header or cookie token
	if auth := c.GetHeader("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	} else if cookieToken, err := c.Cookie("token"); err == nil && cookieToken != "" {
		req.Header.Set("Authorization", "Bearer "+cookieToken)
	}
}

func SetAgentToken(req *http.Request, accessToken *string) {
	if accessToken != nil && *accessToken != "" {
		req.Header.Set("X-Arcane-Agent-Token", *accessToken)
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
