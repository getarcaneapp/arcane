package infisical

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"getarcane.dev/arcane/v3/pkg/infisical"
	"getarcane.dev/types/v3"
)

type (
	// Setup writes to Infisical with a source's setup identity. Deploys never use
	// it.
	Setup struct {
		client         *infisical.Client
		deployClientID string
	}
)

const (
	// DeployRole is the project role setup gives the deploy identity: it can read
	// secret values and nothing else.
	DeployRole = "viewer"
)

var (
	// ErrNoSetupIdentity is returned when a source has no setup identity.
	ErrNoSetupIdentity = errors.New("this secret source has no setup identity; add one in its settings to let Arcane create projects and secrets")

	folderNameUnsafe = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// NewSetup builds a setup client. setupSecret is the decrypted setup
// credential.
func NewSetup(httpClient *http.Client, settings types.SecretSourceInfisicalSettings, setupSecret string) (*Setup, error) {
	if strings.TrimSpace(settings.SetupClientID) == "" || setupSecret == "" {
		return nil, ErrNoSetupIdentity
	}
	client, err := infisical.NewClient(httpClient, infisical.Config{
		SiteURL:          settings.SiteURL,
		ClientID:         settings.SetupClientID,
		ClientSecret:     setupSecret,
		OrganizationSlug: settings.OrganizationSlug,
	})
	if err != nil {
		return nil, err
	}
	return &Setup{client: client, deployClientID: settings.ClientID}, nil
}

// SuggestFolder turns an Arcane project name into a folder path for the
// shared-folder mode, e.g. "My App" becomes "/my-app".
func SuggestFolder(projectName string) string {
	name := folderNameUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(projectName)), "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "app"
	}
	return "/" + name
}

// NormalizeSetupTarget validates a setup target and fills its defaults.
func NormalizeSetupTarget(target types.SecretSourceSetupTarget, arcaneProjectName string) (types.SecretSourceSetupTarget, error) {
	normalized := types.SecretSourceSetupTarget{
		Mode:        strings.TrimSpace(target.Mode),
		Environment: strings.TrimSpace(target.Environment),
		SecretPath:  infisical.NormalizeSecretPath(target.SecretPath),
	}
	if normalized.Environment == "" {
		return normalized, errors.New("an environment is required")
	}
	switch normalized.Mode {
	case types.SecretSourceSetupModeNewProject:
		normalized.ProjectName = strings.TrimSpace(target.ProjectName)
		if normalized.ProjectName == "" {
			normalized.ProjectName = strings.TrimSpace(arcaneProjectName)
		}
		if normalized.ProjectName == "" {
			return normalized, errors.New("a project name is required")
		}
		if len(normalized.ProjectName) > 64 {
			return normalized, errors.New("project names are limited to 64 characters in Infisical")
		}
	case types.SecretSourceSetupModeExistingProject, types.SecretSourceSetupModeSharedFolder:
		normalized.ProjectID = strings.TrimSpace(target.ProjectID)
		if normalized.ProjectID == "" {
			return normalized, errors.New("choose an Infisical project")
		}
		// A shared project keeps each Arcane project in its own folder.
		if normalized.Mode == types.SecretSourceSetupModeSharedFolder && normalized.SecretPath == "/" {
			normalized.SecretPath = SuggestFolder(arcaneProjectName)
		}
	default:
		return normalized, fmt.Errorf("unknown setup mode %q", target.Mode)
	}
	return normalized, nil
}

// RemoteValues returns the secrets defined directly at the target path, without
// imports or reference expansion, so they compare with .env values. A project
// or folder that does not exist yet has no secrets.
func (s *Setup) RemoteValues(ctx context.Context, target types.SecretSourceSetupTarget) (map[string]string, error) {
	if target.Mode == types.SecretSourceSetupModeNewProject {
		return map[string]string{}, nil
	}
	values, err := s.client.ListSecrets(ctx, infisical.SecretsQuery{
		ProjectID:   target.ProjectID,
		Environment: target.Environment,
		SecretPath:  target.SecretPath,
	})
	if infisical.IsNotFound(err) {
		return map[string]string{}, nil
	}
	return values, err
}

// ProjectNameTaken reports whether a project the setup identity can see is
// already named name.
func (s *Setup) ProjectNameTaken(ctx context.Context, name string) (bool, error) {
	projects, err := s.client.ListProjects(ctx)
	if err != nil {
		return false, err
	}
	for _, project := range projects {
		if strings.EqualFold(project.Name, strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}

// FindDeployIdentity finds the identity whose client ID the source deploys
// with. It returns nil when the setup identity cannot see it.
func (s *Setup) FindDeployIdentity(ctx context.Context) (*types.SecretSourceSetupIdentity, error) {
	orgID, err := s.client.OrganizationID(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := s.client.FindIdentityByClientID(ctx, orgID, s.deployClientID)
	if err != nil || identity == nil {
		return nil, err
	}
	return &types.SecretSourceSetupIdentity{ID: identity.ID, Name: identity.Name}, nil
}

// CreateProject creates the project of a new-project target and checks that
// it has the target environment.
func (s *Setup) CreateProject(ctx context.Context, name, environment string) (string, error) {
	project, err := s.client.CreateProject(ctx, name)
	if err != nil {
		return "", err
	}
	for _, env := range project.Environments {
		if env.Slug == environment {
			return project.ID, nil
		}
	}
	slugs := make([]string, 0, len(project.Environments))
	for _, env := range project.Environments {
		slugs = append(slugs, env.Slug)
	}
	return project.ID, fmt.Errorf("created project %q (ID %s), but it has no %q environment (it has %s); use it as an existing project with one of those",
		name, project.ID, environment, strings.Join(slugs, ", "))
}

// EnsurePath creates the missing folders of path.
func (s *Setup) EnsurePath(ctx context.Context, projectID, environment, path string) ([]string, error) {
	return s.client.EnsureFolderPath(ctx, projectID, environment, path)
}

// WriteSecrets creates the missing secrets and replaces the overwritten ones.
// pending reports a change held for approval by an Infisical policy.
func (s *Setup) WriteSecrets(ctx context.Context, projectID, environment, path string, create, overwrite []infisical.SecretInput) (pending bool, err error) {
	created, err := s.client.CreateSecrets(ctx, projectID, environment, path, create)
	if err != nil {
		return false, fmt.Errorf("create secrets: %w", err)
	}
	updated, err := s.client.UpdateSecrets(ctx, projectID, environment, path, overwrite)
	if err != nil {
		return created.PendingApproval, fmt.Errorf("update secrets: %w", err)
	}
	return created.PendingApproval || updated.PendingApproval, nil
}

// GrantDeployIdentity gives the deploy identity the viewer role.
func (s *Setup) GrantDeployIdentity(ctx context.Context, projectID, identityID string) (alreadyMember bool, err error) {
	return s.client.AddProjectIdentity(ctx, projectID, identityID, DeployRole)
}
