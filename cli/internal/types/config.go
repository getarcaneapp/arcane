package types

import (
	"errors"
	"maps"
	"strings"
)

type (
	PaginationResourceConfig struct {
		Limit int `yaml:"limit,omitempty"`
	}

	PaginationConfig struct {
		Resources map[string]PaginationResourceConfig `yaml:"resources,omitempty"`
		Default   PaginationResourceConfig            `yaml:"default,omitempty"`
	}

	PreviewsConfig struct {
		NewSelfUpdater bool `yaml:"newSelfUpdater"`
	}

	Config struct {
		Pagination        PaginationConfig `yaml:"pagination,omitempty"`
		ServerURL         string           `yaml:"server_url"`
		APIKey            string           `yaml:"api_key,omitempty"`
		JWTToken          string           `yaml:"-"`
		RefreshToken      string           `yaml:"-"`
		FederatedAudience string           `yaml:"federated_audience,omitempty"`
		LogLevel          string           `yaml:"log_level,omitempty"`
		CLIUpdateChannel  string           `yaml:"cli_update_channel,omitempty"`
		Previews          PreviewsConfig   `yaml:"previews"`
	}
)

var (
	KnownPaginatedResources = []string{
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

	paginationResourceReplacer = strings.NewReplacer("_", "", "-", "", " ", "")
)

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

func (c *Config) HasAuth() bool {
	return c.APIKey != "" || c.JWTToken != ""
}

func (c *Config) ValidateServerURL() error {
	if c.ServerURL == "" {
		return errors.New("server_url is not configured. Run: arcane config set --server-url <url>")
	}
	return nil
}

func (c *Config) Validate() error {
	if err := c.ValidateServerURL(); err != nil {
		return err
	}
	if !c.HasAuth() {
		return errors.New("authentication is not configured. Run: arcane config set --api-key <key> OR arcane auth login")
	}
	return nil
}

func (c *Config) IsConfigured() bool {
	return c.ServerURL != "" && c.HasAuth()
}

func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Pagination.Resources = maps.Clone(c.Pagination.Resources)
	return &out
}

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

func (c *Config) SetDefaultLimit(limit int) {
	if c == nil {
		return
	}
	c.Pagination.Default.Limit = limit
}

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
