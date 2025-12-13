package template

import (
	"github.com/getarcaneapp/arcane/types/env"
	"github.com/getarcaneapp/arcane/types/meta"
)

// BaseTemplate contains common fields shared by all template types.
type BaseTemplate struct {
	// ID is the unique identifier of the template.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the template.
	//
	// Required: true
	Name string `json:"name"`

	// Description of the template.
	//
	// Required: true
	Description string `json:"description"`
}

// RemoteTemplate represents a template from a remote registry.
type RemoteTemplate struct {
	BaseTemplate

	// Version of the template.
	//
	// Required: true
	Version string `json:"version"`

	// Author of the template.
	//
	// Required: true
	Author string `json:"author"`

	// ComposeURL is the URL to the Docker Compose file.
	//
	// Required: true
	ComposeURL string `json:"compose_url"`

	// EnvURL is the URL to the environment file.
	//
	// Required: true
	EnvURL string `json:"env_url"`

	// DocumentationURL is the URL to the template documentation.
	//
	// Required: true
	DocumentationURL string `json:"documentation_url"`

	// Tags is a list of tags associated with the template.
	//
	// Required: true
	Tags []string `json:"tags"`
}

// BaseRegistry contains common fields shared by all registry types.
type BaseRegistry struct {
	// Name of the registry.
	//
	// Required: true
	Name string `json:"name"`

	// Description of the registry.
	//
	// Required: true
	Description string `json:"description"`

	// URL of the registry.
	//
	// Required: true
	URL string `json:"url"`
}

// RemoteRegistry represents a remote template registry configuration.
type RemoteRegistry struct {
	BaseRegistry

	// Schema is the JSON schema reference for the registry.
	//
	// Required: false
	Schema string `json:"$schema,omitempty"`

	// Version of the registry.
	//
	// Required: true
	Version string `json:"version"`

	// Author of the registry.
	//
	// Required: true
	Author string `json:"author"`

	// Templates is a list of templates available in the registry.
	//
	// Required: true
	Templates []RemoteTemplate `json:"templates"`
}

// Registry represents a local registry configuration.
type TemplateRegistry struct {
	BaseRegistry

	// ID is the unique identifier of the registry.
	//
	// Required: true
	ID string `json:"id"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: true
	Enabled bool `json:"enabled"`
}

// TemplateContent contains a template with its associated content and metadata.
type TemplateContent struct {
	// Template is the template information.
	//
	// Required: true
	Template Template `json:"template"`

	// Content is the Docker Compose file content.
	//
	// Required: true
	Content string `json:"content"`

	// EnvContent is the environment file content.
	//
	// Required: true
	EnvContent string `json:"envContent"`

	// Services is a list of service names defined in the template.
	//
	// Required: true
	Services []string `json:"services"`

	// EnvVariables is a list of environment variables used in the template.
	//
	// Required: true
	EnvVariables []env.Variable `json:"envVariables"`
}

// Template represents a Docker Compose template.
type Template struct {
	BaseTemplate

	// Content is the Docker Compose file content.
	//
	// Required: true
	Content string `json:"content"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent *string `json:"envContent,omitempty"`

	// IsCustom indicates if this is a user-created custom template.
	//
	// Required: true
	IsCustom bool `json:"isCustom"`

	// IsRemote indicates if this template is from a remote registry.
	//
	// Required: true
	IsRemote bool `json:"isRemote"`

	// RegistryID is the ID of the registry this template belongs to.
	//
	// Required: false
	RegistryID *string `json:"registryId,omitempty"`

	// Registry is the registry information if the template is from a registry.
	//
	// Required: false
	Registry *TemplateRegistry `json:"registry,omitempty"`

	// Metadata contains additional metadata about the template.
	//
	// Required: false
	Metadata *meta.TemplateMeta `json:"metadata,omitempty"`
}

// CreateRequest represents the request to create a template.
type CreateRequest struct {
	// Name of the template.
	//
	// Required: true
	Name string `json:"name"`

	// Description of the template.
	//
	// Required: false
	Description string `json:"description"`

	// Content is the Docker Compose file content.
	//
	// Required: true
	Content string `json:"content"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent"`
}

// UpdateRequest represents the request to update a template.
type UpdateRequest struct {
	// Name of the template.
	//
	// Required: true
	Name string `json:"name"`

	// Description of the template.
	//
	// Required: false
	Description string `json:"description"`

	// Content is the Docker Compose file content.
	//
	// Required: true
	Content string `json:"content"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent"`
}

// DefaultTemplatesResponse contains the default compose and env templates.
type DefaultTemplatesResponse struct {
	// ComposeTemplate is the default Docker Compose template content.
	//
	// Required: true
	ComposeTemplate string `json:"composeTemplate"`

	// EnvTemplate is the default environment template content.
	//
	// Required: true
	EnvTemplate string `json:"envTemplate"`
}

// SaveDefaultTemplatesRequest represents the request to save default templates.
type SaveDefaultTemplatesRequest struct {
	// ComposeContent is the Docker Compose template content.
	//
	// Required: true
	ComposeContent string `json:"composeContent"`

	// EnvContent is the environment template content.
	//
	// Required: false
	EnvContent string `json:"envContent"`
}

// CreateRegistryRequest represents the request to create a template registry.
type CreateRegistryRequest struct {
	// Name of the registry.
	//
	// Required: true
	Name string `json:"name"`

	// URL of the registry.
	//
	// Required: true
	URL string `json:"url"`

	// Description of the registry.
	//
	// Required: false
	Description string `json:"description"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: false
	Enabled bool `json:"enabled"`
}

// UpdateRegistryRequest represents the request to update a template registry.
type UpdateRegistryRequest struct {
	// Name of the registry.
	//
	// Required: true
	Name string `json:"name"`

	// URL of the registry.
	//
	// Required: true
	URL string `json:"url"`

	// Description of the registry.
	//
	// Required: false
	Description string `json:"description"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: false
	Enabled bool `json:"enabled"`
}
