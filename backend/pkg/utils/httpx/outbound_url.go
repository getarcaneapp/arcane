package httpx

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateOutboundHTTPURL parses and validates an outbound HTTP(S) target URL.
// It intentionally performs syntactic hardening (scheme/host/credentials)
// without restricting private network ranges, because environment agents may be
// deployed on trusted private subnets.
func ValidateOutboundHTTPURL(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("URL is required")
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
		return nil, fmt.Errorf("embedded credentials are not allowed")
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL host is required")
	}

	return parsed, nil
}
