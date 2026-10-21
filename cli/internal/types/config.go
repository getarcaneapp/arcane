package types

import (
	"errors"
	"maps"
	"strings"
)

// PaginationResourceConfig defines pagination options for a specific resource.
type PaginationResourceConfig struct {
	// Limit is the default page size for list commands for this resource.
	Limit int `yaml:"limit,omitempty"`
}

// PaginationConfig contains global and per-resource pagination settings.
type PaginationConfig struct {
	// Default applies to paginated resources that do not have explicit per-resource config.
	Default PaginationResourceConfig `yaml:"default,omitempty"`
	// Resources contains per-resource pagination settings keyed by resource name.
	Resources map[string]PaginationResourceConfig `yaml:"resources,omitempty"`
}

// KnownPaginatedResources is the canonical list of paginated CLI resources.
var KnownPaginatedResources = []string{
	"containers",
	"images",
	"volumes",
	"networks",
	"projects",
	"environments",
	"registries",
	"templates",
	"repos",
	"gitops-syncs",
	"users",
	"roles",
	"events",
	"apikeys",
}

var paginationResourceReplacer = strings.NewReplacer("_", "", "-", "", " ", "")

// NormalizePaginatedResource normalizes resource names and common aliases.
func NormalizePaginatedResource(resource string) string {
	r := paginationResourceReplacer.Replace(strings.ToLower(strings.TrimSpace(resource)))
	switch r {
	case "apikey", "apikeys", "keys", "key":
		return "apikeys"
	case "container":
		return "containers"
	case "image":
		return "images"
	case "volume":
		return "volumes"
	case "network":
		return "networks"
	case "project":
		return "projects"
	case "environment":
		return "environments"
	case "registry", "registries":
		return "registries"
	case "template":
		return "templates"
	case "repo", "repos", "gitrepository", "gitrepositories", "gitrepo", "gitrepos":
		return "repos"
	case "gitops", "gitop", "gitopssync", "gitopssyncs":
		return "gitops-syncs"
	case "user":
		return "users"
	case "role":
		return "roles"
	case "event":
		return "events"
	default:
		return r
	}
}

// PreviewsConfig contains optional CLI preview features.
type PreviewsConfig struct {
	AllEnvironments bool `yaml:"allEnvironments"`
	NewSelfUpdater  bool `yaml:"newSelfUpdater"`
}

// Config holds the CLI configuration for connecting to an Arcane server.
// It is persisted to disk as YAML and loaded on each CLI invocation.
type Config struct {
	// ServerURL is the base URL of the Arcane server (e.g., http://localhost:3552)
	ServerURL string `yaml:"server_url"`
	// APIKey is the API key for authentication (sent as X-API-KEY)
	APIKey string `yaml:"api_key,omitempty"`
	// JWTToken is the access token sent as Authorization: Bearer. It is saved in auth.json.
	JWTToken string `yaml:"-"`
	// RefreshToken obtains new access tokens. Saved and read like JWTToken.
	RefreshToken string `yaml:"-"`
	// Previews contains optional CLI preview features.
	Previews PreviewsConfig `yaml:"previews"`
	// DefaultEnvironment is the default environment ID to use
	DefaultEnvironment string `yaml:"default_environment,omitempty"`
	// FederatedAudience is the default token audience for `arcane-cli auth federated`
	FederatedAudience string `yaml:"federated_audience,omitempty"`
	// LogLevel is the logging level (debug, info, warn, error, fatal, panic)
	LogLevel string `yaml:"log_level,omitempty"`
	// CLIUpdateChannel controls which channel self-update uses (stable or next).
	CLIUpdateChannel string `yaml:"cli_update_channel,omitempty"`
	// Pagination contains global and per-resource pagination configuration.
	Pagination PaginationConfig `yaml:"pagination,omitempty"`
}

// HasAuth returns true if either an API key or JWT token is configured.
func (c *Config) HasAuth() bool {
	return c.APIKey != "" || c.JWTToken != ""
}

// ValidateServerURL checks if the configuration has the server URL set.
// This is useful for commands like `auth login` that do not require prior authentication.
func (c *Config) ValidateServerURL() error {
	if c.ServerURL == "" {
		return errors.New("server_url is not configured. Run: arcane config set --server-url <url>")
	}
	return nil
}

// Validate requires a configured server URL and either an API key or JWT token.
func (c *Config) Validate() error {
	if err := c.ValidateServerURL(); err != nil {
		return err
	}
	if !c.HasAuth() {
		return errors.New("authentication is not configured. Run: arcane config set --api-key <key> OR arcane auth login")
	}
	return nil
}

// IsConfigured reports whether the server URL and authentication are configured.
func (c *Config) IsConfigured() bool {
	return c.ServerURL != "" && c.HasAuth()
}

// Clone returns a deep-copy of Config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Pagination.Resources = maps.Clone(c.Pagination.Resources)
	return &out
}

// LimitFor returns the configured limit for a resource, falling back to the global default.
func (c *Config) LimitFor(resource string) int {
	if c == nil {
		return 0
	}
	resource = NormalizePaginatedResource(resource)
	if resource != "" && c.Pagination.Resources != nil {
		if v, ok := c.Pagination.Resources[resource]; ok && v.Limit > 0 {
			return v.Limit
		}
	}
	if c.Pagination.Default.Limit > 0 {
		return c.Pagination.Default.Limit
	}
	return 0
}

// SetDefaultLimit configures the global pagination default.
func (c *Config) SetDefaultLimit(limit int) {
	if c == nil {
		return
	}
	c.Pagination.Default.Limit = limit
}

// SetResourceLimit configures per-resource pagination defaults.
func (c *Config) SetResourceLimit(resource string, limit int) {
	if c == nil {
		return
	}
	resource = NormalizePaginatedResource(resource)
	if resource == "" {
		return
	}
	if c.Pagination.Resources == nil {
		c.Pagination.Resources = make(map[string]PaginationResourceConfig)
	}
	if limit <= 0 {
		delete(c.Pagination.Resources, resource)
		return
	}
	c.Pagination.Resources[resource] = PaginationResourceConfig{Limit: limit}
}
