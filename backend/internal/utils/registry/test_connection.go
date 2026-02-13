package registry

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/docker/distribution/registry/client/auth/challenge"
	dockerregistry "github.com/docker/docker/registry"
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

func TestRegistryConnection(ctx context.Context, registryURL string, creds *Credentials, certPool *x509.CertPool, insecure bool) (*TestResult, error) {
	c := NewClientWithInsecure(certPool, insecure)
	res := &TestResult{
		URL:       registryURL,
		Domain:    registryURL,
		Timestamp: time.Now(),
		Errors:    []string{},
	}

	reg := normalizeRegistry(registryURL)
	endpointURL, err := parseRegistryEndpoint(c, reg, res)
	if err != nil {
		return res, err
	}

	challengeManager, err := pingRegistry(endpointURL, c, res)
	if err != nil {
		return res, err
	}

	authURL := extractAuthURL(challengeManager, endpointURL)
	authHeader := performAuth(ctx, c, reg, authURL, creds, res)
	testCatalog(ctx, c, reg, authHeader, res)

	res.OverallSuccess = res.PingSuccess && res.AuthSuccess && res.CatalogSuccess
	return res, nil
}

func normalizeRegistry(registryURL string) string {
	reg := trimScheme(registryURL)
	if reg == "docker.io" {
		return DefaultRegistry
	}
	return reg
}

func parseRegistryEndpoint(c *Client, reg string, res *TestResult) (*url.URL, error) {
	registryEndpoint := c.GetRegistryURL(reg)
	endpointURL, err := url.Parse(registryEndpoint)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Invalid registry URL: %v", err))
		res.PingSuccess = false
		res.OverallSuccess = false
		return nil, err
	}
	return endpointURL, nil
}

func pingRegistry(endpointURL *url.URL, c *Client, res *TestResult) (challenge.Manager, error) {
	challengeManager, err := dockerregistry.PingV2Registry(endpointURL, c.http.Transport)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Connectivity test failed: %v", err))
		res.PingSuccess = false
		res.AuthSuccess = false
		res.CatalogSuccess = false
		res.OverallSuccess = false
		return nil, err
	}
	res.PingSuccess = true
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

	for _, challenge := range challenges {
		if challenge.Scheme == "bearer" {
			if realm, ok := challenge.Parameters["realm"]; ok {
				authURL := realm
				if service, ok := challenge.Parameters["service"]; ok {
					authURL = fmt.Sprintf("%s?service=%s", realm, service)
				}
				return authURL
			}
		}
	}
	return ""
}

func performAuth(ctx context.Context, c *Client, reg, authURL string, creds *Credentials, res *TestResult) string {
	if authURL == "" || creds == nil {
		res.AuthSuccess = (authURL == "")
		return ""
	}

	if authHeader := tryBearerAuth(ctx, c, authURL, creds, res); authHeader != "" {
		return authHeader
	}

	return tryBasicAuth(ctx, c, reg, creds, res)
}

func tryBearerAuth(ctx context.Context, c *Client, authURL string, creds *Credentials, res *TestResult) string {
	tok, err := c.GetTokenMulti(ctx, authURL, []string{}, creds)
	if err == nil && tok != "" {
		res.AuthSuccess = true
		return "Bearer " + tok
	}
	return ""
}

func tryBasicAuth(ctx context.Context, c *Client, reg string, creds *Credentials, res *TestResult) string {
	pingURL := c.GetRegistryURL(reg) + "/v2/"
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, pingURL, nil)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Auth request creation failed: %v", err))
		res.AuthSuccess = false
		return ""
	}

	req.SetBasicAuth(creds.Username, creds.Token)
	resp, err := c.http.Do(req) //nolint:gosec // intentional request to user-provided registry for connection test
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Auth request failed: %v", err))
		res.AuthSuccess = false
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		res.AuthSuccess = true
		ba := []byte(creds.Username + ":" + creds.Token)
		return "Basic " + base64.StdEncoding.EncodeToString(ba)
	}

	res.Errors = append(res.Errors, fmt.Sprintf("Auth failed: status %d", resp.StatusCode))
	res.AuthSuccess = false
	return ""
}

func testCatalog(ctx context.Context, c *Client, reg, authHeader string, res *TestResult) {
	catalogURL := c.GetRegistryURL(reg) + "/v2/_catalog"
	ctx3, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx3, http.MethodGet, catalogURL, nil)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Catalog request creation failed: %v", err))
		res.CatalogSuccess = false
		return
	}

	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := c.http.Do(req) //nolint:gosec // intentional request to user-provided registry for connection test
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("Catalog request failed: %v", err))
		res.CatalogSuccess = false
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		res.CatalogSuccess = true
	} else {
		res.CatalogSuccess = false
		res.Errors = append(res.Errors, fmt.Sprintf("Catalog returned status: %d", resp.StatusCode))
	}
}

func trimScheme(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "/")
	return u
}
