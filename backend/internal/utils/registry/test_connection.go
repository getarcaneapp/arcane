package registry

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/docker/distribution/registry/client/auth/challenge"
)

type TestResult struct {
	OverallSuccess bool      `json:"overall_success"`
	PingSuccess    bool      `json:"ping_success"`
	AuthSuccess    bool      `json:"auth_success"`
	CatalogSuccess bool      `json:"catalog_success"`
	URL            string    `json:"url"`
	Domain         string    `json:"domain"`
	Timestamp      time.Time `json:"timestamp"`
	Errors         []string  `json:"errors"`
}

const (
	authTimeout     = 10 * time.Second
	catalogTimeout  = 15 * time.Second
	registryPing    = "/v2/"
	registryCatalog = "/v2/_catalog"
)

type registryConnectionTester struct {
	client   *Client
	registry string
	result   *TestResult
	endpoint *url.URL
}

func TestRegistryConnection(ctx context.Context, registryURL string, creds *Credentials) (*TestResult, error) {
	tester := &registryConnectionTester{
		client:   NewClient(),
		registry: normalizeRegistry(registryURL),
		result: &TestResult{
			URL:       registryURL,
			Domain:    registryURL,
			Timestamp: time.Now(),
			Errors:    []string{},
		},
	}

	if err := tester.parseEndpoint(); err != nil {
		return tester.result, err
	}

	challengeManager, err := tester.ping(ctx)
	if err != nil {
		return tester.result, err
	}

	authHeader := tester.performAuth(ctx, challengeManager, creds)
	tester.testCatalog(ctx, authHeader)

	tester.result.OverallSuccess = tester.result.PingSuccess && tester.result.AuthSuccess && tester.result.CatalogSuccess
	return tester.result, nil
}

func normalizeRegistry(registryURL string) string {
	reg := NormalizeRegistryForComparison(registryURL)
	if reg == "docker.io" {
		return DefaultRegistry
	}
	return reg
}

func (t *registryConnectionTester) parseEndpoint() error {
	registryEndpoint := t.client.GetRegistryURL(t.registry)
	endpointURL, err := url.Parse(registryEndpoint)
	if err != nil {
		t.addErrorf("Invalid registry URL: %v", err)
		return err
	}

	t.endpoint = endpointURL
	return nil
}

func (t *registryConnectionTester) ping(ctx context.Context) (challenge.Manager, error) {
	challengeManager, err := PingV2Registry(ctx, t.endpoint, t.client.http.Transport)
	if err != nil {
		t.addErrorf("Connectivity test failed: %v", err)
		return nil, err
	}
	t.result.PingSuccess = true
	return challengeManager, nil
}

func extractAuthURL(challengeManager challenge.Manager, endpointURL *url.URL) string {
	if challengeManager == nil {
		return ""
	}

	challenges, err := challengeManager.GetChallenges(*endpointURL)
	if err != nil {
		return ""
	}

	for _, ch := range challenges {
		if !strings.EqualFold(ch.Scheme, "bearer") {
			continue
		}
		realm, ok := ch.Parameters["realm"]
		if !ok || strings.TrimSpace(realm) == "" {
			continue
		}

		authURL, parseErr := url.Parse(realm)
		if parseErr != nil {
			return ""
		}
		if service, ok := ch.Parameters["service"]; ok && strings.TrimSpace(service) != "" {
			query := authURL.Query()
			if query.Get("service") == "" {
				query.Set("service", service)
				authURL.RawQuery = query.Encode()
			}
		}
		return authURL.String()
	}

	return ""
}

func (t *registryConnectionTester) performAuth(ctx context.Context, challengeManager challenge.Manager, creds *Credentials) string {
	authURL := extractAuthURL(challengeManager, t.endpoint)
	if authURL == "" {
		t.result.AuthSuccess = true
		return ""
	}
	if creds == nil {
		t.result.AuthSuccess = false
		return ""
	}

	if authHeader := t.tryBearerAuth(ctx, authURL, creds); authHeader != "" {
		t.result.AuthSuccess = true
		return authHeader
	}

	authHeader, ok := t.tryBasicAuth(ctx, creds)
	t.result.AuthSuccess = ok
	return authHeader
}

func (t *registryConnectionTester) tryBearerAuth(ctx context.Context, authURL string, creds *Credentials) string {
	tok, err := t.client.GetTokenMulti(ctx, authURL, []string{}, creds)
	if err == nil && tok != "" {
		return "Bearer " + tok
	}

	return ""
}

func (t *registryConnectionTester) tryBasicAuth(ctx context.Context, creds *Credentials) (string, bool) {
	resp, err := t.doRegistryGet(ctx, registryPing, authTimeout, func(req *http.Request) {
		req.SetBasicAuth(creds.Username, creds.Token)
	})
	if err != nil {
		t.addErrorf("Auth request failed: %v", err)
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.addErrorf("Auth failed: status %d", resp.StatusCode)
		return "", false
	}

	ba := []byte(creds.Username + ":" + creds.Token)
	return "Basic " + base64.StdEncoding.EncodeToString(ba), true
}

func (t *registryConnectionTester) testCatalog(ctx context.Context, authHeader string) {
	resp, err := t.doRegistryGet(ctx, registryCatalog, catalogTimeout, func(req *http.Request) {
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
	})
	if err != nil {
		t.addErrorf("Catalog request failed: %v", err)
		t.result.CatalogSuccess = false
		return
	}
	defer func() { _ = resp.Body.Close() }()

	t.result.CatalogSuccess = resp.StatusCode == http.StatusOK
	if !t.result.CatalogSuccess {
		t.addErrorf("Catalog returned status: %d", resp.StatusCode)
	}
}

func (t *registryConnectionTester) doRegistryGet(ctx context.Context, path string, timeout time.Duration, setup func(*http.Request)) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, t.registryURL(path), http.NoBody)
	if err != nil {
		return nil, err
	}
	if setup != nil {
		setup(req)
	}

	resp, err := t.client.http.Do(req) //nolint:gosec // intentional request to user-provided registry for connection test
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (t *registryConnectionTester) registryURL(path string) string {
	return strings.TrimRight(t.client.GetRegistryURL(t.registry), "/") + path
}

func (t *registryConnectionTester) addErrorf(format string, args ...any) {
	t.result.Errors = append(t.result.Errors, fmt.Sprintf(format, args...))
}
