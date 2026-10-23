package types

// BaseTemplate contains common fields shared by all template types.
type BaseTemplate struct {
	// ID is the unique identifier of the template.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// Name of the template.
	//
	// Required: false
	Name string `json:"name" required:"false" unorm:"nfc" trim:"true" minLength:"1"`

	// Description of the template.
	//
	// Required: false
	Description string `json:"description" required:"false" unorm:"nfc" trim:"true"`
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

	// IconURL is the URL to the template icon.
	//
	// Required: false
	IconURL string `json:"icon_url,omitempty"`

	// Tags is a list of tags associated with the template.
	//
	// Required: true
	Tags []string `json:"tags"`
}

// TemplateBaseRegistry contains common fields shared by all registry types.
type TemplateBaseRegistry struct {
	// Name of the registry.
	//
	// Required: false
	Name string `json:"name" required:"false" unorm:"nfc" trim:"true"`

	// Description of the registry.
	//
	// Required: false
	Description string `json:"description" required:"false" unorm:"nfc" trim:"true"`

	// URL of the registry.
	//
	// Required: false
	URL string `json:"url" required:"false"`
}

// TemplateRemoteRegistry represents a remote template registry configuration.
type TemplateRemoteRegistry struct {
	TemplateBaseRegistry

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

// TemplateRegistry represents a local registry configuration.
type TemplateRegistry struct {
	TemplateBaseRegistry

	// ID is the unique identifier of the registry.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// Enabled indicates if the registry is enabled.
	//
	// Required: false
	Enabled bool `json:"enabled" required:"false"`

	// LastFetchError is the error message from the most recent failed fetch, if any.
	//
	// Required: false
	LastFetchError *string `json:"lastFetchError,omitempty" readOnly:"true"`
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
	EnvVariables []EnvVariable `json:"envVariables"`

	// IsFolderTemplate indicates the template belongs to a folder in the templates directory,
	// so its name and description cannot be changed.
	//
	// Required: true
	IsFolderTemplate bool `json:"isFolderTemplate"`
}

// Template represents a Docker Compose template.
type Template struct {
	BaseTemplate

	// Content is the Docker Compose file content.
	//
	// Required: false
	Content string `json:"content" required:"false"`

	// EnvContent is the environment file content.
	//
	// Required: false
	EnvContent *string `json:"envContent,omitempty"`

	// IsCustom indicates if this is a user-created custom template.
	//
	// Required: true
	IsCustom bool `json:"isCustom" readOnly:"true"`

	// IsRemote indicates if this template is from a remote registry.
	//
	// Required: true
	IsRemote bool `json:"isRemote" readOnly:"true"`

	// RegistryID is the ID of the registry this template belongs to.
	//
	// Required: false
	RegistryID *string `json:"registryId,omitempty" readOnly:"true"`

	// Registry is the registry information if the template is from a registry.
	//
	// Required: false
	Registry *TemplateRegistry `json:"registry,omitempty" readOnly:"true"`

	// Metadata contains additional metadata about the template.
	//
	// Required: false
	Metadata *TemplateMeta `json:"metadata,omitempty" readOnly:"true"`
}

// DefaultTemplatesResponse contains the default compose, swarm stack, and env templates.
type DefaultTemplatesResponse struct {
	// ComposeTemplate is the default Docker Compose template content.
	//
	// Required: true
	ComposeTemplate string `json:"composeTemplate"`

	// SwarmStackTemplate is the default Docker Swarm stack template content.
	//
	// Required: true
	SwarmStackTemplate string `json:"swarmStackTemplate"`

	// SwarmStackEnvTemplate is the default environment template content for Docker Swarm stacks.
	//
	// Required: true
	SwarmStackEnvTemplate string `json:"swarmStackEnvTemplate"`

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
