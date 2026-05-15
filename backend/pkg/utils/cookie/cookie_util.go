package cookie

import (
	"net/http"
)

var (
	TokenCookieName         = "__Host-token" // #nosec G101: cookie name label, not a credential
	InsecureTokenCookieName = "token"        // #nosec G101: cookie name label, not a credential
	OidcStateCookieName     = "oidc_state"
)

func isSecureInternal(r *http.Request) bool {
	return r.TLS != nil
}

func tokenCookieNameInternal(r *http.Request) string {
	if isSecureInternal(r) {
		return TokenCookieName
	}
	return InsecureTokenCookieName
}

func ClearTokenCookie(w http.ResponseWriter, r *http.Request) {
	name := tokenCookieNameInternal(r)
	http.SetCookie(w, &http.Cookie{ // #nosec G124: Secure mirrors the request's TLS state so the clear directive matches whichever cookie variant (__Host-token vs. token) was originally set.
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureInternal(r),
		SameSite: http.SameSiteLaxMode,
	})
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

// BuildTokenCookieString builds a Set-Cookie header string for Huma handlers.
// Uses the insecure cookie name since we can't detect TLS from context.
// For secure contexts, the middleware should handle the __Host- prefix.
func BuildTokenCookieString(maxAgeInSeconds int, token string) string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	cookie := &http.Cookie{ // #nosec G124: Huma handlers intentionally use the HTTP-compatible fallback token cookie here.
		Name:     InsecureTokenCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAgeInSeconds,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildClearTokenCookieString builds a Set-Cookie header string to clear the token cookie.
func BuildClearTokenCookieString() string {
	cookie := &http.Cookie{ // #nosec G124: clearing must target the same HTTP-compatible fallback token cookie.
		Name:     InsecureTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildClearTokenCookieStringFor builds a Set-Cookie header string to clear the
// token cookie variant matching the connection's TLS state — `__Host-token` for
// TLS, `token` otherwise — mirroring ClearTokenCookie for callers that have no
// http.ResponseWriter (e.g. Huma middleware).
func BuildClearTokenCookieStringFor(secure bool) string {
	name := InsecureTokenCookieName
	if secure {
		name = TokenCookieName
	}
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
