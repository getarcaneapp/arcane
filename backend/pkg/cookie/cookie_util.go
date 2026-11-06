package cookie

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"kit.getarcane.app"
)

type (
	SecureCookieContextKey struct{}
)

// Legacy ML-DSA browser tokens exceed the 4096-byte cookie limit, so readers
// retain numbered chunk support until existing sessions expire or refresh.
const (
	tokenCookieChunkSize = 3072
	tokenCookieMaxChunks = 4
)

var (
	TokenCookieName           = "__Host-token" // #nosec G101: cookie name label, not a credential
	InsecureTokenCookieName   = "token"        // #nosec G101: cookie name label, not a credential
	OidcStateCookieName       = "oidc_state"
	RefreshCookieName         = "__Secure-refresh"
	InsecureRefreshCookieName = "refresh"
	RefreshCookiePath         = "/api/auth"
)

func tokenCookieChunkName(base string, index int) string {
	if index == 0 {
		return base
	}
	return fmt.Sprintf("%s.%d", base, index)
}

func SecureCookieFromContext(ctx context.Context) bool {
	secure, _ := ctx.Value(SecureCookieContextKey{}).(bool)
	return secure
}

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

func BuildRefreshCookieStringFor(maxAgeInSeconds int, value string, secure bool) string {
	return buildCookieString(kit.Ternary(secure, RefreshCookieName, InsecureRefreshCookieName), RefreshCookiePath, value, max(maxAgeInSeconds, 0), secure, http.SameSiteStrictMode)
}

func BuildClearRefreshCookieStringsFor(secure bool) []string {
	headers := []string{buildCookieString(InsecureRefreshCookieName, RefreshCookiePath, "", -1, false, http.SameSiteStrictMode)}
	if secure {
		headers = append(headers, buildCookieString(RefreshCookieName, RefreshCookiePath, "", -1, true, http.SameSiteStrictMode))
	}
	return headers
}

func GetTokenCookie(r *http.Request) (string, error) {
	if value, err := readTokenCookie(r, TokenCookieName); err == nil {
		return value, nil
	}
	return readTokenCookie(r, InsecureTokenCookieName)
}

func readTokenCookie(r *http.Request, base string) (string, error) {
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
		chunk, localOk := byName[tokenCookieChunkName(base, i)]
		if !localOk {
			break
		}
		value.WriteString(chunk)
	}
	return value.String(), nil
}

func BuildTokenCookieStringFor(maxAgeInSeconds int, token string, secure bool) []string {
	base := kit.Ternary(secure, TokenCookieName, InsecureTokenCookieName)

	var chunks []string
	for len(token) > tokenCookieChunkSize {
		chunks = append(chunks, token[:tokenCookieChunkSize])
		token = token[tokenCookieChunkSize:]
	}
	chunks = append(chunks, token)

	var headers []string
	for i, chunk := range chunks {
		headers = append(headers, buildCookieString(tokenCookieChunkName(base, i), "/", chunk, max(maxAgeInSeconds, 0), secure, http.SameSiteLaxMode))
	}
	for i := len(chunks); i < tokenCookieMaxChunks; i++ {
		headers = append(headers, buildCookieString(tokenCookieChunkName(base, i), "/", "", -1, secure, http.SameSiteLaxMode))
	}
	return headers
}

func BuildClearTokenCookieStringsFor(secure bool) []string {
	var headers []string
	for i := range tokenCookieMaxChunks {
		headers = append(headers, buildCookieString(tokenCookieChunkName(InsecureTokenCookieName, i), "/", "", -1, false, http.SameSiteLaxMode))
		if secure {
			headers = append(headers, buildCookieString(tokenCookieChunkName(TokenCookieName, i), "/", "", -1, true, http.SameSiteLaxMode))
		}
	}
	return headers
}

func buildCookieString(name, path, value string, maxAge int, secure bool, sameSite http.SameSite) string {
	cookie := &http.Cookie{ // #nosec G124: Secure mirrors the caller-provided TLS state so the clear directive matches whichever cookie variant (__Host-token vs. token) was originally set.
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
	return cookie.String()
}

func BuildOidcStateCookieString(value string, maxAgeInSeconds int, secure bool) string {
	return buildCookieString(OidcStateCookieName, "/", value, max(maxAgeInSeconds, 0), secure, http.SameSiteLaxMode)
}

func BuildClearOidcStateCookieString(secure bool) string {
	return buildCookieString(OidcStateCookieName, "/", "", -1, secure, http.SameSiteLaxMode)
}
