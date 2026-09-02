package cookie

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

var (
	TokenCookieName         = "__Host-token" // #nosec G101: cookie name label, not a credential
	InsecureTokenCookieName = "token"        // #nosec G101: cookie name label, not a credential
	OidcStateCookieName     = "oidc_state"
)

// Legacy ML-DSA browser tokens exceed the 4096-byte cookie limit, so readers
// retain numbered chunk support until existing sessions expire or refresh.
const (
	tokenCookieChunkSize = 3072
	tokenCookieMaxChunks = 4
)

func tokenCookieChunkNameInternal(base string, index int) string {
	if index == 0 {
		return base
	}
	return fmt.Sprintf("%s.%d", base, index)
}

// SecureCookieContextKey is the context key under which router middleware
// records its trusted secure-cookie decision (a bool derived from TLS or
// trusted proxy headers).
type SecureCookieContextKey struct{}

// SecureCookieFromContext returns the secure-cookie decision that router
// middleware derived from TLS or trusted proxy headers.
func SecureCookieFromContext(ctx context.Context) bool {
	secure, _ := ctx.Value(SecureCookieContextKey{}).(bool)
	return secure
}

// SecureCookieFromRequest returns true when the request was made over TLS or
// router middleware marked it as forwarded from HTTPS by a trusted proxy.
func SecureCookieFromRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return SecureCookieFromContext(r.Context())
}

func ClearTokenCookie(w http.ResponseWriter, r *http.Request) {
	for _, cookieHeader := range BuildClearTokenCookieStringsFor(SecureCookieFromRequest(r)) {
		w.Header().Add("Set-Cookie", cookieHeader)
	}
}

func GetTokenCookieFromHeader(cookieHeader string) (string, error) {
	return GetTokenCookie(&http.Request{Header: http.Header{"Cookie": []string{cookieHeader}}})
}

func GetTokenCookie(r *http.Request) (string, error) {
	if value, err := readTokenCookieInternal(r, TokenCookieName); err == nil {
		return value, nil
	}
	return readTokenCookieInternal(r, InsecureTokenCookieName)
}

func readTokenCookieInternal(r *http.Request, base string) (string, error) {
	byName := make(map[string]string, len(r.Cookies()))
	for _, c := range r.Cookies() {
		byName[c.Name] = c.Value
	}
	first, ok := byName[base]
	if !ok {
		return "", http.ErrNoCookie
	}
	var value strings.Builder
	value.WriteString(first)
	for i := 1; ; i++ {
		chunk, ok := byName[tokenCookieChunkNameInternal(base, i)]
		if !ok {
			break
		}
		value.WriteString(chunk)
	}
	return value.String(), nil
}

// BuildTokenCookieStringFor builds Set-Cookie header strings matching the
// current request security context. Compact browser tokens use one cookie;
// legacy oversized tokens are chunked for upgrade compatibility. Unused chunk
// slots are cleared so stale legacy chunks cannot corrupt reassembly.
// Callers must pass the trusted secure flag from SecureCookieFromContext /
// SecureCookieFromRequest so the cookie name (__Host-token vs. token)
// round-trips correctly behind HTTPS reverse proxies.
func BuildTokenCookieStringFor(maxAgeInSeconds int, token string, secure bool) []string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	base := InsecureTokenCookieName
	if secure {
		base = TokenCookieName
	}

	var chunks []string
	for len(token) > tokenCookieChunkSize {
		chunks = append(chunks, token[:tokenCookieChunkSize])
		token = token[tokenCookieChunkSize:]
	}
	chunks = append(chunks, token)

	var headers []string
	for i, chunk := range chunks {
		cookie := &http.Cookie{ // #nosec G124: Secure mirrors the trusted request context so the cookie can round-trip through HTTPS reverse proxies.
			Name:     tokenCookieChunkNameInternal(base, i),
			Value:    chunk,
			Path:     "/",
			MaxAge:   maxAgeInSeconds,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		}
		headers = append(headers, cookie.String())
	}
	for i := len(chunks); i < tokenCookieMaxChunks; i++ {
		headers = append(headers, buildClearTokenCookieStringInternal(tokenCookieChunkNameInternal(base, i), secure))
	}
	return headers
}

// BuildClearTokenCookieStringsFor builds Set-Cookie header strings to clear
// token cookies matching the current request security context. Secure contexts
// also clear the HTTP fallback cookie so stale sessions from older releases are
// flushed instead of being re-presented forever.
func BuildClearTokenCookieStringsFor(secure bool) []string {
	var headers []string
	for i := range tokenCookieMaxChunks {
		headers = append(headers, buildClearTokenCookieStringInternal(tokenCookieChunkNameInternal(InsecureTokenCookieName, i), false))
		if secure {
			headers = append(headers, buildClearTokenCookieStringInternal(tokenCookieChunkNameInternal(TokenCookieName, i), true))
		}
	}
	return headers
}

func buildClearTokenCookieStringInternal(name string, secure bool) string {
	cookie := &http.Cookie{ // #nosec G124: Secure mirrors the caller-provided TLS state so the clear directive matches whichever cookie variant (__Host-token vs. token) was originally set.
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildOidcStateCookieString builds a Set-Cookie header string for the OIDC state cookie.
func BuildOidcStateCookieString(value string, maxAgeInSeconds int, secure bool) string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	cookie := &http.Cookie{ // #nosec G124: secure is provided by the caller based on request context.
		Name:     OidcStateCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAgeInSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildClearOidcStateCookieString builds a Set-Cookie header string to clear the OIDC state cookie.
func BuildClearOidcStateCookieString(secure bool) string {
	cookie := &http.Cookie{ // #nosec G124: secure is provided by the caller based on request context.
		Name:     OidcStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}
