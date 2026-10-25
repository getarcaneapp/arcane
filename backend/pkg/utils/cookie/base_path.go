package cookie

import (
	"strings"
	"sync/atomic"
)

// configuredBasePath holds the URL prefix Arcane is served under, mirrored from
// config.Config.BasePath at startup so the cookie builders can scope session cookies to it.
var configuredBasePath atomic.Pointer[string]

// SetBasePath records the base path session cookies are scoped to. It is called once during
// startup, before any request is served.
func SetBasePath(basePath string) {
	configuredBasePath.Store(&basePath)
}

// BasePath returns the base path Arcane is served under, as recorded by SetBasePath.
func BasePath() string {
	if p := configuredBasePath.Load(); p != nil {
		return *p
	}
	return ""
}

// CookiePath returns the Path attribute for session cookies: "/" for a deployment served at
// the root of its host, the base path prefixed otherwise.
func CookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath + "/"
}

// RefreshCookiePathFor returns the Path attribute for the refresh cookie, which stays scoped
// to the auth endpoints that rotate it.
func RefreshCookiePathFor(basePath string) string {
	return CookiePath(basePath) + strings.TrimPrefix(RefreshCookiePath, "/")
}

// HostCookieNamePrefix is the RFC 6265 cookie name prefix that pins a cookie to Path="/" and
// to the host that set it. Such cookies cannot be scoped to a base path, so a prefixed
// deployment uses the unprefixed names instead and keeps the protection the session cookies
// rely on through their Path, Secure and HttpOnly attributes.
const HostCookieNamePrefix = "__Host-"

// SecureCookieName returns name unchanged for a deployment served at the root of its host, or
// name without its "__Host-" prefix when the cookie has to be scoped to a base path.
func SecureCookieName(name, basePath string) string {
	if basePath == "" {
		return name
	}
	return strings.TrimPrefix(name, HostCookieNamePrefix)
}
