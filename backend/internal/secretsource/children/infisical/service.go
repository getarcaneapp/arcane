// Package infisical is the Infisical secret provider: Universal Auth login,
// project and folder browsing, and reading one environment path.
package infisical

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"getarcane.dev/arcane/v3/pkg/infisical"
	"getarcane.dev/types/v3"
)

// Provider reads secrets from one Infisical instance. It reuses one API
// client, so the access token is cached across calls.
type Provider struct {
	client *infisical.Client
}

// NormalizeSettings validates settings and returns them in canonical form.
func NormalizeSettings(settings *types.SecretSourceInfisicalSettings) (*types.SecretSourceInfisicalSettings, error) {
	if settings == nil {
		return nil, errors.New("infisical settings are required")
	}
	siteURL, err := infisical.ParseSiteURL(settings.SiteURL)
	if err != nil {
		return nil, err
	}
	clientID := strings.TrimSpace(settings.ClientID)
	if clientID == "" {
		return nil, errors.New("client ID is required")
	}
	return &types.SecretSourceInfisicalSettings{
		SiteURL:          siteURL.String(),
		ClientID:         clientID,
		OrganizationSlug: strings.TrimSpace(settings.OrganizationSlug),
	}, nil
}

// NormalizeTarget validates a binding target and returns it in canonical form.
func NormalizeTarget(target *types.SecretSourceInfisicalTarget) (*types.SecretSourceInfisicalTarget, error) {
	if target == nil {
		return nil, errors.New("an Infisical project and environment are required")
	}
	projectID := strings.TrimSpace(target.ProjectID)
	environment := strings.TrimSpace(target.Environment)
	if projectID == "" || environment == "" {
		return nil, errors.New("an Infisical project and environment are required")
	}
	return &types.SecretSourceInfisicalTarget{
		ProjectID:        projectID,
		Environment:      environment,
		SecretPath:       infisical.NormalizeSecretPath(target.SecretPath),
		IncludeImports:   target.IncludeImports,
		ExpandReferences: target.ExpandReferences,
	}, nil
}

// Describe names a target in messages, e.g. environment "prod", path "/api".
func Describe(target *types.SecretSourceInfisicalTarget) string {
	if target == nil {
		return "no target"
	}
	return fmt.Sprintf("environment %q, path %q", target.Environment, target.SecretPath)
}

// New builds a provider. clientSecret is the decrypted source credential.
func New(httpClient *http.Client, settings types.SecretSourceInfisicalSettings, clientSecret string) (*Provider, error) {
	client, err := infisical.NewClient(httpClient, infisical.Config{
		SiteURL:          settings.SiteURL,
		ClientID:         settings.ClientID,
		ClientSecret:     clientSecret,
		OrganizationSlug: settings.OrganizationSlug,
	})
	if err != nil {
		return nil, err
	}
	return &Provider{client: client}, nil
}

// Test logs in and checks whether the identity can list projects. Failures
// are reported in the result so the UI can show Infisical's message.
func (p *Provider) Test(ctx context.Context) types.SecretSourceTestResult {
	session, err := p.client.Login(ctx)
	if err != nil {
		return types.SecretSourceTestResult{OK: false, Message: err.Error()}
	}
	projects, err := p.client.ListProjects(ctx)
	if err != nil {
		return types.SecretSourceTestResult{
			OK:      true,
			Message: "Authenticated, but this identity cannot list projects; enter the project ID by hand",
		}
	}
	return types.SecretSourceTestResult{
		OK:           true,
		Message:      fmt.Sprintf("Authenticated; token valid for %s", session.ExpiresIn.Round(time.Minute)),
		CanBrowse:    true,
		VisibleCount: len(projects),
	}
}

// Browse lists projects (with their environments as options) or the folders
// directly under a path.
func (p *Provider) Browse(ctx context.Context, query types.SecretSourceBrowseQuery) ([]types.SecretSourceBrowseItem, error) {
	switch query.Kind {
	case types.SecretSourceBrowseInfisicalProjects:
		projects, err := p.client.ListProjects(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]types.SecretSourceBrowseItem, 0, len(projects))
		for _, project := range projects {
			environments := make([]types.SecretSourceBrowseItem, 0, len(project.Environments))
			for _, environment := range project.Environments {
				environments = append(environments, types.SecretSourceBrowseItem{ID: environment.Slug, Name: environment.Name})
			}
			items = append(items, types.SecretSourceBrowseItem{ID: project.ID, Name: project.Name, Detail: project.Slug, Options: environments})
		}
		return items, nil
	case types.SecretSourceBrowseInfisicalFolders:
		if strings.TrimSpace(query.ProjectID) == "" || strings.TrimSpace(query.Environment) == "" {
			return nil, errors.New("project and environment are required to list folders")
		}
		folders, err := p.client.ListFolders(ctx, query.ProjectID, query.Environment, query.Path)
		if err != nil {
			return nil, err
		}
		slices.Sort(folders)
		items := make([]types.SecretSourceBrowseItem, 0, len(folders))
		for _, folder := range folders {
			items = append(items, types.SecretSourceBrowseItem{ID: folder, Name: folder})
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unknown browse kind %q for Infisical", query.Kind)
	}
}

// Fetch reads the target's secrets. Infisical keys always carry a value, so
// nothing is skipped here; the caller drops names that are not valid
// environment variable names.
func (p *Provider) Fetch(ctx context.Context, target types.SecretSourceBindingTarget) (map[string]string, []string, error) {
	if target.Infisical == nil {
		return nil, nil, errors.New("binding has no Infisical target")
	}
	values, err := p.client.ListSecrets(ctx, infisical.SecretsQuery{
		ProjectID:              target.Infisical.ProjectID,
		Environment:            target.Infisical.Environment,
		SecretPath:             target.Infisical.SecretPath,
		IncludeImports:         target.Infisical.IncludeImports,
		ExpandSecretReferences: target.Infisical.ExpandReferences,
	})
	return values, nil, err
}
