package registry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// buildAuthHeader constructs a proper Authorization header value from a token.
// If token already has a scheme (Bearer/Basic), it's used as-is. Otherwise, Bearer is assumed.
func buildAuthHeader(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") {
		return t
	}
	return "Bearer " + t
}

// getHeaderCI returns the first header value for the given key, case-insensitively.
func getHeaderCI(h http.Header, key string) string {
	for k, v := range h {
		if strings.EqualFold(k, key) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func extractDigestFromHeaders(h http.Header) string {
	d := getHeaderCI(h, ContentDigestHeader)
	if d != "" {
		return d
	}
	etag := getHeaderCI(h, "ETag")
	if etag != "" {
		etag = strings.Trim(etag, `"`)
		if strings.HasPrefix(etag, "sha256:") {
			return etag
		}
	}
	return ""
}

func (c *Client) GetLatestDigest(ctx context.Context, registry, repository, tag, token string) (string, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.GetRegistryURL(registry), repository, tag)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")
	req.Header.Add("Accept", "application/vnd.oci.image.index.v1+json")
	req.Header.Add("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("User-Agent", "Arcane")

	if ah := buildAuthHeader(token); ah != "" {
		req.Header.Set("Authorization", ah)
	}

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// If HEAD isn't supported by the registry, fall back to GET and try again.
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		slog.DebugContext(ctx, "HEAD not supported or returned unexpected status, retrying with GET", "url", url, "status", resp.StatusCode)
		// create GET request
		getReq, rerr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if rerr != nil {
			return "", rerr
		}
		getReq.Header = req.Header.Clone()
		getResp, derr := c.http.Do(getReq)
		if derr != nil {
			return "", derr
		}
		defer getResp.Body.Close()
		resp = getResp
	}

	elapsed := time.Since(start)
	slog.DebugContext(ctx, "manifest request completed", "url", url, "status", resp.StatusCode, "elapsed", elapsed)

	if resp.StatusCode == http.StatusUnauthorized {
		h := getHeaderCI(resp.Header, "WWW-Authenticate")
		if h != "" {
			slog.DebugContext(ctx, "manifest request unauthorized", "www-authenticate", h)
			return "", fmt.Errorf("unauthorized: %s", h)
		}
		return "", fmt.Errorf("manifest request failed with status: 401")
	}
	if resp.StatusCode != http.StatusOK {
		www := getHeaderCI(resp.Header, "WWW-Authenticate")
		if www == "" {
			www = "not present"
		}
		slog.DebugContext(ctx, "manifest request unexpected status", "www-authenticate", www)
		return "", fmt.Errorf("manifest request failed with status: %d, auth: %q", resp.StatusCode, www)
	}

	d := extractDigestFromHeaders(resp.Header)
	if d == "" {
		return "", fmt.Errorf("no digest header found in response")
	}

	slog.DebugContext(ctx, "resolved remote digest", "registry", registry, "repository", repository, "tag", tag, "digest", d, "elapsed", elapsed)

	return d, nil
}

func (c *Client) GetLatestDigestTimed(ctx context.Context, registry, repository, tag, token string) (string, time.Duration, error) {
	start := time.Now()
	d, err := c.GetLatestDigest(ctx, registry, repository, tag, token)
	return d, time.Since(start), err
}
