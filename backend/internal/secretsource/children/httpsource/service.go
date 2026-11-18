// Package httpsource is the generic HTTP secret provider. It reads a flat
// JSON object of names and values from any endpoint, such as the sops-kit and
// bws-kit sidecars, so a new secret manager can be supported without code in
// Arcane.
package httpsource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/pkg/httpsecrets"
	"getarcane.dev/arcane/v3/pkg/secretapi"
	"getarcane.dev/types/v3"
)

// Provider reads from one endpoint.
type Provider struct {
	client *httpsecrets.Client
}

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceHTTPSettings) (*types.SecretSourceHTTPSettings, error) {
	if settings == nil {
		return nil, errors.New("endpoint settings are required")
	}
	base, err := httpsecrets.ParseBaseURL(settings.BaseURL)
	if err != nil {
		return nil, err
	}
	return &types.SecretSourceHTTPSettings{BaseURL: base.String()}, nil
}

// NormalizeTarget validates a binding target. The path is optional.
func NormalizeTarget(target *types.SecretSourceHTTPTarget) (*types.SecretSourceHTTPTarget, error) {
	if target == nil {
		return &types.SecretSourceHTTPTarget{}, nil
	}
	path := strings.Trim(strings.TrimSpace(target.Path), "/")
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("%q is not allowed in a path", segment)
		}
		if path != "" && segment == "" {
			return nil, errors.New("empty path segment")
		}
	}
	if strings.ContainsAny(path, "?#") {
		return nil, errors.New("the path cannot contain ? or #")
	}
	return &types.SecretSourceHTTPTarget{Path: path}, nil
}

// Describe names a target in messages.
func Describe(target *types.SecretSourceHTTPTarget) string {
	if target == nil || target.Path == "" {
		return "the endpoint root"
	}
	return "path /" + target.Path
}

// New builds a provider; token may be empty.
func New(httpClient *http.Client, settings types.SecretSourceHTTPSettings, token string) (*Provider, error) {
	client, err := httpsecrets.NewClient(httpClient, settings.BaseURL, token)
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

// Test reads the base URL. Endpoints that only serve secrets on sub-paths
// may answer 404 or something other than JSON there (a directory listing),
// which still proves they are reachable.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	object, err := p.client.Get(ctx, "")
	switch {
	case secretapi.IsNotFound(err), errors.Is(err, secretapi.ErrUnexpectedShape):
		return types.SecretSourceTestResult{OK: true, Message: "The endpoint answers; its root has no secrets, so set a path when binding"}
	case err != nil:
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	return types.SecretSourceTestResult{
		OK:           true,
		Message:      fmt.Sprintf("The endpoint returned %d values", len(object)),
		VisibleCount: len(object),
	}
}

// Browse is not supported; the endpoint contract has no listing.
func (p *Provider) Browse(_ context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	return nil, fmt.Errorf("the HTTP provider cannot list %q; type the path", query.Kind)
}

// Fetch reads the target path.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.HTTP == nil {
		return nil, nil, errors.New("binding has no HTTP target")
	}
	object, err := p.client.Get(ctx, target.HTTP.Path)
	if err != nil {
		return nil, nil, err
	}
	values, skipped := secretapi.StringValues(object)
	slices.Sort(skipped)
	return values, skipped, nil
}
