package httpx

import "strings"

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
