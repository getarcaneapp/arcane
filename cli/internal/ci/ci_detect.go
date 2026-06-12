package ci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ProviderAuto    = "auto"
	ProviderGitHub  = "github"
	ProviderGitLab  = "gitlab"
	ProviderGeneric = "generic"
)

// DetectToken resolves a CI-issued OIDC token from the requested provider.
func DetectToken(ctx context.Context, provider string, audience string, getenv func(string) string, httpClient *http.Client) (string, string, error) {
	provider = normalizeFederatedProviderInternal(provider)
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	switch provider {
	case ProviderAuto:
		if token, err := mintGitHubActionsTokenInternal(ctx, audience, getenv, httpClient); err == nil {
			return token, ProviderGitHub, nil
		}
		if token := strings.TrimSpace(getenv("CI_JOB_JWT_V2")); token != "" {
			return token, ProviderGitLab, nil
		}
		return "", "", errors.New("no supported CI OIDC token source detected")
	case ProviderGitHub:
		token, err := mintGitHubActionsTokenInternal(ctx, audience, getenv, httpClient)
		if err != nil {
			return "", "", err
		}
		return token, ProviderGitHub, nil
	case ProviderGitLab:
		token := strings.TrimSpace(getenv("CI_JOB_JWT_V2"))
		if token == "" {
			return "", "", errors.New("CI_JOB_JWT_V2 is not set; pass a GitLab id_tokens value with --token")
		}
		return token, ProviderGitLab, nil
	case ProviderGeneric:
		return "", "", errors.New("generic provider requires --token, --token-file, or --token-stdin")
	default:
		return "", "", fmt.Errorf("unsupported federated provider %q", provider)
	}
}

func mintGitHubActionsTokenInternal(ctx context.Context, audience string, getenv func(string) string, httpClient *http.Client) (string, error) {
	requestURL := strings.TrimSpace(getenv("ACTIONS_ID_TOKEN_REQUEST_URL"))
	requestToken := strings.TrimSpace(getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	if requestURL == "" || requestToken == "" {
		return "", errors.New("GitHub Actions OIDC request environment is not set")
	}

	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub Actions OIDC request URL: %w", err)
	}
	if strings.TrimSpace(audience) != "" {
		q := parsedURL.Query()
		q.Set("audience", strings.TrimSpace(audience))
		parsedURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub Actions OIDC request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request GitHub Actions OIDC token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("failed to read GitHub Actions OIDC response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub Actions OIDC request failed with status %d", resp.StatusCode)
	}

	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to parse GitHub Actions OIDC response: %w", err)
	}
	token := strings.TrimSpace(payload.Value)
	if token == "" {
		return "", errors.New("GitHub Actions OIDC response did not include a token")
	}
	return token, nil
}

func normalizeFederatedProviderInternal(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return ProviderAuto
	}
	return provider
}
