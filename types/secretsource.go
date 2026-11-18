package types

import "time"

type (
	// SecretSourceInfisicalSettings connects to Infisical with a Universal Auth machine
	// identity. The client secret is the source's credential.
	//
	// SetupClientID optionally names a second identity that the project setup
	// wizard uses to create projects, folders, and secrets. Its client secret is
	// the source's setup credential. Deploys never use it.
	SecretSourceInfisicalSettings struct {
		SiteURL          string `json:"siteUrl"`
		ClientID         string `json:"clientId"`
		OrganizationSlug string `json:"organizationSlug,omitempty"`
		SetupClientID    string `json:"setupClientId,omitempty"`
	}

	// SecretSourceBitwardenSettings points at a Bitwarden CLI `bw serve` endpoint, which works
	// with both Bitwarden and Vaultwarden. The CLI holds the vault session, so this
	// provider has no credential of its own.
	SecretSourceBitwardenSettings struct {
		ServeURL string `json:"serveUrl"`
	}

	// SecretSourceVaultSettings connects to HashiCorp Vault or OpenBao. The credential is
	// the token (token auth) or the AppRole secret ID.
	SecretSourceVaultSettings struct {
		Address string `json:"address"`
		// Namespace is optional (Vault Enterprise and OpenBao namespaces).
		Namespace  string `json:"namespace,omitempty"`
		AuthMethod string `json:"authMethod"`
		// RoleID and AppRoleMount are used with AppRole auth.
		RoleID       string `json:"roleId,omitempty"`
		AppRoleMount string `json:"appRoleMount,omitempty"`
		// SetupToken reports that a separate token for the project setup wizard
		// is stored as the source's setup credential. Deploys never use it, so
		// the deploy token can stay read-only.
		SetupToken bool `json:"setupToken,omitempty"`
	}

	// SecretSourceDopplerSettings connects to Doppler. The credential is a service token
	// (scoped to one config) or a personal or service-account token.
	SecretSourceDopplerSettings struct {
		// APIURL is optional; empty means https://api.doppler.com.
		APIURL string `json:"apiUrl,omitempty"`
	}

	// SecretSourceOnePasswordSettings points at a 1Password Connect server. The credential is
	// the Connect access token.
	SecretSourceOnePasswordSettings struct {
		ServerURL string `json:"serverUrl"`
	}

	// SecretSourceProtonPassSettings points at a pass-kit sidecar, which runs Proton's
	// pass-cli with a personal access token. The credential, when set, is the
	// kit's bearer token.
	SecretSourceProtonPassSettings struct {
		KitURL string `json:"kitUrl"`
	}

	// SecretSourceHTTPSettings points at any endpoint that answers GET with a flat JSON
	// object of names and values. The credential, when set, is sent as a bearer
	// token.
	SecretSourceHTTPSettings struct {
		BaseURL string `json:"baseUrl"`
	}

	// SecretSourceSettings carries the non-secret settings of exactly one provider.
	SecretSourceSettings struct {
		Infisical   *SecretSourceInfisicalSettings   `json:"infisical,omitempty"`
		Bitwarden   *SecretSourceBitwardenSettings   `json:"bitwarden,omitempty"`
		Vault       *SecretSourceVaultSettings       `json:"vault,omitempty"`
		Doppler     *SecretSourceDopplerSettings     `json:"doppler,omitempty"`
		OnePassword *SecretSourceOnePasswordSettings `json:"onepassword,omitempty"`
		ProtonPass  *SecretSourceProtonPassSettings  `json:"protonpass,omitempty"`
		HTTP        *SecretSourceHTTPSettings        `json:"http,omitempty"`
	}

	// SecretSource is a saved connection to an external secret provider. The
	// credential is write-only and never returned.
	SecretSource struct {
		Settings      SecretSourceSettings `json:"settings"`
		CreatedAt     time.Time            `json:"createdAt"`
		LastTestedAt  *time.Time           `json:"lastTestedAt,omitempty"`
		LastTestError *string              `json:"lastTestError,omitempty"`
		UpdatedAt     *time.Time           `json:"updatedAt,omitempty"`
		ID            string               `json:"id"`
		Name          string               `json:"name"`
		Provider      string               `json:"provider"`
		BindingCount  int                  `json:"bindingCount"`
		HasCredential bool                 `json:"hasCredential"`
		// HasSetupCredential reports whether the setup wizard can write to the
		// provider.
		HasSetupCredential bool `json:"hasSetupCredential"`
	}

	SecretSourceCreateRequest struct {
		Name       string               `json:"name"`
		Provider   string               `json:"provider"`
		Settings   SecretSourceSettings `json:"settings"`
		Credential string               `json:"credential,omitempty"`
		// SetupCredential is the client secret of InfisicalSettings.SetupClientID.
		SetupCredential string `json:"setupCredential,omitempty"`
	}

	// SecretSourceUpdateRequest updates a source; nil fields keep the current value. A
	// nil or empty Credential or SetupCredential keeps the stored one. Clearing
	// the setup client ID also deletes the stored setup credential. The provider
	// cannot change.
	SecretSourceUpdateRequest struct {
		Name            *string               `json:"name,omitzero"`
		Settings        *SecretSourceSettings `json:"settings,omitzero"`
		Credential      *string               `json:"credential,omitzero"`
		SetupCredential *string               `json:"setupCredential,omitzero"`
		// ClearCredential removes the stored credential. Only HTTP sources,
		// whose bearer token is optional, accept it.
		ClearCredential bool `json:"clearCredential,omitempty"`
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
		// Mount and KVVersion select the Vault/OpenBao mount for "paths".
		Mount string `json:"mount,omitempty"`
		// VaultID selects the 1Password or Proton Pass vault for "items".
		VaultID   string `json:"vaultId,omitempty"`
		KVVersion int    `json:"kvVersion,omitempty"`
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

	// SecretSourceBitwardenTarget selects a folder, a collection, or a single item.
	SecretSourceBitwardenTarget struct {
		Scope string `json:"scope"`
		ID    string `json:"id"`
		// Name is the folder, collection, or item name at bind time, for display.
		Name string `json:"name,omitempty"`
	}

	// SecretSourceVaultTarget selects one secret in a KV mount. Each key of the secret is
	// one variable.
	SecretSourceVaultTarget struct {
		Mount string `json:"mount"`
		Path  string `json:"path"`
		// KVVersion is 1 or 2.
		KVVersion int `json:"kvVersion"`
	}

	// SecretSourceDopplerTarget selects one config. Both fields are empty with a service
	// token, which is already scoped to one config.
	SecretSourceDopplerTarget struct {
		Project string `json:"project,omitempty"`
		Config  string `json:"config,omitempty"`
	}

	// SecretSourceOnePasswordTarget selects a vault or one item in a vault.
	SecretSourceOnePasswordTarget struct {
		Scope   string `json:"scope"`
		VaultID string `json:"vaultId"`
		ItemID  string `json:"itemId,omitempty"`
		// Name is the vault or item name at bind time, for display.
		Name string `json:"name,omitempty"`
	}

	// SecretSourceProtonPassTarget selects a vault or one item in a vault. Scope uses the
	// 1Password values: in a vault, each item title is a variable; in an item,
	// each custom field is.
	SecretSourceProtonPassTarget struct {
		Scope   string `json:"scope"`
		VaultID string `json:"vaultId"`
		ItemID  string `json:"itemId,omitempty"`
		// Name is the vault or item name at bind time, for display.
		Name string `json:"name,omitempty"`
	}

	// SecretSourceHTTPTarget selects a path under the endpoint's base URL. Empty reads the
	// base URL itself.
	SecretSourceHTTPTarget struct {
		Path string `json:"path,omitempty"`
	}

	// SecretSourceBindingTarget carries the target of exactly one provider.
	SecretSourceBindingTarget struct {
		Infisical   *SecretSourceInfisicalTarget   `json:"infisical,omitempty"`
		Bitwarden   *SecretSourceBitwardenTarget   `json:"bitwarden,omitempty"`
		Vault       *SecretSourceVaultTarget       `json:"vault,omitempty"`
		Doppler     *SecretSourceDopplerTarget     `json:"doppler,omitempty"`
		OnePassword *SecretSourceOnePasswordTarget `json:"onepassword,omitempty"`
		ProtonPass  *SecretSourceProtonPassTarget  `json:"protonpass,omitempty"`
		HTTP        *SecretSourceHTTPTarget        `json:"http,omitempty"`
	}

	// SecretSourceTargetRef names one target of a source without binding it, such as the
	// targets that fill a new container's environment once.
	SecretSourceTargetRef struct {
		Target   SecretSourceBindingTarget `json:"target"`
		SourceID string                    `json:"sourceId"`
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
		// UnusedKeys are not referenced by any service in the compose file, so
		// no container receives them.
		UnusedKeys []string `json:"unusedKeys"`
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
	// Supported providers.
	SecretSourceProviderInfisical   = "infisical"
	SecretSourceProviderBitwarden   = "bitwarden"
	SecretSourceProviderVault       = "vault"
	SecretSourceProviderDoppler     = "doppler"
	SecretSourceProviderOnePassword = "onepassword"
	SecretSourceProviderHTTP        = "http"
	SecretSourceProviderProtonPass  = "protonpass"

	// 1Password binding scopes.
	// SecretSourceOnePasswordScopeVault maps every item in a vault: item title is the key,
	// its password (or credential, or notes) is the value.
	SecretSourceOnePasswordScopeVault = "vault"
	// SecretSourceOnePasswordScopeItem maps the fields of one item: label is the key.
	SecretSourceOnePasswordScopeItem = "item"

	// Vault and OpenBao authentication methods.
	SecretSourceVaultAuthToken   = "token"
	SecretSourceVaultAuthAppRole = "approle"

	// Bitwarden binding scopes.
	// SecretSourceBitwardenScopeFolder maps every item in a folder: item name is the key,
	// the login password (or a secure note's text) is the value.
	SecretSourceBitwardenScopeFolder = "folder"
	// SecretSourceBitwardenScopeCollection does the same for an organization collection.
	SecretSourceBitwardenScopeCollection = "collection"
	// SecretSourceBitwardenScopeItem maps the custom fields of one item to keys and values.
	SecretSourceBitwardenScopeItem = "item"

	// Browse kinds per provider.
	SecretSourceBrowseInfisicalProjects    = "projects"
	SecretSourceBrowseInfisicalFolders     = "folders"
	SecretSourceBrowseBitwardenFolders     = "folders"
	SecretSourceBrowseBitwardenCollections = "collections"
	SecretSourceBrowseBitwardenItems       = "items"
	SecretSourceBrowseVaultMounts          = "mounts"
	SecretSourceBrowseVaultPaths           = "paths"
	SecretSourceBrowseDopplerProjects      = "projects"
	SecretSourceBrowseDopplerConfigs       = "configs"
	SecretSourceBrowseOnePasswordVaults    = "vaults"
	SecretSourceBrowseOnePasswordItems     = "items"
	SecretSourceBrowseProtonPassVaults     = "vaults"
	SecretSourceBrowseProtonPassItems      = "items"

	// Binding owners. Bindings belong to projects; the owner kind leaves room for
	// standalone containers later.
	SecretSourceBindingOwnerProject = "project"
)

var (
	// SecretSourceProviders lists every supported provider.
	SecretSourceProviders = []string{SecretSourceProviderInfisical, SecretSourceProviderBitwarden, SecretSourceProviderVault, SecretSourceProviderDoppler, SecretSourceProviderOnePassword, SecretSourceProviderProtonPass, SecretSourceProviderHTTP}
)
