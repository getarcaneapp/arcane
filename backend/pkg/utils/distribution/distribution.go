package distribution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/pkg/utils/imagedigest"
	ref "go.podman.io/image/v5/docker/reference"
)

const defaultRegistryHost = "registry-1.docker.io"
const dockerHubRateLimitRepository = "ratelimitpreview/test"

// trustedAuthDelegations maps registry hosts to their trusted external auth realm hosts.
// Some registries serve images under their own domain but delegate token auth to a
// separate host (e.g. lscr.io delegates to ghcr.io).
var trustedAuthDelegations = map[string]string{
	"docker.io": "auth.docker.io",
	"lscr.io":   "ghcr.io",
}

type Credentials struct {
	Username string
	Token    string
}

// RateLimitInfo contains pull quota information returned by registry headers.
type RateLimitInfo struct {
	Limit         *int   `json:"limit,omitempty"`
	Remaining     *int   `json:"remaining,omitempty"`
	Used          *int   `json:"used,omitempty"`
	WindowSeconds *int   `json:"windowSeconds,omitempty"`
	Source        string `json:"source,omitempty"`
}

type Reference struct {
	NormalizedRef string
	RegistryHost  string
	Repository    string
	Tag           string
}

func NormalizeReference(imageRef string) (*Reference, error) {
	named, err := ref.ParseNormalizedNamed(strings.TrimSpace(imageRef))
	if err != nil {
		return nil, fmt.Errorf("invalid image reference %q: %w", imageRef, err)
	}

	if _, ok := named.(ref.Digested); ok {
		return nil, fmt.Errorf("digest-pinned references are not supported for distribution inspect: %q", imageRef)
	}

	registryHost := normalizeRegistryForComparisonInternal(ref.Domain(named))
	repository := ref.Path(named)

	tag := "latest"
	if tagged, ok := named.(ref.NamedTagged); ok {
		tag = tagged.Tag()
	}

	return &Reference{
		NormalizedRef: registryHost + "/" + repository + ":" + tag,
		RegistryHost:  registryHost,
		Repository:    repository,
		Tag:           tag,
	}, nil
}

func IsFallbackEligibleDaemonError(err error) bool {
	if err == nil {
		return false
	}

	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "authentication required") ||
		strings.Contains(errLower, "no basic auth credentials") ||
		strings.Contains(errLower, "access denied") ||
		strings.Contains(errLower, "incorrect username or password") ||
		strings.Contains(errLower, "status: 401") ||
		strings.Contains(errLower, "status 401") {
		return false
	}

	if strings.Contains(errLower, "x509") || strings.Contains(errLower, "certificate") || strings.Contains(errLower, "tls") {
		return false
	}

	// Most network-level daemon errors are not fallback-eligible: if the daemon
	// cannot reach the registry, the backend's direct HTTP client is also unlikely
	// to succeed. Only registry/API capability failures trigger fallback.
	//
	// Exception: "proxyconnect" errors indicate the daemon's HTTP transport has a
	// broken proxy configured at the OS level (e.g. Synology NAS with Docker 20.10).
	// The Arcane backend container does not inherit that proxy, so the direct HTTP
	// client can still reach the registry successfully.
	indicators := []string{
		"not found",
		" 404 ",
		"status: 404",
		"status 404",
		"403 forbidden",
		"status: 403",
		"status 403",
		"administrative rules",
		"not implemented",
		"unsupported",
		"distribution disabled",
		"distribution api",
		"proxyconnect",
	}

	for _, indicator := range indicators {
		if strings.Contains(errLower, indicator) {
			return true
		}
	}

	return false
}

func FetchDigest(ctx context.Context, registryHost, repository, tag string, credential *Credentials) (string, error) {
	return FetchDigestWithHTTPClient(ctx, registryHost, repository, tag, credential, nil)
}

// FetchDockerHubRateLimit fetches Docker Hub pull rate limit information.
func FetchDockerHubRateLimit(ctx context.Context, credential *Credentials) (*RateLimitInfo, error) {
	return FetchDockerHubRateLimitWithHTTPClient(ctx, credential, nil)
}

// FetchDockerHubRateLimitWithHTTPClient fetches Docker Hub pull rate limit
// information using the provided HTTP client.
func FetchDockerHubRateLimitWithHTTPClient(ctx context.Context, credential *Credentials, httpClient *http.Client) (*RateLimitInfo, error) {
	return FetchRegistryRateLimitWithHTTPClient(ctx, "docker.io", dockerHubRateLimitRepository, "latest", credential, httpClient)
}

// FetchRegistryRateLimitWithHTTPClient fetches pull rate limit information from
// an OCI registry manifest response.
func FetchRegistryRateLimitWithHTTPClient(ctx context.Context, registryHost, repository, tag string, credential *Credentials, httpClient *http.Client) (*RateLimitInfo, error) {
	if httpClient == nil {
		httpClient = NewRegistryHTTPClient()
	}

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	authHeader := ""
	if credential != nil && strings.TrimSpace(credential.Username) != "" && strings.TrimSpace(credential.Token) != "" {
		authHeader = basicAuthHeaderInternal(credential.Username, credential.Token)
	}

	resp, err := manifestRequestInternal(requestCtx, httpClient, registryHost, repository, tag, authHeader)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		if challenge == "" {
			return nil, fmt.Errorf("manifest request failed with status: %d", resp.StatusCode)
		}
		return fetchRateLimitWithTokenAuthInternal(requestCtx, httpClient, registryHost, repository, tag, challenge, credential)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request failed with status: %d", resp.StatusCode)
	}

	return extractRateLimitFromHeadersInternal(resp.Header)
}

func FetchDigestWithHTTPClient(ctx context.Context, registryHost, repository, tag string, credential *Credentials, httpClient *http.Client) (string, error) {
	if httpClient == nil {
		httpClient = NewRegistryHTTPClient()
	}

	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	authHeader := ""
	if credential != nil && strings.TrimSpace(credential.Username) != "" && strings.TrimSpace(credential.Token) != "" {
		authHeader = basicAuthHeaderInternal(credential.Username, credential.Token)
	}

	resp, err := manifestRequestInternal(requestCtx, httpClient, registryHost, repository, tag, authHeader)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		if challenge == "" {
			return "", fmt.Errorf("manifest request failed with status: %d", resp.StatusCode)
		}
		return fetchWithTokenAuthInternal(requestCtx, httpClient, registryHost, repository, tag, challenge, credential)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest request failed with status: %d", resp.StatusCode)
	}

	digest := extractDigestFromHeadersInternal(resp.Header)
	if digest == "" {
		return "", fmt.Errorf("no digest header found in response")
	}

	return digest, nil
}

func fetchRateLimitWithTokenAuthInternal(ctx context.Context, httpClient *http.Client, registryHost, repository, tag, challenge string, credential *Credentials) (*RateLimitInfo, error) {
	realm, service := parseWWWAuthInternal(challenge)
	if realm == "" {
		return nil, fmt.Errorf("no auth realm found")
	}
	if err := validateAuthRealmInternal(registryHost, realm); err != nil {
		return nil, err
	}

	token, err := fetchRegistryTokenInternal(ctx, httpClient, realm, service, repository, credential)
	if err != nil {
		return nil, err
	}

	resp, err := manifestRequestInternal(ctx, httpClient, registryHost, repository, tag, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authenticated manifest request failed with status: %d", resp.StatusCode)
	}

	return extractRateLimitFromHeadersInternal(resp.Header)
}

func fetchWithTokenAuthInternal(ctx context.Context, httpClient *http.Client, registryHost, repository, tag, challenge string, credential *Credentials) (string, error) {
	realm, service := parseWWWAuthInternal(challenge)
	if realm == "" {
		return "", fmt.Errorf("no auth realm found")
	}
	if err := validateAuthRealmInternal(registryHost, realm); err != nil {
		return "", err
	}

	token, err := fetchRegistryTokenInternal(ctx, httpClient, realm, service, repository, credential)
	if err != nil {
		return "", err
	}

	resp, err := manifestRequestInternal(ctx, httpClient, registryHost, repository, tag, token)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authenticated manifest request failed with status: %d", resp.StatusCode)
	}

	digest := extractDigestFromHeadersInternal(resp.Header)
	if digest == "" {
		return "", fmt.Errorf("no digest header found in authenticated response")
	}

	return digest, nil
}

func fetchRegistryTokenInternal(ctx context.Context, httpClient *http.Client, authURL, service, repository string, credential *Credentials) (string, error) {
	parsed, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("invalid auth url: %w", err)
	}

	query := parsed.Query()
	if query.Get("service") == "" {
		if strings.TrimSpace(service) != "" {
			query.Set("service", strings.TrimSpace(service))
		} else {
			query.Set("service", serviceNameFromAuthURLInternal(authURL))
		}
	}
	query.Add("scope", fmt.Sprintf("repository:%s:pull", repository))
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	if credential != nil && strings.TrimSpace(credential.Username) != "" && strings.TrimSpace(credential.Token) != "" {
		req.SetBasicAuth(strings.TrimSpace(credential.Username), strings.TrimSpace(credential.Token))
	}

	resp, err := httpClient.Do(req) //nolint:gosec // authURL comes from the registry challenge for the current image
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status: %d", resp.StatusCode)
	}

	var tokenResponse struct {
		Token  string `json:"token"`
		Legacy string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	token := strings.TrimSpace(tokenResponse.Token)
	if token == "" {
		token = strings.TrimSpace(tokenResponse.Legacy)
	}
	if token == "" {
		return "", fmt.Errorf("no token in response")
	}

	return token, nil
}

func manifestRequestInternal(ctx context.Context, httpClient *http.Client, registryHost, repository, tag, authHeader string) (*http.Response, error) {
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registryBaseURLInternal(registryHost), repository, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}
	addManifestRequestHeadersInternal(req, authHeader)

	resp, err := httpClient.Do(req) //nolint:gosec // manifestURL is derived from the normalized image reference
	if err != nil {
		return nil, fmt.Errorf("manifest request failed: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusMethodNotAllowed:
		// Retry with GET only when the registry rejects HEAD as an unsupported method.
		_ = resp.Body.Close()

		getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create manifest fallback request: %w", err)
		}
		addManifestRequestHeadersInternal(getReq, authHeader)

		getResp, err := httpClient.Do(getReq) //nolint:gosec // manifestURL is derived from the normalized image reference
		if err != nil {
			return nil, fmt.Errorf("manifest fallback request failed: %w", err)
		}

		return getResp, nil
	default:
		return resp, nil
	}
}

// NewRegistryHTTPClient returns the shared transport configuration used for
// direct registry digest lookups.
func NewRegistryHTTPClient() *http.Client {
	var transport *http.Transport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = http.ProxyFromEnvironment

	return &http.Client{Transport: transport}
}

func registryBaseURLInternal(registryHost string) string {
	trimmed := strings.TrimSpace(registryHost)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" && normalizeRegistryForComparisonInternal(parsed.Host) == "docker.io" {
			return "https://" + defaultRegistryHost
		}
		return strings.TrimSuffix(trimmed, "/")
	}

	normalizedHost := normalizeRegistryForComparisonInternal(trimmed)
	if normalizedHost == "docker.io" {
		return "https://" + defaultRegistryHost
	}

	return "https://" + normalizedHost
}

func addManifestRequestHeadersInternal(req *http.Request, authHeader string) {
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json")
	req.Header.Set("User-Agent", "Arcane")
	if strings.TrimSpace(authHeader) != "" {
		req.Header.Set("Authorization", buildAuthHeaderInternal(authHeader))
	}
}

func buildAuthHeaderInternal(authHeader string) string {
	trimmed := strings.TrimSpace(authHeader)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") {
		return trimmed
	}

	return "Bearer " + trimmed
}

func basicAuthHeaderInternal(username, token string) string {
	raw := strings.TrimSpace(username) + ":" + strings.TrimSpace(token)
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

func extractDigestFromHeadersInternal(headers http.Header) string {
	if normalized, err := imagedigest.Normalize(headers.Get("Docker-Content-Digest")); err == nil {
		return normalized
	}

	etag := strings.Trim(headers.Get("ETag"), `"`)
	if normalized, err := imagedigest.Normalize(etag); err == nil {
		return normalized
	}

	return ""
}

func extractRateLimitFromHeadersInternal(headers http.Header) (*RateLimitInfo, error) {
	limit, limitWindow, limitErr := parseRateLimitHeaderInternal(headers.Get("RateLimit-Limit"))
	if limitErr != nil {
		return nil, fmt.Errorf("parse RateLimit-Limit: %w", limitErr)
	}

	remaining, remainingWindow, remainingErr := parseRateLimitHeaderInternal(headers.Get("RateLimit-Remaining"))
	if remainingErr != nil {
		return nil, fmt.Errorf("parse RateLimit-Remaining: %w", remainingErr)
	}

	if limit == nil && remaining == nil {
		return nil, fmt.Errorf("rate limit headers not returned")
	}

	windowSeconds := limitWindow
	if windowSeconds == nil {
		windowSeconds = remainingWindow
	}

	var used *int
	if limit != nil && remaining != nil {
		value := *limit - *remaining
		used = &value
	}

	return &RateLimitInfo{
		Limit:         limit,
		Remaining:     remaining,
		Used:          used,
		WindowSeconds: windowSeconds,
		Source:        strings.TrimSpace(headers.Get("Docker-RateLimit-Source")),
	}, nil
}

func parseRateLimitHeaderInternal(value string) (*int, *int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil, nil
	}

	if beforeComma, _, ok := strings.Cut(value, ","); ok {
		value = strings.TrimSpace(beforeComma)
	}

	parts := strings.Split(value, ";")
	quota, err := parsePositiveIntInternal(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, nil, err
	}

	var window *int
	for _, part := range parts[1:] {
		key, raw, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(key) != "w" {
			continue
		}

		parsedWindow, parseErr := parsePositiveIntInternal(strings.TrimSpace(raw))
		if parseErr != nil {
			return nil, nil, fmt.Errorf("invalid window: %w", parseErr)
		}
		window = parsedWindow
		break
	}

	return quota, window, nil
}

func parsePositiveIntInternal(value string) (*int, error) {
	if value == "" {
		return nil, fmt.Errorf("empty integer value")
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil, fmt.Errorf("invalid integer value %q", value)
	}

	return &parsed, nil
}

func parseWWWAuthInternal(header string) (string, string) {
	lower := strings.ToLower(header)
	if !strings.HasPrefix(lower, "bearer ") {
		return "", ""
	}

	_, after, ok := strings.Cut(header, " ")
	if !ok {
		return "", ""
	}

	var realm string
	var service string
	for _, part := range splitBearerDirectivesInternal(after) {
		part = strings.TrimSpace(part)
		lowerPart := strings.ToLower(part)

		switch {
		case strings.HasPrefix(lowerPart, "realm="):
			realm = strings.Trim(part[len("realm="):], `"`)
		case strings.HasPrefix(lowerPart, "service="):
			service = strings.Trim(part[len("service="):], `"`)
		}
	}

	return realm, service
}

func splitBearerDirectivesInternal(value string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false

	for _, r := range value {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func serviceNameFromAuthURLInternal(authURL string) string {
	if strings.Contains(authURL, "auth.docker.io") {
		return "registry.docker.io"
	}

	trimmed := strings.TrimPrefix(authURL, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	host, _, _ := strings.Cut(trimmed, "/")
	if host == "" {
		return "registry"
	}

	return host
}

func validateAuthRealmInternal(registryHost, realm string) error {
	parsedRealm, err := url.Parse(strings.TrimSpace(realm))
	if err != nil {
		return fmt.Errorf("invalid auth realm: %w", err)
	}

	if !strings.EqualFold(parsedRealm.Scheme, "https") {
		return fmt.Errorf("auth realm must use HTTPS, got %q", parsedRealm.Scheme)
	}

	realmHost := normalizeAuthRealmHostInternal(parsedRealm.Host)
	registry := normalizeAuthRealmHostInternal(registryHost)

	if realmHost == "" {
		return fmt.Errorf("invalid auth realm host")
	}
	if realmHost == registry {
		return nil
	}

	if trustedRealm, ok := trustedAuthDelegations[registry]; ok && realmHost == trustedRealm {
		return nil
	}

	return fmt.Errorf("untrusted auth realm host %q for registry %q", realmHost, registry)
}

func normalizeAuthRealmHostInternal(raw string) string {
	normalized := normalizeRegistryForComparisonInternal(raw)
	if normalized == "" {
		return ""
	}

	host, port, err := net.SplitHostPort(normalized)
	if err != nil {
		return normalized
	}

	if port == "443" {
		return host
	}

	return net.JoinHostPort(host, port)
}

func normalizeRegistryForComparisonInternal(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/")

	if slash := strings.Index(value, "/"); slash != -1 {
		value = value[:slash]
	}

	switch value {
	case "docker.io", "registry-1.docker.io", "index.docker.io":
		return "docker.io"
	default:
		return value
	}
}
