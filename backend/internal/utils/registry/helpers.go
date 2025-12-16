package registry

import (
	"context"
	"net/http"
	"strings"
	"time"

	ref "go.podman.io/image/v5/docker/reference"
)

func GetRegistryAddress(imageRef string) (string, error) {
	named, err := ref.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	addr := ref.Domain(named)
	if addr == DefaultRegistryDomain {
		return DefaultRegistryHost, nil
	}
	return addr, nil
}

// ResolveRegistryRedirect checks if a registry domain redirects to Docker Hub
// by attempting a HEAD request to /v2/. If it fails with DNS or connection errors,
// it assumes the domain is a redirect/alias to Docker Hub.
func ResolveRegistryRedirect(ctx context.Context, registryDomain string) (resolvedDomain string, isDockerHub bool) {
	// Skip if already a known Docker Hub domain
	if registryDomain == DefaultRegistryDomain ||
		registryDomain == DefaultRegistry ||
		registryDomain == DefaultRegistryHost {
		return DefaultRegistry, true
	}

	// Try to connect to the registry's v2 API
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	testURL := "https://" + registryDomain + "/v2/"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, testURL, nil)
	if err != nil {
		// If we can't even create a request, assume it's a Docker Hub redirect
		return DefaultRegistry, true
	}

	resp, err := client.Do(req)
	if err != nil {
		// DNS failure, connection refused, or timeout - likely a redirect domain
		// Common for custom domains like docker.umami.is that don't host registry APIs
		if strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "timeout") {
			return DefaultRegistry, true
		}
		// Other errors - return original domain
		return registryDomain, false
	}
	defer resp.Body.Close()

	// If we get a valid response (even 401/403), the registry API exists
	// Status codes 200, 401, 403 indicate a working registry
	if resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden {
		return registryDomain, false
	}

	// For other status codes, assume it might be a redirect
	return DefaultRegistry, true
}

// RegistryConfig represents a registry configuration for scheme determination
type RegistryConfig struct {
	URL      string
	Insecure bool
}

// GetRegistryScheme determines whether a registry should use http or https
// based on the provided registry configurations
func GetRegistryScheme(domain string, registries []RegistryConfig) string {
	// Normalize domain for comparison
	normalizedDomain := strings.TrimPrefix(domain, "https://")
	normalizedDomain = strings.TrimPrefix(normalizedDomain, "http://")
	normalizedDomain = strings.ToLower(normalizedDomain)

	for _, reg := range registries {
		regURL := strings.TrimPrefix(reg.URL, "https://")
		regURL = strings.TrimPrefix(regURL, "http://")
		regURL = strings.TrimSuffix(regURL, "/")
		regURL = strings.ToLower(regURL)

		if regURL == normalizedDomain || strings.Contains(normalizedDomain, regURL) {
			if reg.Insecure {
				return "http"
			}
			return "https"
		}
	}

	return "https" // Default to secure
}
