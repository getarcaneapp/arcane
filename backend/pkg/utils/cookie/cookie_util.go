package cookie

import (
	"context"
	"net/http"
)

var (
	TokenCookieName         = "__Host-token" // #nosec G101: cookie name label, not a credential
	InsecureTokenCookieName = "token"        // #nosec G101: cookie name label, not a credential
	OidcStateCookieName     = "oidc_state"
)

type secureCookieContextKey struct{}

// WithSecureCookieContext records the router's trusted secure-cookie decision.
func WithSecureCookieContext(ctx context.Context, secure bool) context.Context {
	return context.WithValue(ctx, secureCookieContextKey{}, secure)
}

// SecureCookieFromContext returns the secure-cookie decision that router
// middleware derived from TLS or trusted proxy headers.
func SecureCookieFromContext(ctx context.Context) bool {
	secure, _ := ctx.Value(secureCookieContextKey{}).(bool)
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

func isSecureInternal(r *http.Request) bool {
	return SecureCookieFromRequest(r)
}

func ClearTokenCookie(w http.ResponseWriter, r *http.Request) {
	for _, cookieHeader := range BuildClearTokenCookieStringsFor(isSecureInternal(r)) {
		w.Header().Add("Set-Cookie", cookieHeader)
	}
}

func GetTokenCookie(r *http.Request) (string, error) {
	if c, err := r.Cookie(TokenCookieName); err == nil {
		return c.Value, nil
	}
	c, err := r.Cookie(InsecureTokenCookieName)
	if err != nil {
		return "", err
	}
	return c.Value, nil
}

// BuildTokenCookieStringFor builds a Set-Cookie header string matching the
// current request security context. Callers must pass the trusted secure flag
// from SecureCookieFromContext / SecureCookieFromRequest so the cookie name
// (__Host-token vs. token) round-trips correctly behind HTTPS reverse proxies.
func BuildTokenCookieStringFor(maxAgeInSeconds int, token string, secure bool) string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	cookieName := InsecureTokenCookieName
	if secure {
		cookieName = TokenCookieName
	}
	cookie := &http.Cookie{ // #nosec G124: Secure mirrors the trusted request context so the cookie can round-trip through HTTPS reverse proxies.
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAgeInSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildClearTokenCookieStringsFor builds Set-Cookie header strings to clear
// token cookies matching the current request security context. Secure contexts
// also clear the HTTP fallback cookie so stale sessions from older releases are
// flushed instead of being re-presented forever.
func BuildClearTokenCookieStringsFor(secure bool) []string {
	headers := []string{buildClearTokenCookieStringInternal(InsecureTokenCookieName, false)}
	if secure {
		headers = append(headers, buildClearTokenCookieStringInternal(TokenCookieName, true))
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
