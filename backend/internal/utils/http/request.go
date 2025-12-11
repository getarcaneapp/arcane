package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ValidateWebSocketOrigin validates the Origin header for WebSocket connections
// to prevent CSRF attacks. It checks:
// 1. Same-origin requests (Origin matches Host)
// 2. Allowed origins from appURL
// 3. Handles empty Origin headers (some clients don't send it)
func ValidateWebSocketOrigin(appURL string) func(r *http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		if origin == "" {
			return true
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}

		if originURL.Host == r.Host {
			return true
		}

		appURLParsed, err := url.Parse(appURL)
		if err != nil {
			return false
		}
		if originURL.Host == appURLParsed.Host {
			return true
		}

		if isLocalhost(originURL.Host) && isLocalhost(r.Host) {
			return true
		}

		return false
	}
}

func isLocalhost(host string) bool {
	hostOnly := host
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		hostOnly = host[:idx]
	}

	return hostOnly == "localhost" ||
		hostOnly == "127.0.0.1" ||
		hostOnly == "::1" ||
		strings.HasPrefix(hostOnly, "127.") ||
		hostOnly == "[::1]"
}

// GetQueryParam reads a string query parameter from the Gin context.
// If `required` is true and the parameter is missing or empty, an error is returned.
func GetQueryParam(c *gin.Context, name string, required bool) (string, error) {
	v, ok := c.GetQuery(name)
	if !ok || v == "" {
		if required {
			return "", fmt.Errorf("missing query parameter %s", name)
		}
		return "", nil
	}
	return v, nil
}

// GetIntQueryParam reads and parses an integer query parameter from the Gin context.
// If `required` is true and the parameter is missing, or if parsing fails, an error is returned.
func GetIntQueryParam(c *gin.Context, name string, required bool) (int, error) {
	v, ok := c.GetQuery(name)
	if !ok || v == "" {
		if required {
			return 0, fmt.Errorf("missing numeric query parameter %s", name)
		}
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric query parameter %s: %w", name, err)
	}
	return n, nil
}

// GetClientBaseURL determines the client's base URL from request headers.
// It checks Origin, X-Forwarded-Host/Proto, and Host headers.
// If none provide a valid URL, it falls back to the configured appURL.
func GetClientBaseURL(origin, forwardedHost, forwardedProto, host, appURL string) string {
	// 1. Trust Origin if present
	if origin != "" {
		return strings.TrimSuffix(origin, "/")
	}

	scheme := "http"
	// Try to get scheme from AppURL as default
	if u, err := url.Parse(appURL); err == nil && u.Scheme != "" {
		scheme = u.Scheme
	}

	if forwardedProto != "" {
		scheme = forwardedProto
	}

	// 2. Check X-Forwarded-Host
	if forwardedHost != "" {
		return fmt.Sprintf("%s://%s", scheme, forwardedHost)
	}

	// 3. Check Host
	if host != "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}

	// 4. Fallback
	return strings.TrimSuffix(appURL, "/")
}
