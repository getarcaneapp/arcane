package httpx

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"kit.getarcane.app"
)

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

func ManagerBaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	managerURL := strings.TrimRight(rawURL, "/")
	managerURL = strings.TrimSuffix(managerURL, "/api")
	return managerURL
}

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

	port := cmp.Or(parsed.Port(), kit.Ternary(strings.EqualFold(parsed.Scheme, "https"), "443", "80"))

	return net.JoinHostPort(host, port)
}

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
