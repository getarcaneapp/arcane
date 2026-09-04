package httpx

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeBaseURL validates an outbound HTTP URL and strips endpoint-specific components.
func NormalizeBaseURL(rawURL string) (string, error) {
	parsed, err := ValidateOutboundHTTPURL(rawURL)
	if err != nil {
		return "", err
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// ManagerBaseURL returns the base URL of the manager application.
// It strips any trailing slashes or /api suffix from MANAGER_API_URL.
func ManagerBaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	managerURL := strings.TrimRight(rawURL, "/")
	managerURL = strings.TrimSuffix(managerURL, "/api")
	return managerURL
}

// ManagerGRPCAddr returns the manager gRPC address in host:port form.
func ManagerGRPCAddr(rawURL string) string {
	baseURL := ManagerBaseURL(rawURL)
	if baseURL == "" {
		return ""
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	host := parsed.Hostname()
	if host == "" {
		return ""
	}

	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
}
