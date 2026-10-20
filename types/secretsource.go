package types

import "time"

type (
	// SecretSourceInfisicalSettings connects to Infisical with a Universal Auth machine
	// identity. The client secret is the source's credential.
	SecretSourceInfisicalSettings struct {
		SiteURL          string `json:"siteUrl"`
		ClientID         string `json:"clientId"`
		OrganizationSlug string `json:"organizationSlug,omitempty"`
	}

	// SecretSourceSettings carries the non-secret settings of exactly one provider.
	SecretSourceSettings struct {
		Infisical *SecretSourceInfisicalSettings `json:"infisical,omitempty"`
	}

	// SecretSource is a saved connection to an external secret provider. The
	// credential is write-only and never returned.
	SecretSource struct {
		CreatedAt     time.Time            `json:"createdAt"`
		Settings      SecretSourceSettings `json:"settings"`
		LastTestedAt  *time.Time           `json:"lastTestedAt,omitempty"`
		LastTestError *string              `json:"lastTestError,omitempty"`
		UpdatedAt     *time.Time           `json:"updatedAt,omitempty"`
		ID            string               `json:"id"`
		Name          string               `json:"name"`
		Provider      string               `json:"provider"`
		BindingCount  int                  `json:"bindingCount"`
		HasCredential bool                 `json:"hasCredential"`
	}

	SecretSourceCreateRequest struct {
		Name       string               `json:"name"`
		Provider   string               `json:"provider"`
		Settings   SecretSourceSettings `json:"settings"`
		Credential string               `json:"credential,omitempty"`
	}

	// SecretSourceUpdateRequest updates a source; nil fields keep the current value. A
	// nil or empty Credential keeps the stored one. The provider cannot change.
	SecretSourceUpdateRequest struct {
		Name       *string               `json:"name,omitzero"`
		Settings   *SecretSourceSettings `json:"settings,omitzero"`
		Credential *string               `json:"credential,omitzero"`
	}

	// SecretSourceTestRequest tests settings without saving them. When SourceID is set
	// and Credential is empty, the stored credential is used, and the result is
	// recorded on the source when the tested settings match the stored ones.
	SecretSourceTestRequest struct {
		SourceID   string               `json:"sourceId,omitempty"`
		Provider   string               `json:"provider"`
		Settings   SecretSourceSettings `json:"settings"`
		Credential string               `json:"credential,omitempty"`
	}

	SecretSourceTestResult struct {
		Message      string `json:"message"`
		VisibleCount int    `json:"visibleCount"`
		OK           bool   `json:"ok"`
		// CanBrowse reports whether the pickers can list targets; when false the
		// UI asks for IDs by hand.
		CanBrowse bool `json:"canBrowse"`
	}

	// SecretSourceBrowseQuery selects what to list from a source for the binding pickers.
	SecretSourceBrowseQuery struct {
		Kind        string `json:"kind"`
		ProjectID   string `json:"projectId,omitempty"`
		Environment string `json:"environment,omitempty"`
		Path        string `json:"path,omitempty"`
	}

	// SecretSourceBrowseItem is one pickable target. Options carries nested choices, such as
	// the environments of an Infisical project.
	SecretSourceBrowseItem struct {
		ID      string                   `json:"id"`
		Name    string                   `json:"name"`
		Detail  string                   `json:"detail,omitempty"`
		Options []SecretSourceBrowseItem `json:"options,omitempty"`
	}

	// SecretSourceInfisicalTarget selects one environment path of one Infisical project.
	SecretSourceInfisicalTarget struct {
		ProjectID        string `json:"projectId"`
		Environment      string `json:"environment"`
		SecretPath       string `json:"secretPath"`
		IncludeImports   bool   `json:"includeImports"`
		ExpandReferences bool   `json:"expandReferences"`
	}

	// SecretSourceBindingTarget carries the target of exactly one provider.
	SecretSourceBindingTarget struct {
		Infisical *SecretSourceInfisicalTarget `json:"infisical,omitempty"`
	}

	// SecretSourceBinding connects one Arcane project to one target in a secret source. A
	// project can have several; when two deliver the same key, the binding with
	// the lower position wins.
	SecretSourceBinding struct {
		Target         SecretSourceBindingTarget `json:"target"`
		DeployedAt     *time.Time                `json:"deployedAt,omitempty"`
		LastFetchedAt  *time.Time                `json:"lastFetchedAt,omitempty"`
		LastCheckedAt  *time.Time                `json:"lastCheckedAt,omitempty"`
		LastFetchError *string                   `json:"lastFetchError,omitempty"`
		ID             string                    `json:"id"`
		ProjectID      string                    `json:"projectId"`
		SourceID       string                    `json:"sourceId"`
		SourceName     string                    `json:"sourceName"`
		Provider       string                    `json:"provider"`
		Position       int                       `json:"position"`
		Required       bool                      `json:"required"`
		Enabled        bool                      `json:"enabled"`
		AutoRedeploy   bool                      `json:"autoRedeploy"`
		RedeployNeeded bool                      `json:"redeployNeeded"`
	}

	// SecretSourceBindingUpsertRequest creates or updates a binding. Position is optional:
	// a new binding goes last, and an update keeps its place unless one is given.
	SecretSourceBindingUpsertRequest struct {
		Target       SecretSourceBindingTarget `json:"target"`
		Position     *int                      `json:"position,omitempty" minimum:"0"`
		SourceID     string                    `json:"sourceId"`
		Required     bool                      `json:"required"`
		Enabled      bool                      `json:"enabled"`
		AutoRedeploy bool                      `json:"autoRedeploy"`
	}

	// SecretSourceProjectCheckResult reports every binding of a project, in order.
	SecretSourceProjectCheckResult struct {
		Bindings []SecretSourceCheckResult `json:"bindings"`
		// RedeployNeeded is true when any binding changed since the last deploy.
		RedeployNeeded bool `json:"redeployNeeded"`
	}

	// SecretSourceCheckResult reports what a fetch would deliver, without any secret values.
	SecretSourceCheckResult struct {
		FetchedAt  time.Time  `json:"fetchedAt"`
		DeployedAt *time.Time `json:"deployedAt,omitempty"`
		BindingID  string     `json:"bindingId"`
		// Error is set when this binding's fetch failed; the other fields are
		// then empty.
		Error string   `json:"error,omitempty"`
		Keys  []string `json:"keys"`
		// ShadowedKeys are delivered by an earlier binding too, so that binding's
		// value is used.
		ShadowedKeys []string `json:"shadowedKeys"`
		// OverriddenKeys are keys also defined in the project's .env or
		// .env.global; the provider's value wins at deploy.
		OverriddenKeys []string `json:"overriddenKeys"`
		// InvalidKeys cannot be used as environment variable names, or have no
		// usable value, and are skipped.
		InvalidKeys []string `json:"invalidKeys"`
		// RedeployNeeded is true when the secrets changed since the last deploy.
		RedeployNeeded bool `json:"redeployNeeded"`
		NeverDeployed  bool `json:"neverDeployed"`
	}
)

const (
	// Supported secret source providers.
	SecretSourceProviderInfisical = "infisical"

	// Browse kinds per provider.
	SecretSourceBrowseInfisicalProjects = "projects"
	SecretSourceBrowseInfisicalFolders  = "folders"

	// Binding owners. Bindings belong to projects; the owner kind leaves room for
	// standalone containers later.
	SecretSourceBindingOwnerProject = "project"
)

var (
	// SecretSourceProviders lists every supported provider.
	SecretSourceProviders = []string{SecretSourceProviderInfisical}
)
