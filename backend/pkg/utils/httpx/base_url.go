package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	kit "go.getarcane.app/kit/pkg"
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
		port = kit.Ternary(strings.EqualFold(parsed.Scheme, "https"), "443", "80")
	}

	return net.JoinHostPort(host, port)
}

// ValidateOutboundHTTPURL parses and validates an outbound HTTP(S) target URL.
// It intentionally performs syntactic hardening (scheme/host/credentials)
// without restricting private network ranges, because environment agents may be
// deployed on trusted private subnets.
func ValidateOutboundHTTPURL(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	if parsed.User != nil {
		return nil, errors.New("embedded credentials are not allowed")
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("URL host is required")
	}

	return parsed, nil
}
