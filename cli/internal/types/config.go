package types

import (
	"errors"
	"maps"
	"strings"
)

// PaginationResourceConfig defines pagination options for a specific resource.
type PaginationResourceConfig struct {
	// Limit is the default page size for list commands for this resource.
	Limit int `yaml:"limit,omitempty" mapstructure:"limit"`
}

// PaginationConfig contains global and per-resource pagination settings.
type PaginationConfig struct {
	// Default applies to paginated resources that do not have explicit per-resource config.
	Default PaginationResourceConfig `yaml:"default,omitempty" mapstructure:"default"`
	// Resources contains per-resource pagination settings keyed by resource name.
	Resources map[string]PaginationResourceConfig `yaml:"resources,omitempty" mapstructure:"resources"`
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
	"events",
	"apikeys",
}

// NormalizePaginatedResource normalizes resource names and common aliases.
func NormalizePaginatedResource(resource string) string {
	r := strings.ToLower(strings.TrimSpace(resource))
	r = strings.ReplaceAll(r, "_", "")
	r = strings.ReplaceAll(r, "-", "")
	r = strings.ReplaceAll(r, " ", "")
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
	case "event":
		return "events"
	default:
		return r
	}
}

// Config holds the CLI configuration for connecting to an Arcane server.
// It is persisted to disk as YAML and loaded on each CLI invocation.
type Config struct {
	// ServerURL is the base URL of the Arcane server (e.g., http://localhost:3552)
	ServerURL string `yaml:"server_url" mapstructure:"server_url"`
	// APIKey is the API key for authentication (sent as X-API-KEY)
	APIKey string `yaml:"api_key,omitempty" mapstructure:"api_key"`
	// JWTToken is the JWT access token for authentication (sent as Authorization: Bearer)
	JWTToken string `yaml:"jwt_token,omitempty" mapstructure:"jwt_token"`
	// RefreshToken is the refresh token for obtaining new access tokens
	RefreshToken string `yaml:"refresh_token,omitempty" mapstructure:"refresh_token"`
	// DefaultEnvironment is the default environment ID to use
	DefaultEnvironment string `yaml:"default_environment,omitempty" mapstructure:"default_environment"`
	// FederatedAudience is the default token audience for `arcane-cli auth federated`
	FederatedAudience string `yaml:"federated_audience,omitempty" mapstructure:"federated_audience"`
	// LogLevel is the logging level (debug, info, warn, error, fatal, panic)
	LogLevel string `yaml:"log_level,omitempty" mapstructure:"log_level"`
	// CLIUpdateChannel controls which channel self-update uses (stable or next).
	CLIUpdateChannel string `yaml:"cli_update_channel,omitempty" mapstructure:"cli_update_channel"`
	// Pagination contains global and per-resource pagination configuration.
	Pagination PaginationConfig `yaml:"pagination,omitempty" mapstructure:"pagination"`
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

// Validate checks if the configuration has all required fields set.
// It returns an error with instructions if ServerURL or APIKey is missing.
// This should be called before using the config to make API requests.
func (c *Config) Validate() error {
	if err := c.ValidateServerURL(); err != nil {
		return err
	}
	if !c.HasAuth() {
		return errors.New("authentication is not configured. Run: arcane config set --api-key <key> OR arcane auth login")
	}
	return nil
}

// IsConfigured returns true if both ServerURL and APIKey are set.
// This is a quick check to determine if the CLI has been configured
// without triggering validation errors.
func (c *Config) IsConfigured() bool {
	return c.ServerURL != "" && c.HasAuth()
}

// Clone returns a deep-copy of Config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	if c.Pagination.Resources != nil {
		out.Pagination.Resources = make(map[string]PaginationResourceConfig, len(c.Pagination.Resources))
		maps.Copy(out.Pagination.Resources, c.Pagination.Resources)
	}
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
