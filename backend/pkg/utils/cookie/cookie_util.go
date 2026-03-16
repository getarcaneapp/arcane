package cookie

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	TokenCookieName         = "__Host-token" // #nosec G101: cookie name label, not a credential
	InsecureTokenCookieName = "token"        // #nosec G101: cookie name label, not a credential
	OidcStateCookieName     = "oidc_state"
)

func isSecure(c *gin.Context) bool {
	return c.Request.TLS != nil
}

func tokenCookieName(c *gin.Context) string {
	if isSecure(c) {
		return TokenCookieName
	}
	return InsecureTokenCookieName
}

func ClearTokenCookie(c *gin.Context) {
	name := tokenCookieName(c)
	c.SetCookie(name, "", -1, "/", "", isSecure(c), true)
}

func GetTokenCookie(c *gin.Context) (string, error) {
	// Try secure name first, then fallback to insecure
	if v, err := c.Cookie(TokenCookieName); err == nil {
		return v, nil
	}
	return c.Cookie(InsecureTokenCookieName)
}

// BuildTokenCookieString builds a Set-Cookie header string for Huma handlers.
// Uses the insecure cookie name since we can't detect TLS from context.
// For secure contexts, the middleware should handle the __Host- prefix.
func BuildTokenCookieString(maxAgeInSeconds int, token string) string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	cookie := &http.Cookie{
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
	cookie := &http.Cookie{
		Name:     InsecureTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

// BuildOidcStateCookieString builds a Set-Cookie header string for the OIDC state cookie.
func BuildOidcStateCookieString(value string, maxAgeInSeconds int, secure bool) string {
	if maxAgeInSeconds < 0 {
		maxAgeInSeconds = 0
	}
	cookie := &http.Cookie{
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
	cookie := &http.Cookie{
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
