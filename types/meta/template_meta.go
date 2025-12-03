package meta

// Template represents metadata about a template.
type Template struct {
	// Version of the template.
	//
	// Required: false
	Version *string `json:"version,omitempty"`

	// Author of the template.
	//
	// Required: false
	Author *string `json:"author,omitempty"`

	// Tags is a list of tags associated with the template.
	//
	// Required: false
	Tags []string `json:"tags,omitempty"`

	// RemoteURL is the URL to the remote template file.
	//
	// Required: false
	RemoteURL *string `json:"remoteUrl,omitempty"`

	// EnvURL is the URL to the environment file.
	//
	// Required: false
	EnvURL *string `json:"envUrl,omitempty"`

	// DocumentationURL is the URL to the template documentation.
	//
	// Required: false
	DocumentationURL *string `json:"documentationUrl,omitempty"`

	// UpdatedAt is the date and time when the template was last updated.
	//
	// Required: false
	UpdatedAt *string `json:"updatedAt,omitempty"`
}
