package distribution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	ref "go.podman.io/image/v5/docker/reference"
)

const defaultRegistryHost = "index.docker.io"

type Credentials struct {
	Username string
	Token    string
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

	// Network-level daemon errors are intentionally not fallback-eligible.
	// If the daemon cannot reach the registry at all, the backend's direct HTTP
	// client is also unlikely to succeed, so only registry/API capability
	// failures are allowed to trigger fallback.
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

func fetchWithTokenAuthInternal(ctx context.Context, httpClient *http.Client, registryHost, repository, tag, challenge string, credential *Credentials) (string, error) {
	realm, service := parseWWWAuthInternal(challenge)
	if realm == "" {
		return "", fmt.Errorf("no auth realm found")
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
	if digest := headers.Get("Docker-Content-Digest"); digest != "" {
		return digest
	}

	etag := strings.Trim(headers.Get("ETag"), `"`)
	if strings.HasPrefix(etag, "sha256:") {
		return etag
	}

	return ""
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
