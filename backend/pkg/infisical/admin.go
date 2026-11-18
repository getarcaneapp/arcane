package infisical

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type (
	// Identity is a machine identity of an organization.
	Identity struct {
		ID   string
		Name string
	}

	// SecretInput is one secret to create or update.
	SecretInput struct {
		Key     string
		Value   string
		Comment string
	}

	// WriteOutcome reports how Infisical handled a secret write. A path with a
	// change approval policy turns the write into a pending approval request.
	WriteOutcome struct {
		PendingApproval bool
	}
)

const (
	// The calls in this file write to Infisical. Arcane only makes them with the
	// optional setup identity of a source, never with the identity used to deploy.

	// maxIdentityLookups bounds how many Universal Auth configurations
	// FindIdentityByClientID reads before it gives up.
	maxIdentityLookups = 200
)

// CreateProject creates a secret-manager project with Infisical's default
// environments (dev, staging, prod). An identity that creates a project
// becomes its admin.
func (c *Client) CreateProject(ctx context.Context, name string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	body := map[string]any{"projectName": name, "type": "secret-manager", "shouldCreateDefaultEnvs": true}
	var payload struct {
		Project Project `json:"project"`
	}
	if err := c.sendJSONInternal(ctx, http.MethodPost, "/api/v1/projects", body, &payload); err != nil {
		return Project{}, err
	}
	if payload.Project.ID == "" {
		return Project{}, errors.New("infisical created the project but returned no project ID")
	}
	return payload.Project, nil
}

// EnsureFolderPath creates every missing folder of path, such as /apps/immich.
// It returns the folders it created, outermost first.
func (c *Client) EnsureFolderPath(ctx context.Context, projectID, environment, path string) ([]string, error) {
	segments := strings.Split(strings.Trim(NormalizeSecretPath(path), "/"), "/")
	created := []string{}
	parent := "/"
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		existing, err := c.ListFolders(ctx, projectID, environment, parent)
		if err != nil {
			return created, err
		}
		if !slices.Contains(existing, segment) {
			body := map[string]any{"projectId": projectID, "environment": environment, "name": segment, "path": parent}
			if createErr := c.sendJSONInternal(ctx, http.MethodPost, "/api/v2/folders", body, nil); createErr != nil {
				return created, fmt.Errorf("create folder %s: %w", joinSecretPathInternal(parent, segment), createErr)
			}
			created = append(created, joinSecretPathInternal(parent, segment))
		}
		parent = joinSecretPathInternal(parent, segment)
	}
	return created, nil
}

// CreateSecrets creates secrets that do not exist yet in one path.
func (c *Client) CreateSecrets(ctx context.Context, projectID, environment, path string, secrets []SecretInput) (WriteOutcome, error) {
	if len(secrets) == 0 {
		return WriteOutcome{}, nil
	}
	items := make([]map[string]any, 0, len(secrets))
	for _, secret := range secrets {
		items = append(items, map[string]any{"secretKey": secret.Key, "secretValue": secret.Value, "secretComment": secret.Comment})
	}
	body := map[string]any{
		"projectId":   projectID,
		"environment": environment,
		"secretPath":  NormalizeSecretPath(path),
		"secrets":     items,
	}
	return c.writeSecretsInternal(ctx, http.MethodPost, body)
}

// UpdateSecrets replaces the values of existing secrets in one path. It fails
// when a secret does not exist, so it never creates secrets by accident.
func (c *Client) UpdateSecrets(ctx context.Context, projectID, environment, path string, secrets []SecretInput) (WriteOutcome, error) {
	if len(secrets) == 0 {
		return WriteOutcome{}, nil
	}
	items := make([]map[string]any, 0, len(secrets))
	for _, secret := range secrets {
		items = append(items, map[string]any{"secretKey": secret.Key, "secretValue": secret.Value})
	}
	body := map[string]any{
		"projectId":   projectID,
		"environment": environment,
		"secretPath":  NormalizeSecretPath(path),
		"mode":        "failOnNotFound",
		"secrets":     items,
	}
	return c.writeSecretsInternal(ctx, http.MethodPatch, body)
}

func (c *Client) writeSecretsInternal(ctx context.Context, method string, body map[string]any) (WriteOutcome, error) {
	var payload struct {
		Approval *struct {
			ID string `json:"id"`
		} `json:"approval"`
	}
	if err := c.sendJSONInternal(ctx, method, "/api/v4/secrets/batch", body, &payload); err != nil {
		return WriteOutcome{}, err
	}
	return WriteOutcome{PendingApproval: payload.Approval != nil}, nil
}

// OrganizationID returns the organization of the logged-in identity.
func (c *Client) OrganizationID(ctx context.Context) (string, error) {
	var payload struct {
		IdentityDetails struct {
			Organization struct {
				ID string `json:"id"`
			} `json:"organization"`
		} `json:"identityDetails"`
	}
	if err := c.getJSONInternal(ctx, "/api/v1/identities/details", nil, &payload); err != nil {
		return "", err
	}
	if payload.IdentityDetails.Organization.ID == "" {
		return "", errors.New("infisical returned no organization for this identity")
	}
	return payload.IdentityDetails.Organization.ID, nil
}

// FindIdentityByClientID finds the machine identity whose Universal Auth
// client ID is clientID. It returns nil when no identity the caller can read
// matches. The caller needs permission to read the organization's identities.
func (c *Client) FindIdentityByClientID(ctx context.Context, organizationID, clientID string) (*Identity, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, errors.New("client ID is required")
	}
	var payload struct {
		Identities []struct {
			IdentityID string `json:"identityId"`
			Identity   struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"identity"`
		} `json:"identities"`
	}
	if err := c.getJSONInternal(ctx, "/api/v1/identities", url.Values{"orgId": {organizationID}}, &payload); err != nil {
		return nil, err
	}

	for i, item := range payload.Identities {
		if i >= maxIdentityLookups {
			break
		}
		id := item.Identity.ID
		if id == "" {
			id = item.IdentityID
		}
		if id == "" {
			continue
		}
		var auth struct {
			IdentityUniversalAuth struct {
				ClientID string `json:"clientId"`
			} `json:"identityUniversalAuth"`
		}
		err := c.getJSONInternal(ctx, "/api/v1/auth/universal-auth/identities/"+url.PathEscape(id), nil, &auth)
		if err != nil {
			// Identities without Universal Auth answer 4xx; skip them.
			if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.StatusCode < 500 {
				continue
			}
			return nil, err
		}
		if auth.IdentityUniversalAuth.ClientID == clientID {
			return &Identity{ID: id, Name: item.Identity.Name}, nil
		}
	}
	return nil, nil
}

// AddProjectIdentity gives an identity a role in a project. It reports
// alreadyMember instead of failing when the identity is already in it.
func (c *Client) AddProjectIdentity(ctx context.Context, projectID, identityID, role string) (alreadyMember bool, err error) {
	body := map[string]any{"roles": []map[string]any{{"role": role, "isTemporary": false}}}
	path := "/api/v1/projects/" + url.PathEscape(projectID) + "/memberships/identities/" + url.PathEscape(identityID)
	err = c.sendJSONInternal(ctx, http.MethodPost, path, body, nil)
	if apiErr, ok := errors.AsType[*APIError](err); ok &&
		(apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusConflict) &&
		strings.Contains(strings.ToLower(apiErr.Message), "already") {
		return true, nil
	}
	return false, err
}

// IsNotFound reports whether err is an Infisical 404, such as a folder path
// that does not exist yet.
func IsNotFound(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

func (c *Client) sendJSONInternal(ctx context.Context, method, path string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode infisical request: %w", err)
	}
	token, err := c.tokenInternal(ctx)
	if err != nil {
		return err
	}
	return c.doInternal(ctx, method, path, nil, encoded, token, out)
}

func joinSecretPathInternal(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}
